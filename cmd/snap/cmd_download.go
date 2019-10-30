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
	"crypto"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/jessevdk/go-flags"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
)

type cmdDownload struct {
	channelMixin
	clientMixin

	Revision  string `long:"revision"`
	Basename  string `long:"basename"`
	TargetDir string `long:"target-directory"`
	Direct    bool   `long:"direct"`

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
		// TRANSLATORS: This should not start with a lowercase letter.
		"direct": i18n.G("Do not try to connect to snapd to download"),
	}), []argDesc{{
		name: "<snap>",
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Snap name"),
	}})
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

func fetchSnapAssertionsDirect(tsto downloadStore, snapPath string, snapInfo *snap.Info) (string, error) {
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

//
type downloadStore interface {
	DownloadSnap(name string, opts image.DownloadOptions) (targetFn string, info *snap.Info, err error)
	AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher
}

var newDownloadStore = func() (downloadStore, error) {
	return image.NewToolingStore()
}

func (x *cmdDownload) downloadDirect(snapName string, revision snap.Revision) error {
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
	assertPath, err := fetchSnapAssertionsDirect(tsto, snapPath, snapInfo)
	if err != nil {
		return err
	}
	printInstallHint(assertPath, snapPath)
	return nil
}

func downloadSnapFromStreamWithProgress(downloadPath string, stream io.ReadCloser, dlInfo *client.DownloadInfo) error {
	// TODO: support resume of exiting files and not download if the
	//       file is already there with the right hash
	f, err := os.OpenFile(downloadPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	// TODO: we have similar code like this in ToolingStore.DownloadSnap
	// and store.downloadImpl

	pbar := progress.MakeProgressBar()
	pbar.Start(filepath.Base(downloadPath), float64(dlInfo.Size))
	defer pbar.Finished()

	// Intercept sigint
	c := make(chan os.Signal, 3)
	signal.Notify(c, syscall.SIGINT)
	go func() {
		<-c
		pbar.Finished()
		stream.Close()
	}()
	defer signal.Reset(syscall.SIGINT)

	h := crypto.SHA3_384.New()
	mw := io.MultiWriter(f, h, pbar)
	if _, err := io.Copy(mw, stream); err != nil {
		return err
	}

	if dlInfo.Sha3_384 != "" && dlInfo.Sha3_384 != fmt.Sprintf("%x", h.Sum(nil)) {
		return fmt.Errorf("unexpected sha3-384 for %s", downloadPath)
	}

	return nil
}

func fetchSnapAssertionsViaSnapd(cli *client.Client, assertFname, hexSha3_384 string) error {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   sysdb.Trusted(),
	})
	if err != nil {
		return err
	}
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		headers, err := asserts.HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey)
		if err != nil {
			return nil, err
		}
		asserts, err := cli.Known(ref.Type.Name, headers, &client.KnownOptions{Remote: true})
		if err != nil {
			return nil, err
		}
		if len(asserts) != 1 {
			return nil, fmt.Errorf("unexpected number of assertions: expected 1, got %d", len(asserts))
		}
		return asserts[0], nil
	}

	w, err := os.Create(assertFname)
	if err != nil {
		return err
	}
	defer w.Close()

	encoder := asserts.NewEncoder(w)
	save := func(a asserts.Assertion) error {
		encoder.Encode(a)
		return db.Add(a)
	}

	rawSha3, err := hex.DecodeString(hexSha3_384)
	if err != nil {
		return err
	}
	sha3_384, err := asserts.EncodeDigest(crypto.SHA3_384, rawSha3)
	if err != nil {
		return err
	}
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{sha3_384},
	}
	fetcher := asserts.NewFetcher(db, retrieve, save)
	if err := fetcher.Fetch(ref); err != nil {
		return err
	}

	// XXX: cross check?
	return nil
}

func (x *cmdDownload) downloadViaSnapd(snapName string, rev snap.Revision) error {
	opts := &client.SnapOptions{
		Channel:   x.Channel,
		CohortKey: x.CohortKey,
		Revision:  rev.String(),
	}
	dlInfo, stream, err := x.client.Download(snapName, opts)
	if err != nil {
		return err
	}
	defer stream.Close()

	fmt.Fprintf(Stdout, i18n.G("Fetching snap %q\n"), snapName)
	fname := dlInfo.SuggestedFileName
	if x.Basename != "" {
		fname = x.Basename + ".snap"
	}
	downloadPath := filepath.Join(x.TargetDir, fname)
	if err := os.MkdirAll(filepath.Dir(downloadPath), 0755); err != nil {
		return err
	}
	if err := downloadSnapFromStreamWithProgress(fname, stream, dlInfo); err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Fetching assertions for %q\n"), snapName)
	assertFname := strings.TrimSuffix(downloadPath, filepath.Ext(downloadPath)) + ".assert"
	if err := fetchSnapAssertionsViaSnapd(x.client, assertFname, dlInfo.Sha3_384); err != nil {
		return err
	}
	printInstallHint(downloadPath, assertFname)

	return nil
}

func isErrorKindLoginRequired(err error) bool {
	var clientErr *client.Error
	if xerrors.As(err, &clientErr) {
		return clientErr.Kind == client.ErrorKindLoginRequired
	}
	return false
}

func isConnectionError(err error) bool {
	var connErr client.ConnectionError
	return xerrors.As(err, &connErr)
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
	// "snap download" works only in --direct mode on non-linux
	if runtime.GOOS != "linux" {
		x.Direct = true
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

	if x.Direct {
		return x.downloadDirect(snapName, revision)
	}

	err := x.downloadViaSnapd(snapName, revision)
	if isConnectionError(err) || isErrorKindLoginRequired(err) {
		fmt.Fprintf(Stderr, i18n.G("Cannot connect to the snapd daemon, trying direct download\n"))
		return x.downloadDirect(snapName, revision)
	}
	return err
}
