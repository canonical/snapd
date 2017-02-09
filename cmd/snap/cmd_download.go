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
	"strings"

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
	Revision string `long:"revision"`
	StoreID string `long:"store-id"`

	Positional struct {
		Snap remoteSnapName
	} `positional-args:"true" required:"true"`
}

var shortDownloadHelp = i18n.G("Downloads the given snap")
var longDownloadHelp = i18n.G(`
The download command downloads the given snap and its supporting assertions
to the current directory under .snap and .assert file extensions, respectively.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	}, channelDescs.also(map[string]string{
		"revision": i18n.G("Download the given revision of a snap, to which you must have developer access"),
		"store-id": i18n.G("Download snap from the given store, to which you must have access"),
	}), []argDesc{{
		name: "<snap>",
		desc: i18n.G("Snap name"),
	}})
}

func fetchSnapAssertions(sto *store.Store, snapPath string, snapInfo *snap.Info, dlOpts *image.DownloadOptions) error {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	})
	if err != nil {
		return err
	}

	assertPath := strings.TrimSuffix(snapPath, filepath.Ext(snapPath)) + ".assert"
	w, err := os.Create(assertPath)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot create assertions file: %v"), err)
	}
	defer w.Close()

	encoder := asserts.NewEncoder(w)
	save := func(a asserts.Assertion) error {
		return encoder.Encode(a)
	}
	f := image.StoreAssertionFetcher(sto, dlOpts, db, save)

	_, err = image.FetchAndCheckSnapAssertions(snapPath, snapInfo, f, db)
	return err
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

	snapName := string(x.Positional.Snap)

	// FIXME: set auth context
	var authContext auth.AuthContext
	var user *auth.UserState

	var cfg *store.Config = nil
	if x.StoreID != "" {
		cfg = store.DefaultConfig()
		cfg.StoreID = x.StoreID;
	}

	sto := store.New(cfg, authContext)
	// we always allow devmode for downloads
	devMode := true

	dlOpts := image.DownloadOptions{
		TargetDir: "", // cwd
		DevMode:   devMode,
		Channel:   x.Channel,
		User:      user,
	}

	fmt.Fprintf(Stderr, i18n.G("Fetching snap %q\n"), snapName)
	snapPath, snapInfo, err := image.DownloadSnap(sto, snapName, revision, &dlOpts)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stderr, i18n.G("Fetching assertions for %q\n"), snapName)
	err = fetchSnapAssertions(sto, snapPath, snapInfo, &dlOpts)
	if err != nil {
		return err
	}

	return nil
}
