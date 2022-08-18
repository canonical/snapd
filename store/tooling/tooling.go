// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package tooling

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mvo5/goconfigparser"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// ToolingStore wraps access to the store for tools.
type ToolingStore struct {
	// Stdout is for output, mainly progress bars
	// left unset stdout is used
	Stdout io.Writer

	sto  StoreImpl
	user *auth.UserState
}

// A StoreImpl can find metadata on snaps, download snaps and fetch assertions.
// This interface is a subset of store.Store methods.
type StoreImpl interface {
	// SnapAction queries the store for snap information for the given install/refresh actions. Orthogonally it can be used to fetch or update assertions.
	SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, store.AssertionQuery, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error)

	// Download downloads the snap addressed by download info
	Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error

	// Assertion retrieves the assertion for the given type and primary key.
	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)
}

func newToolingStore(arch, storeID string) (*ToolingStore, error) {
	cfg := store.DefaultConfig()
	cfg.Architecture = arch
	cfg.StoreID = storeID
	var user *auth.UserState
	if authFn := os.Getenv("UBUNTU_STORE_AUTH_DATA_FILENAME"); authFn != "" {
		var err error
		user, err = readAuthFile(authFn)
		if err != nil {
			return nil, err
		}
	}
	sto := store.New(cfg, toolingStoreContext{})
	return &ToolingStore{
		sto:  sto,
		user: user,
	}, nil
}

type authData struct {
	Macaroon   string   `json:"macaroon"`
	Discharges []string `json:"discharges"`
}

func readAuthFile(authFn string) (*auth.UserState, error) {
	data, err := ioutil.ReadFile(authFn)
	if err != nil {
		return nil, fmt.Errorf("cannot read auth file %q: %v", authFn, err)
	}

	creds, err := parseAuthFile(authFn, data)
	if err != nil {
		// try snapcraft login format instead
		var err2 error
		creds, err2 = parseSnapcraftLoginFile(authFn, data)
		if err2 != nil {
			trimmed := bytes.TrimSpace(data)
			if len(trimmed) > 0 && trimmed[0] == '[' {
				return nil, err2
			}
			return nil, err
		}
	}

	return &auth.UserState{
		StoreMacaroon:   creds.Macaroon,
		StoreDischarges: creds.Discharges,
	}, nil
}

func parseAuthFile(authFn string, data []byte) (*authData, error) {
	var creds authData
	err := json.Unmarshal(data, &creds)
	if err != nil {
		return nil, fmt.Errorf("cannot decode auth file %q: %v", authFn, err)
	}
	if creds.Macaroon == "" || len(creds.Discharges) == 0 {
		return nil, fmt.Errorf("invalid auth file %q: missing fields", authFn)
	}
	return &creds, nil
}

func snapcraftLoginSection() string {
	if snapdenv.UseStagingStore() {
		return "login.staging.ubuntu.com"
	}
	return "login.ubuntu.com"
}

func parseSnapcraftLoginFile(authFn string, data []byte) (*authData, error) {
	errPrefix := fmt.Sprintf("invalid snapcraft login file %q", authFn)

	cfg := goconfigparser.New()
	if err := cfg.ReadString(string(data)); err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	sec := snapcraftLoginSection()
	macaroon, err := cfg.Get(sec, "macaroon")
	if err != nil {
		return nil, fmt.Errorf("%s: %s", errPrefix, err)
	}
	unboundDischarge, err := cfg.Get(sec, "unbound_discharge")
	if err != nil {
		return nil, fmt.Errorf("%s: %v", errPrefix, err)
	}
	if macaroon == "" || unboundDischarge == "" {
		return nil, fmt.Errorf("invalid snapcraft login file %q: empty fields", authFn)
	}
	return &authData{
		Macaroon:   macaroon,
		Discharges: []string{unboundDischarge},
	}, nil
}

// toolingStoreContext implements trivially store.DeviceAndAuthContext
// except implementing UpdateUserAuth properly to be used to refresh a
// soft-expired user macaroon.
type toolingStoreContext struct{}

func (tac toolingStoreContext) CloudInfo() (*auth.CloudInfo, error) {
	return nil, nil
}

func (tac toolingStoreContext) Device() (*auth.DeviceState, error) {
	return &auth.DeviceState{}, nil
}

func (tac toolingStoreContext) DeviceSessionRequestParams(_ string) (*store.DeviceSessionRequestParams, error) {
	return nil, store.ErrNoSerial
}

func (tac toolingStoreContext) ProxyStoreParams(defaultURL *url.URL) (proxyStoreID string, proxySroreURL *url.URL, err error) {
	return "", defaultURL, nil
}

func (tac toolingStoreContext) StoreID(fallback string) (string, error) {
	return fallback, nil
}

func (tac toolingStoreContext) UpdateDeviceAuth(_ *auth.DeviceState, newSessionMacaroon string) (*auth.DeviceState, error) {
	return nil, fmt.Errorf("internal error: no device state in tools")
}

