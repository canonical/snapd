// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

package image

// TODO: put these in appropriate package(s) once they are clarified a bit more

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// A Store can find metadata on snaps, download snaps and fetch assertions.
type Store interface {
	SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, error)
	Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)
}

// ToolingStore wraps access to the store for tools.
type ToolingStore struct {
	sto  Store
	user *auth.UserState
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
	if osutil.GetenvBool("SNAPPY_USE_STAGING_STORE") {
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

func NewToolingStoreFromModel(model *asserts.Model, fallbackArchitecture string) (*ToolingStore, error) {
	architecture := model.Architecture()
	// can happen on classic
	if architecture == "" {
		architecture = fallbackArchitecture
	}
	return newToolingStore(architecture, model.Store())
}

func NewToolingStore() (*ToolingStore, error) {
	arch := os.Getenv("UBUNTU_STORE_ARCH")
	storeID := os.Getenv("UBUNTU_STORE_ID")
	return newToolingStore(arch, storeID)
}

// DownloadOptions carries options for downloading snaps plus assertions.
type DownloadOptions struct {
	TargetDir string
	// if TargetPathFunc is not nil it will be invoked
	// to compute the target path for the download and TargetDir is
	// ignored
	TargetPathFunc func(*snap.Info) (string, error)

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

func (opts *DownloadOptions) validate() error {
	if strings.ContainsRune(opts.Basename, filepath.Separator) {
		return errPathInBase
	}
	if !(opts.Revision.Unset() || opts.CohortKey == "") {
		return errRevisionAndCohort
	}
	return nil
}

func (opts *DownloadOptions) String() string {
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

// DownloadSnap downloads the snap with the given name and optionally revision
// using the provided store and options. It returns the final full path of the
// snap inside the opts.TargetDir and a snap.Info for the snap.
func (tsto *ToolingStore) DownloadSnap(name string, opts DownloadOptions) (targetFn string, info *snap.Info, err error) {
	if err := opts.validate(); err != nil {
		return "", nil, err
	}
	sto := tsto.sto

	if opts.TargetPathFunc == nil && opts.TargetDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
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

	sars, err := sto.SnapAction(context.TODO(), nil, actions, tsto.user, nil)
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return "", nil, err
	}
	snap := sars[0].Info

	if opts.TargetPathFunc == nil {
		baseName := opts.Basename
		if baseName == "" {
			baseName = snap.Filename()
		} else {
			baseName += ".snap"
		}
		targetFn = filepath.Join(opts.TargetDir, baseName)
	} else {
		var err error
		targetFn, err = opts.TargetPathFunc(snap)
		if err != nil {
			return "", nil, err
		}
	}

	// check if we already have the right file
	if osutil.FileExists(targetFn) {
		sha3_384Dgst, size, err := osutil.FileDigest(targetFn, crypto.SHA3_384)
		if err == nil && size == uint64(snap.DownloadInfo.Size) && fmt.Sprintf("%x", sha3_384Dgst) == snap.DownloadInfo.Sha3_384 {
			logger.Debugf("not downloading, using existing file %s", targetFn)
			return targetFn, snap, nil
		}
		logger.Debugf("File exists but has wrong hash, ignoring (here).")
	}

	pb := progress.MakeProgressBar()
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
	if err = sto.Download(context.TODO(), name, targetFn, &snap.DownloadInfo, pb, tsto.user, dlOpts); err != nil {
		return "", nil, err
	}

	signal.Reset(syscall.SIGINT)

	return targetFn, snap, nil
}

// AssertionFetcher creates an asserts.Fetcher for assertions against the given store using dlOpts for authorization, the fetcher will add assertions in the given database and after that also call save for each of them.
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

// FetchAndCheckSnapAssertions fetches and cross checks the snap assertions matching the given snap file using the provided asserts.Fetcher and assertion database.
func FetchAndCheckSnapAssertions(snapPath string, info *snap.Info, f asserts.Fetcher, db asserts.RODatabase) (*asserts.SnapDeclaration, error) {
	sha3_384, size, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return nil, err
	}

	// this assumes series "16"
	if err := snapasserts.FetchSnapAssertions(f, sha3_384); err != nil {
		return nil, fmt.Errorf("cannot fetch snap signatures/assertions: %v", err)
	}

	// cross checks
	if err := snapasserts.CrossCheck(info.InstanceName(), sha3_384, size, &info.SideInfo, db); err != nil {
		return nil, err
	}

	a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": info.SnapID,
	})
	if err != nil {
		return nil, fmt.Errorf("internal error: lost snap declaration for %q: %v", info.InstanceName(), err)
	}
	return a.(*asserts.SnapDeclaration), nil
}

// Find provides the snapsserts.Finder interface for snapasserts.DerviceSideInfo
func (tsto *ToolingStore) Find(at *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error) {
	pk, err := asserts.PrimaryKeyFromHeaders(at, headers)
	if err != nil {
		return nil, err
	}
	return tsto.sto.Assertion(at, pk, tsto.user)
}
