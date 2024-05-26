// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2023 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store/tooling"
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

var (
	shortDownloadHelp = i18n.G("Download the given snap")
	longDownloadHelp  = i18n.G(`
The download command downloads the given snap and its supporting assertions
to the current directory with .snap and .assert file extensions, respectively.
`)
)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	}, channelDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"revision": i18n.G("Download the given revision of a snap"),
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

func fetchSnapAssertionsDirect(tsto *tooling.ToolingStore, snapPath string, snapInfo *snap.Info) (string, error) {
	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	}))

	assertPath := strings.TrimSuffix(snapPath, filepath.Ext(snapPath)) + ".assert"
	w := mylog.Check2(os.Create(assertPath))

	defer w.Close()

	encoder := asserts.NewEncoder(w)
	save := func(a asserts.Assertion) error {
		return encoder.Encode(a)
	}
	f := tsto.AssertionFetcher(db, save)

	_ = mylog.Check2(image.FetchAndCheckSnapAssertions(snapPath, snapInfo, nil, f, db))
	return assertPath, err
}

func printInstallHint(assertPath, snapPath string) {
	// simplify paths
	wd, _ := os.Getwd()
	if p := mylog.Check2(filepath.Rel(wd, assertPath)); err == nil {
		assertPath = p
	}
	if p := mylog.Check2(filepath.Rel(wd, snapPath)); err == nil {
		snapPath = p
	}
	// add a hint what to do with the downloaded snap (LP:1676707)
	fmt.Fprintf(Stdout, i18n.G(`Install the snap with:
   snap ack %s
   snap install %s
`), assertPath, snapPath)
}

// for testing
var downloadDirect = downloadDirectImpl

func downloadDirectImpl(snapName string, revision snap.Revision, dlOpts tooling.DownloadSnapOptions) error {
	tsto := mylog.Check2(tooling.NewToolingStore())

	tsto.Stdout = Stdout

	fmt.Fprintf(Stdout, i18n.G("Fetching snap %q\n"), snapName)
	dlSnap := mylog.Check2(tsto.DownloadSnap(snapName, dlOpts))

	fmt.Fprintf(Stdout, i18n.G("Fetching assertions for %q\n"), snapName)
	assertPath := mylog.Check2(fetchSnapAssertionsDirect(tsto, dlSnap.Path, dlSnap.Info))

	printInstallHint(assertPath, dlSnap.Path)
	return nil
}

func (x *cmdDownload) downloadFromStore(snapName string, revision snap.Revision) error {
	dlOpts := tooling.DownloadSnapOptions{
		TargetDir: x.TargetDir,
		Basename:  x.Basename,
		Channel:   x.Channel,
		CohortKey: x.CohortKey,
		Revision:  revision,
		// if something goes wrong, don't force it to start over again
		LeavePartialOnError: true,
	}
	return downloadDirect(snapName, revision, dlOpts)
}

func (x *cmdDownload) Execute(args []string) error {
	if strings.ContainsRune(x.Basename, filepath.Separator) {
		return fmt.Errorf(i18n.G("cannot specify a path in basename (use --target-dir for that)"))
	}
	mylog.Check(x.setChannelFromCommandline())

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

		revision = mylog.Check2(snap.ParseRevision(x.Revision))

	}

	snapName := string(x.Positional.Snap)
	return x.downloadFromStore(snapName, revision)
}
