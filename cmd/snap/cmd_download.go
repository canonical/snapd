// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

type cmdDownload struct {
	channelMixin
	Revision string `long:"revision"`

	Positional struct {
		Snap remoteSnapName
	} `positional-args:"true" required:"true"`
}

var shortDownloadHelp = i18n.G("Download the given snap")
var longDownloadHelp = i18n.G(`
The download command downloads the given snap and its supporting assertions
to the current directory with .snap and .assert file extensions, respectively.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	}, channelDescs.also(map[string]string{
		"revision": i18n.G("Download the given revision of a snap, to which you must have developer access"),
	}), []argDesc{{
		name: "<snap>",
		// TRANSLATORS: This should probably not start with a lowercase letter.
		desc: i18n.G("Snap name"),
	}})
}

func fetchSnapAssertions(tsto *image.ToolingStore, snapPath string, snapInfo *snap.Info) (string, error) {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	})
	if err != nil {
		return "", err
	}

	assertPath := strings.TrimSuffix(snapPath, filepath.Ext(snapPath)) + ".assert"
	w, err := os.Create(assertPath)
	if err != nil {
		return "", fmt.Errorf(i18n.G("cannot create assertions file: %v"), err)
	}
	defer w.Close()

	encoder := asserts.NewEncoder(w)
	save := func(a asserts.Assertion) error {
		return encoder.Encode(a)
	}
	f := tsto.AssertionFetcher(db, save)

	_, err = image.FetchAndCheckSnapAssertions(snapPath, snapInfo, f, db)
	return assertPath, err
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
		if x.Channel != "" {
			return fmt.Errorf(i18n.G("cannot specify both channel and revision"))
		}
		var err error
		revision, err = snap.ParseRevision(x.Revision)
		if err != nil {
			return err
		}
	}

	snapName := string(x.Positional.Snap)

	tsto, err := image.NewToolingStore()
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Fetching snap %q\n"), snapName)
	dlOpts := image.DownloadOptions{
		TargetDir: "", // cwd
		Channel:   x.Channel,
	}
	snapPath, snapInfo, err := tsto.DownloadSnap(snapName, revision, &dlOpts)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Fetching assertions for %q\n"), snapName)
	assertPath, err := fetchSnapAssertions(tsto, snapPath, snapInfo)
	if err != nil {
		return err
	}

	// simplify paths
	wd, _ := os.Getwd()
	if p, err := filepath.Rel(wd, assertPath); err == nil {
		assertPath = p
	}
	if p, err := filepath.Rel(wd, snapPath); err == nil {
		snapPath = p
	}
	// add a hint what to do with the downloaded snap (LP:1676707)
	fmt.Fprintf(Stdout, i18n.G(`Install the snap with:
   snap ack %s
   snap install %s
`), assertPath, snapPath)

	return nil
}
