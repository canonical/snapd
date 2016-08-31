// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type cmdDownload struct {
	channelMixin
	Revision string `long:"revision" description:"Download the given revision of a snap, to which you must have developer access"`

	Positional struct {
		Snap string `positional-arg-name:"<snap>" description:"snap name"`
	} `positional-args:"true" required:"true"`
}

var shortDownloadHelp = i18n.G("Download a given snap")
var longDownloadHelp = i18n.G(`
The download command will download the given snap to the current directory.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	})
}

func fetchSnapAssertions(sto *store.Store, snapFn string) (string, error) {
	// TODO: share some of this code
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		KeypairManager: asserts.NewMemoryKeypairManager(),
		Backstore:      asserts.NewMemoryBackstore(),
		Trusted:        sysdb.Trusted(),
	})
	if err != nil {
		return "", err
	}

	assertsFn := snapFn + ".asserts"
	w, err := os.Create(assertsFn)
	if err != nil {
		return "", fmt.Errorf("cannot create assertions file: %s", err)
	}
	defer w.Close()

	encoder := asserts.NewEncoder(w)
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return sto.Assertion(ref.Type, ref.PrimaryKey, nil)
	}
	save := func(a asserts.Assertion) error {
		return encoder.Encode(a)
	}
	f := asserts.NewFetcher(db, retrieve, save)

	sha3_384, _, err := asserts.SnapFileSHA3_384(snapFn)
	if err != nil {
		return "", err
	}
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{sha3_384},
	}
	if err := f.Fetch(ref); err != nil {
		return "", fmt.Errorf("cannot fetch assertion %s: %s", ref, err)
	}
	// FIXME: cross check assertions here?

	return assertsFn, nil
}

func (x *cmdDownload) Execute(args []string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}

	if len(args) > 0 {
		return ErrExtraArgs
	}

	var revision snap.Revision
	if x.Revision == "" {
		revision = snap.R(0)
	} else {
		var err error
		revision, err = snap.ParseRevision(x.Revision)
		if err != nil {
			return err
		}
	}

	snapName := x.Positional.Snap

	// FIXME: set auth context
	var authContext auth.AuthContext
	var user *auth.UserState

	sto := store.New(nil, authContext)
	// we always allow devmode for downloads
	devMode := true
	snap, err := sto.Snap(snapName, x.Channel, devMode, revision, user)
	if err != nil {
		return err
	}

	fmt.Printf("Fetching snap %s\n", snapName)
	pb := progress.NewTextProgress()
	tmpName, err := sto.Download(snapName, &snap.DownloadInfo, pb, user)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)

	fmt.Printf("Fetching assertions for %s\n", snapName)
	tmpAssertsFn, err := fetchSnapAssertions(sto, tmpName)
	if err != nil {
		return err
	}
	defer os.Remove(tmpAssertsFn)

	// copy files into the working directory
	targetFn := filepath.Base(snap.MountFile())
	if err := osutil.CopyFile(tmpAssertsFn, targetFn+".asserts", 0); err != nil {
		return err
	}
	return osutil.CopyFile(tmpName, targetFn, 0)
}