func (tac toolingStoreContext) UpdateUserAuth(user *auth.UserState, discharges []string) (*auth.UserState, error) {
	user.StoreDischarges = discharges
	return user, nil
}

// NewToolingStoreFromModel creates ToolingStore for the snap store used by the given model.
func NewToolingStoreFromModel(model *asserts.Model, fallbackArchitecture string) (*ToolingStore, error) {
	architecture := model.Architecture()
	// can happen on classic
	if architecture == "" {
		architecture = fallbackArchitecture
	}
	return newToolingStore(architecture, model.Store())
}

// NewToolingStore creates ToolingStore, with optional arch and store id
// read from UBUNTU_STORE_ARCH and UBUNTU_STORE_ID environment variables.
func NewToolingStore() (*ToolingStore, error) {
	arch := os.Getenv("UBUNTU_STORE_ARCH")
	storeID := os.Getenv("UBUNTU_STORE_ID")
	return newToolingStore(arch, storeID)
}

// DownloadSnapOptions carries options for downloading snaps plus assertions.
type DownloadSnapOptions struct {
	TargetDir string

	Revision  snap.Revision
	Channel   string
	CohortKey string
	Basename  string

	LeavePartialOnError bool
}

var (
	errRevisionAndCohort = errors.New("cannot specify both revision and cohort")
	errPathInBase        = errors.New("cannot specify a path in basename (use target dir for that)")
)

func (opts *DownloadSnapOptions) validate() error {
	if strings.ContainsRune(opts.Basename, filepath.Separator) {
		return errPathInBase
	}
	if !(opts.Revision.Unset() || opts.CohortKey == "") {
		return errRevisionAndCohort
	}
	return nil
}

func (opts *DownloadSnapOptions) String() string {
	spec := make([]string, 0, 5)
	if !opts.Revision.Unset() {
		spec = append(spec, fmt.Sprintf("(%s)", opts.Revision))
	}
	if opts.Channel != "" {
		spec = append(spec, fmt.Sprintf("from channel %q", opts.Channel))
	}
	if opts.CohortKey != "" {
		// cohort keys are really long, and the rightmost bit being the
		// interesting bit, so ellipt the rest
		spec = append(spec, fmt.Sprintf(`from cohort %q`, strutil.ElliptLeft(opts.CohortKey, 10)))
	}
	if opts.Basename != "" {
		spec = append(spec, fmt.Sprintf("to %q", opts.Basename+".snap"))
	}
	if opts.TargetDir != "" {
		spec = append(spec, fmt.Sprintf("in %q", opts.TargetDir))
	}
	return strings.Join(spec, " ")
}

type DownloadedSnap struct {
	Path            string
	Info            *snap.Info
	RedirectChannel string
}

// DownloadSnap downloads the snap with the given name and options.
// It returns the final full path of the snap and a snap.Info for it and
// optionally a channel the snap got redirected to wrapped in DownloadedSnap.
func (tsto *ToolingStore) DownloadSnap(name string, opts DownloadSnapOptions) (downloadedSnap *DownloadedSnap, err error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	sto := tsto.sto

	if opts.TargetDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		opts.TargetDir = pwd
	}

	if !opts.Revision.Unset() {
		// XXX: is this really necessary (and, if it is, shoudn't we error out instead)
		opts.Channel = ""
	}

	logger.Debugf("Going to download snap %q %s.", name, &opts)

	actions := []*store.SnapAction{{
		Action:       "download",
		InstanceName: name,
		Revision:     opts.Revision,
		CohortKey:    opts.CohortKey,
		Channel:      opts.Channel,
	}}

	sars, _, err := sto.SnapAction(context.TODO(), nil, actions, nil, tsto.user, nil)
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return nil, err
	}
	sar := &sars[0]

	baseName := opts.Basename
	if baseName == "" {
		baseName = sar.Info.Filename()
	} else {
		baseName += ".snap"
	}
	targetFn := filepath.Join(opts.TargetDir, baseName)

	return tsto.snapDownload(targetFn, sar, opts)
}

func (tsto *ToolingStore) snapDownload(targetFn string, sar *store.SnapActionResult, opts DownloadSnapOptions) (downloadedSnap *DownloadedSnap, err error) {
	snap := sar.Info
	redirectChannel := sar.RedirectChannel

	// check if we already have the right file
	if osutil.FileExists(targetFn) {
		sha3_384Dgst, size, err := osutil.FileDigest(targetFn, crypto.SHA3_384)
		if err == nil && size == uint64(snap.DownloadInfo.Size) && fmt.Sprintf("%x", sha3_384Dgst) == snap.DownloadInfo.Sha3_384 {
			logger.Debugf("not downloading, using existing file %s", targetFn)
			return &DownloadedSnap{
				Path:            targetFn,
				Info:            snap,
				RedirectChannel: redirectChannel,
			}, nil
		}
		logger.Debugf("File exists but has wrong hash, ignoring (here).")
	}

	pb := progress.MakeProgressBar(tsto.Stdout)
	defer pb.Finished()

	// Intercept sigint
	c := make(chan os.Signal, 3)
	signal.Notify(c, syscall.SIGINT)
	go func() {
		<-c
		pb.Finished()
		os.Exit(1)
	}()

	dlOpts := &store.DownloadOptions{LeavePartialOnError: opts.LeavePartialOnError}
	if err = tsto.sto.Download(context.TODO(), snap.SnapName(), targetFn, &snap.DownloadInfo, pb, tsto.user, dlOpts); err != nil {
		return nil, err
	}

	signal.Reset(syscall.SIGINT)

	return &DownloadedSnap{
		Path:            targetFn,
		Info:            snap,
		RedirectChannel: redirectChannel,
	}, nil
}

