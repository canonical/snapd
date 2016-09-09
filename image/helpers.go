// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

// DownloadOptions carries options for downloading snaps plus assertions.
type DownloadOptions struct {
	TargetDir string
	Channel   string
	DevMode   bool
	User      *auth.UserState
}

// A Store can find metadata on snaps, download snaps and fetch assertions.
type Store interface {
	Snap(name, channel string, devmode bool, revision snap.Revision, user *auth.UserState) (*snap.Info, error)
	Download(name string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState) (path string, err error)

	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)
}

// DownloadSnap downloads the snap with the given name and optionally revision  using the provided store and options. It returns the final full path of the snap inside the opts.TargetDir and a snap.Info for the snap.
func DownloadSnap(sto Store, name string, revision snap.Revision, opts *DownloadOptions) (targetPath string, info *snap.Info, err error) {
	if opts == nil {
		opts = &DownloadOptions{}
	}

	targetDir := opts.TargetDir
	if targetDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return "", nil, err
		}
		targetDir = pwd
	}

	snap, err := sto.Snap(name, opts.Channel, opts.DevMode, revision, opts.User)
	if err != nil {
		return "", nil, fmt.Errorf("cannot find snap %q: %v", name, err)
	}
	pb := progress.NewTextProgress()
	tmpName, err := sto.Download(name, &snap.DownloadInfo, pb, opts.User)
	if err != nil {
		return "", nil, err
	}
	defer os.Remove(tmpName)

	baseName := filepath.Base(snap.MountFile())
	targetPath = filepath.Join(targetDir, baseName)
	if err := osutil.CopyFile(tmpName, targetPath, 0); err != nil {
		return "", nil, err
	}

	return targetPath, snap, nil
}

// FetchSnapAssertions fetches and cross checks the snap assertions matching the given snap file using the provided asserts.Fetcher and assertion database.
func FetchSnapAssertions(snapPath string, info *snap.Info, f *asserts.Fetcher, db asserts.RODatabase) error {
	sha3_384, size, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return err
	}
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{sha3_384},
	}
	if err := f.Fetch(ref); err != nil {
		return fmt.Errorf("cannot fetch assertion %v: %v", ref, err)
	}

	// cross checks
	return snapasserts.CrossCheck(info.Name(), sha3_384, size, &info.SideInfo, db)
}
