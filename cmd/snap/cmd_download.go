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
	"errors"
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
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store/tooling"
	"github.com/snapcore/snapd/strutil"
)

type cmdDownload struct {
	channelMixin
	Revision       string `long:"revision"`
	Basename       string `long:"basename"`
	TargetDir      string `long:"target-directory"`
	OnlyComponents bool   `long:"only-components"`

	CohortKey  string `long:"cohort"`
	Positional struct {
		Snap remoteSnapName
	} `positional-args:"true" required:"true"`
}

var shortDownloadHelp = i18n.G("Download the given snap")
var longDownloadHelp = i18n.G(`
The download command downloads the given snap, components, and their supporting
assertions to the current directory with .snap, .comp, and .assert file
extensions, respectively.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	}, channelDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"revision": i18n.G("Download the given revision of a snap. When downloading components, download the components associated with the given snap revision."),
		// TRANSLATORS: This should not start with a lowercase letter.
		"cohort": i18n.G("Download from the given cohort"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"basename": i18n.G("Use this basename for the snap, component, and assertion files (defaults to <snap>_<revision>)"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"target-directory": i18n.G("Download to this directory (defaults to the current directory)"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"only-components": i18n.G("Only download the given components, not the snap"),
	}), []argDesc{{
		name: "<snap[+component...]>",
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Snap and, optionally, component names"),
	}})
}

func printInstallHint(assertPath string, containerPaths []string) {
	// simplify paths
	wd, _ := os.Getwd()
	if p, err := filepath.Rel(wd, assertPath); err == nil {
		assertPath = p
	}

	relativePaths := make([]string, 0, len(containerPaths))
	for _, path := range containerPaths {
		if rel, err := filepath.Rel(wd, path); err == nil {
			relativePaths = append(relativePaths, rel)
		} else {
			relativePaths = append(relativePaths, path)
		}
	}

	// add a hint what to do with the downloaded snap (LP:1676707)
	fmt.Fprintf(Stdout, i18n.G(`Install the snap with:
   snap ack %s
   snap install %s
`), assertPath, strings.Join(relativePaths, " "))
}

func downloadDirect(snapName string, components []string, opts tooling.DownloadSnapOptions) error {
	compRefs := make([]string, 0, len(components))
	for _, comp := range components {
		compRefs = append(compRefs, naming.NewComponentRef(snapName, comp).String())
	}

	if opts.OnlyComponents {
		if len(compRefs) == 1 {
			fmt.Fprintf(Stdout, i18n.G("Fetching component %q\n"), compRefs[0])
		} else {
			fmt.Fprintf(Stdout, i18n.G("Fetching components %s\n"), strutil.Quoted(compRefs))
		}
	} else {
		switch len(components) {
		case 0:
			fmt.Fprintf(Stdout, i18n.G("Fetching snap %q\n"), snapName)
		case 1:
			fmt.Fprintf(Stdout, i18n.G("Fetching snap %q and component %q\n"), snapName, compRefs[0])
		default:
			fmt.Fprintf(Stdout, i18n.G("Fetching snap %q and components %s\n"), snapName, strutil.Quoted(compRefs))
		}
	}

	tsto, err := tooling.NewToolingStore()
	if err != nil {
		return err
	}
	tsto.Stdout = Stdout

	dl, err := downloadContainers(snapName, components, tsto, opts)
	if err != nil {
		return err
	}

	downloaded := make([]string, 0, len(compRefs)+1)
	if !opts.OnlyComponents {
		downloaded = append(downloaded, snapName)
	}
	downloaded = append(downloaded, compRefs...)

	fmt.Fprintf(Stdout, i18n.G("Fetching assertions for %s\n"), strutil.Quoted(downloaded))

	compInfos := make(map[string]*snap.ComponentInfo, len(components))
	for _, comp := range dl.Components {
		compInfos[comp.Path] = comp.Info
	}

	snapPath := dl.Path

	// if we're only downloading components, then we won't have a snap path to
	// work with. since downloadAssertions derives where it downloads the
	// assertions to based on the snap path, we need to set it to something
	// appropriate here.
	if opts.OnlyComponents {
		if opts.Basename == "" {
			snapPath = fmt.Sprintf("%s_%s.snap", dl.Info.SnapName(), dl.Info.Revision)
		} else {
			snapPath = fmt.Sprintf("%s.snap", opts.Basename)
		}
	}

	assertsPath, err := downloadAssertions(dl.Info, snapPath, compInfos, tsto, opts)
	if err != nil {
		return err
	}

	containerPaths := make([]string, 0, len(components)+1)
	if !opts.OnlyComponents {
		containerPaths = append(containerPaths, dl.Path)
	}
	for _, c := range dl.Components {
		containerPaths = append(containerPaths, c.Path)
	}

	printInstallHint(assertsPath, containerPaths)

	return nil
}

var (
	downloadAssertions = downloadAssertionsImpl
	downloadContainers = downloadContainersImpl
)

func downloadContainersImpl(snapName string, components []string, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (*tooling.DownloadedSnap, error) {
	dl, err := tsto.DownloadSnap(snapName, components, opts)
	if err != nil {
		return nil, err
	}
	return dl, nil
}

func downloadAssertionsImpl(
	info *snap.Info,
	snapPath string,
	components map[string]*snap.ComponentInfo,
	tsto *tooling.ToolingStore,
	opts tooling.DownloadSnapOptions,
) (string, error) {
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

	comps := make([]image.CompInfoPath, 0, len(components))
	for path, ci := range components {
		comps = append(comps, image.CompInfoPath{
			Info: ci,
			Path: path,
		})
	}

	if !opts.OnlyComponents {
		_, err := image.FetchAndCheckSnapAssertions(snapPath, info, comps, nil, f, db)
		if err != nil {
			return "", err
		}
	} else {
		for _, c := range comps {
			if err := image.FetchAndCheckComponentAssertions(c, info, nil, f, db); err != nil {
				return "", err
			}
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return assertPath, nil
}

func (x *cmdDownload) downloadFromStore(snap string, comps []string, revision snap.Revision) error {
	return downloadDirect(snap, comps, tooling.DownloadSnapOptions{
		TargetDir: x.TargetDir,
		Basename:  x.Basename,
		Channel:   x.Channel,
		CohortKey: x.CohortKey,
		Revision:  revision,
		// if something goes wrong, don't force it to start over again
		LeavePartialOnError: true,
		OnlyComponents:      x.OnlyComponents,
	})
}

func (x *cmdDownload) Execute(args []string) error {
	if strings.ContainsRune(x.Basename, filepath.Separator) {
		return errors.New(i18n.G("cannot specify a path in basename (use --target-dir for that)"))
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
			return errors.New(i18n.G("cannot specify both channel and revision"))
		}
		if x.CohortKey != "" {
			return errors.New(i18n.G("cannot specify both cohort and revision"))
		}
		var err error
		revision, err = snap.ParseRevision(x.Revision)
		if err != nil {
			return err
		}
	}

	snap, comps := snap.SplitSnapInstanceAndComponents(string(x.Positional.Snap))
	if x.OnlyComponents && len(comps) == 0 {
		return errors.New(i18n.G("cannot specify --only-components without providing any components;"))
	}

	return x.downloadFromStore(snap, comps, revision)
}
