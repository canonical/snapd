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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"

	"golang.org/x/net/context"
)

// A Store can find metadata on snaps, download snaps and fetch assertions.
type Store interface {
	SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error)
	Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) error

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
	if authFn := os.Getenv("UBUNTU_STORE_AUTH"); authFn != "" {
		var err error
		user, err = readAuthFile(authFn)
		if err != nil {
			return nil, err
		}
	}
	sto := store.New(cfg, nil)
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
	f, err := os.Open(authFn)
	if err != nil {
		return nil, fmt.Errorf("cannot open auth file %q: %v", authFn, err)
	}
	defer f.Close()

	var creds authData
	dec := json.NewDecoder(f)
	if err := dec.Decode(&creds); err != nil {
		return nil, fmt.Errorf("cannot decode auth file %q: %v", authFn, err)
	}
	if creds.Macaroon == "" || len(creds.Discharges) == 0 {
		return nil, fmt.Errorf("invalid auth file %q: missing fields", authFn)
	}
	return &auth.UserState{
		StoreMacaroon:   creds.Macaroon,
		StoreDischarges: creds.Discharges,
	}, nil
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

	spec := store.SnapSpec{
		Name:     name,
		Channel:  opts.Channel,
		Revision: revision,
	}
	snap, err := sto.SnapInfo(spec, tsto.user)
	if err != nil {
		return "", nil, fmt.Errorf("cannot find snap %q: %v", name, err)
	}

	baseName := filepath.Base(snap.MountFile())
	targetFn = filepath.Join(targetDir, baseName)

	pb := progress.NewTextProgress()
	if err = sto.Download(context.TODO(), name, targetFn, &snap.DownloadInfo, pb, tsto.user); err != nil {
		return "", nil, err
	}

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
	if err := snapasserts.CrossCheck(info.Name(), sha3_384, size, &info.SideInfo, db); err != nil {
		return nil, err
	}

	a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": info.SnapID,
	})
	if err != nil {
		return nil, fmt.Errorf("internal error: lost snap declaration for %q: %v", info.Name(), err)
	}
	return a.(*asserts.SnapDeclaration), nil
}
