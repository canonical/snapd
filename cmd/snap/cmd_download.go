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
	Revision  string `long:"revision"`
	Basename  string `long:"basename"`
	TargetDir string `long:"target-directory"`

	CohortKey  string `long:"cohort"`
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
		// TRANSLATORS: This should not start with a lowercase letter.
		"revision": i18n.G("Download the given revision of a snap, to which you must have developer access"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"cohort": i18n.G("Download from the given cohort"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"basename": i18n.G("Use this basename for the snap and assertion files (defaults to <snap>_<revision>)"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"target-directory": i18n.G("Download to this directory (defaults to the current directory)"),
	}), []argDesc{{
		name: "<snap>",
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Snap name"),
	}})
}

// downloadStore is enough to support direct snap downloads
type downloadStore interface {
	DownloadSnap(name string, opts image.DownloadOptions) (targetFn string, info *snap.Info, err error)
	AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher
}

var (
	cmdDownloadTrusted = sysdb.Trusted()
	newDownloadStore   = func() (downloadStore, error) {
		return image.NewToolingStore()
	}
)

func fetchSnapAssertions(tsto downloadStore, snapPath string, snapInfo *snap.Info) (string, error) {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   cmdDownloadTrusted,
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

	save := func(a asserts.Assertion) error {
		if err := db.Add(a); err != nil && !asserts.IsUnaccceptedUpdate(err) {
			return err
		}
		return nil
	}
	f := tsto.AssertionFetcher(db, save)

	_, err = image.FetchAndCheckSnapAssertions(snapPath, snapInfo, f, db)
	return assertPath, err
}

func printInstallHint(assertPath, snapPath string) {
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
}

func (x *cmdDownload) Execute(args []string) error {
	if strings.ContainsRune(x.Basename, filepath.Separator) {
		return fmt.Errorf(i18n.G("cannot specify a path in basename (use --target-dir for that)"))
	}
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
		if x.CohortKey != "" {
			return fmt.Errorf(i18n.G("cannot specify both cohort and revision"))
		}
		var err error
		revision, err = snap.ParseRevision(x.Revision)
		if err != nil {
			return err
		}
	}

	snapName := string(x.Positional.Snap)

	tsto, err := newDownloadStore()
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Fetching snap %q\n"), snapName)
	dlOpts := image.DownloadOptions{
		TargetDir: x.TargetDir,
		Basename:  x.Basename,
		Channel:   x.Channel,
		CohortKey: x.CohortKey,
		Revision:  revision,
		// if something goes wrong, don't force it to start over again
		LeavePartialOnError: true,
	}
	snapPath, snapInfo, err := tsto.DownloadSnap(snapName, dlOpts)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Fetching assertions for %q\n"), snapName)
	assertPath, err := fetchSnapAssertions(tsto, snapPath, snapInfo)
	if err != nil {
		return err
	}
	printInstallHint(assertPath, snapPath)

	return nil
}