type SnapToDownload struct {
	Snap      naming.SnapRef
	Channel   string
	CohortKey string
}

type CurrentSnap struct {
	SnapName string
	SnapID   string
	Revision snap.Revision
	Channel  string
	Epoch    snap.Epoch
}

type DownloadManyOptions struct {
	BeforeDownloadFunc func(*snap.Info) (targetPath string, err error)
	EnforceValidation  bool
}

// DownloadMany downloads the specified snaps.
// curSnaps are meant to represent already downloaded snaps that will
// be installed in conjunction with the snaps to download, this is needed
// if enforcing validations (ops.EnforceValidation set to true) to
// have cross-gating work.
func (tsto *ToolingStore) DownloadMany(toDownload []SnapToDownload, curSnaps []*CurrentSnap, opts DownloadManyOptions) (downloadedSnaps map[string]*DownloadedSnap, err error) {
	if len(toDownload) == 0 {
		// nothing to do
		return nil, nil
	}
	if opts.BeforeDownloadFunc == nil {
		return nil, fmt.Errorf("internal error: DownloadManyOptions.BeforeDownloadFunc must be set")
	}

	actionFlag := store.SnapActionIgnoreValidation
	if opts.EnforceValidation {
		actionFlag = store.SnapActionEnforceValidation
	}

	downloadedSnaps = make(map[string]*DownloadedSnap, len(toDownload))
	current := make([]*store.CurrentSnap, 0, len(curSnaps))
	for _, csnap := range curSnaps {
		ch := "stable"
		if csnap.Channel != "" {
			ch = csnap.Channel
		}
		current = append(current, &store.CurrentSnap{
			InstanceName:     csnap.SnapName,
			SnapID:           csnap.SnapID,
			Revision:         csnap.Revision,
			TrackingChannel:  ch,
			Epoch:            csnap.Epoch,
			IgnoreValidation: !opts.EnforceValidation,
		})
	}

	actions := make([]*store.SnapAction, 0, len(toDownload))
	for _, sn := range toDownload {
		actions = append(actions, &store.SnapAction{
			Action:       "download",
			InstanceName: sn.Snap.SnapName(), // XXX consider using snap-id first
			Channel:      sn.Channel,
			CohortKey:    sn.CohortKey,
			Flags:        actionFlag,
		})
	}

	sars, _, err := tsto.sto.SnapAction(context.TODO(), current, actions, nil, tsto.user, nil)
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return nil, err
	}

	for _, sar := range sars {
		targetPath, err := opts.BeforeDownloadFunc(sar.Info)
		if err != nil {
			return nil, err
		}
		dlSnap, err := tsto.snapDownload(targetPath, &sar, DownloadSnapOptions{})
		if err != nil {
			return nil, err
		}
		downloadedSnaps[sar.SnapName()] = dlSnap
	}

	return downloadedSnaps, nil
}

// AssertionFetcher creates an asserts.Fetcher for assertions using dlOpts for authorization, the fetcher will add assertions in the given database and after that also call save for each of them.
func (tsto *ToolingStore) AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher {
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return tsto.sto.Assertion(ref.Type, ref.PrimaryKey, tsto.user)
	}
	save2 := func(a asserts.Assertion) error {
		// for checking
		err := db.Add(a)
		if err != nil {
			if _, ok := err.(*asserts.RevisionError); ok {
				return nil
			}
			return fmt.Errorf("cannot add assertion %v: %v", a.Ref(), err)
		}
		return save(a)
	}
	return asserts.NewFetcher(db, retrieve, save2)
}

// Find provides the snapsserts.Finder interface for snapasserts.DerviceSideInfo
func (tsto *ToolingStore) Find(at *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error) {
	pk, err := asserts.PrimaryKeyFromHeaders(at, headers)
	if err != nil {
		return nil, err
	}
	return tsto.sto.Assertion(at, pk, tsto.user)
}

// MockToolingStore creates a ToolingStore that uses the provided StoreImpl
// implementation for Download, SnapAction and Assertion methods.
// For testing.
func MockToolingStore(sto StoreImpl) *ToolingStore {
	return &ToolingStore{sto: sto}
}
