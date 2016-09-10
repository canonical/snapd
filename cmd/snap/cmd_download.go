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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/overlord/auth"
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
The download command will download the given snap and its supporting assertions to the current directory.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	})
}

func fetchSnapAssertions(sto *store.Store, snapPath string, snapInfo *snap.Info, dlOpts *image.DownloadOptions) error {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	})
	if err != nil {
		return err
	}

	w, err := os.Create(snapPath + ".assertions")
	if err != nil {
		return fmt.Errorf("cannot create assertions file: %v", err)
	}
	defer w.Close()

	encoder := asserts.NewEncoder(w)
	save := func(a asserts.Assertion) error {
		return encoder.Encode(a)
	}
	f := image.StoreAssertionFetcher(sto, dlOpts, db, save)

	return image.FetchSnapAssertions(snapPath, snapInfo, f, db)
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

	dlOpts := image.DownloadOptions{
		TargetDir: "", // cwd
		DevMode:   devMode,
		Channel:   x.Channel,
		User:      user,
	}

	fmt.Fprintf(Stderr, "Fetching snap %s\n", snapName)
	snapPath, snapInfo, err := image.DownloadSnap(sto, snapName, revision, &dlOpts)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stderr, "Fetching assertions for %s\n", snapName)
	err = fetchSnapAssertions(sto, snapPath, snapInfo, &dlOpts)
	if err != nil {
		return err
	}

	return nil
}
