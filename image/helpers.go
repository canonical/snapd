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
	"crypto"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mvo5/goconfigparser"
	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// A Store can find metadata on snaps, download snaps and fetch assertions.
type Store interface {
	SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, *auth.UserState, *store.RefreshOptions) ([]*snap.Info, error)
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
	sto := store.New(cfg, toolingAuthContext{})
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

// toolingAuthContext implements trivially auth.AuthContext except
// implementing UpdateUserAuth properly to be used to refresh a
// soft-expired user macaroon.
type toolingAuthContext struct{}

func (tac toolingAuthContext) CloudInfo() (*auth.CloudInfo, error) {
	return nil, nil
}

func (tac toolingAuthContext) Device() (*auth.DeviceState, error) {
	return &auth.DeviceState{}, nil
}

func (tac toolingAuthContext) DeviceSessionRequestParams(_ string) (*auth.DeviceSessionRequestParams, error) {
	return nil, auth.ErrNoSerial
}

func (tac toolingAuthContext) ProxyStoreParams(defaultURL *url.URL) (proxyStoreID string, proxySroreURL *url.URL, err error) {
	return "", defaultURL, nil
}

func (tac toolingAuthContext) StoreID(fallback string) (string, error) {
	return fallback, nil
}

func (tac toolingAuthContext) UpdateDeviceAuth(_ *auth.DeviceState, newSessionMacaroon string) (*auth.DeviceState, error) {
	return nil, fmt.Errorf("internal error: no device state in tools")
}

func (tac toolingAuthContext) UpdateUserAuth(user *auth.UserState, discharges []string) (*auth.UserState, error) {
	user.StoreDischarges = discharges
	return user, nil
}

func NewToolingStoreFromModel(model *asserts.Model) (*ToolingStore, error) {
	return newToolingStore(model.Architecture(), model.Store())
}

func NewToolingStore() (*ToolingStore, error) {
	arch := os.Getenv("UBUNTU_STORE_ARCH")
	storeID := os.Getenv("UBUNTU_STORE_ID")
	return newToolingStore(arch, storeID)
}

// DownloadOptions carries options for downloading snaps plus assertions.
type DownloadOptions struct {
	TargetDir string
	Channel   string
}

// DownloadSnap downloads the snap with the given name and optionally revision  using the provided store and options. It returns the final full path of the snap inside the opts.TargetDir and a snap.Info for the snap.
func (tsto *ToolingStore) DownloadSnap(name string, revision snap.Revision, opts *DownloadOptions) (targetFn string, info *snap.Info, err error) {
	if opts == nil {
		opts = &DownloadOptions{}
	}
	sto := tsto.sto

	targetDir := opts.TargetDir
	if targetDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		targetDir = pwd
	}

	logger.Debugf("Going to download snap %q (%s) from channel %q to %q.", name, revision, opts.Channel, opts.TargetDir)

	actions := []*store.SnapAction{{
		Action:       "download",
		InstanceName: name,
		Revision:     revision,
	}}

	if revision.Unset() {
		actions[0].Channel = opts.Channel
	}

	snaps, err := sto.SnapAction(context.TODO(), nil, actions, tsto.user, nil)
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return "", nil, err
	}
	snap := snaps[0]

	baseName := filepath.Base(snap.MountFile())
	targetFn = filepath.Join(targetDir, baseName)

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

	if err = sto.Download(context.TODO(), name, targetFn, &snap.DownloadInfo, pb, tsto.user, nil); err != nil {
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
