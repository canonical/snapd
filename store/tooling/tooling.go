// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package tooling

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

// ToolingStore wraps access to the store for tools.
type ToolingStore struct {
	// Stdout is for output, mainly progress bars
	// left unset stdout is used
	Stdout io.Writer

	sto StoreImpl
	cfg *store.Config

	assertMaxFormats map[string]int
}

// A StoreImpl can find metadata on snaps, download snaps and fetch assertions.
// This interface is a subset of store.Store methods.
type StoreImpl interface {
	// SnapAction queries the store for snap information for the given install/refresh actions. Orthogonally it can be used to fetch or update assertions.
	SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, store.AssertionQuery, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error)

	// Download downloads the snap addressed by download info.
	Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error

	// Assertion retrieves the assertion for the given type and primary key.
	Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error)

	// SeqFormingAssertion retrieves the sequence-forming assertion for the given
	// type (currently validation-set only). For sequence <= 0 we query for the
	// latest sequence, otherwise the latest revision of the given sequence is
	// requested.
	SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error)

	// SetAssertionMaxFormats sets the assertion max formats to send.
	SetAssertionMaxFormats(maxFormats map[string]int)
}

func newToolingStore(arch, storeID string) (*ToolingStore, error) {
	cfg := store.DefaultConfig()
	cfg.Architecture = arch
	cfg.StoreID = storeID
	creds, err := getAuthorizer()
	if err != nil {
		return nil, err
	}
	cfg.Authorizer = creds
	if storeURL := os.Getenv("UBUNTU_STORE_URL"); storeURL != "" {
		u, err := url.Parse(storeURL)
		if err != nil {
			return nil, fmt.Errorf("invalid UBUNTU_STORE_URL: %v", err)
		}
		cfg.StoreBaseURL = u
	}
	sto := store.New(cfg, nil)
	return &ToolingStore{
		sto: sto,
		cfg: cfg,
	}, nil
}

// NewToolingStoreFromModel creates ToolingStore for the snap store used by the given model.
func NewToolingStoreFromModel(model *asserts.Model, fallbackArchitecture string) (*ToolingStore, error) {
	architecture := model.Architecture()
	// can happen on classic
	if architecture == "" {
		architecture = fallbackArchitecture
	}
	return newToolingStore(architecture, model.Store())
}

// NewToolingStore creates ToolingStore, with optional arch and store id
// read from UBUNTU_STORE_ARCH and UBUNTU_STORE_ID environment variables.
func NewToolingStore() (*ToolingStore, error) {
	arch := os.Getenv("UBUNTU_STORE_ARCH")
	storeID := os.Getenv("UBUNTU_STORE_ID")
	return newToolingStore(arch, storeID)
}

// DownloadSnapOptions carries options for downloading snaps plus assertions.
type DownloadSnapOptions struct {
	TargetDir string

	Revision  snap.Revision
	Channel   string
	CohortKey string
	Basename  string

	LeavePartialOnError bool
	// OnlyComponents is set if we only want to download the
	// specified components and not the snap
	OnlyComponents bool
}

var (
	errRevisionAndCohort = errors.New("cannot specify both revision and cohort")
	errPathInBase        = errors.New("cannot specify a path in basename (use target dir for that)")
)

func (opts *DownloadSnapOptions) validate() error {
	if strings.ContainsRune(opts.Basename, filepath.Separator) {
		return errPathInBase
	}
	if !(opts.Revision.Unset() || opts.CohortKey == "") {
		return errRevisionAndCohort
	}
	return nil
}

func (opts *DownloadSnapOptions) String() string {
	spec := make([]string, 0, 5)
	if !opts.Revision.Unset() {
		spec = append(spec, fmt.Sprintf("(%s)", opts.Revision))
	}
	if opts.Channel != "" {
		spec = append(spec, fmt.Sprintf("from channel %q", opts.Channel))
	}
	if opts.CohortKey != "" {
		// cohort keys are really long, and the rightmost bit being the
		// interesting bit, so ellipt the rest
		spec = append(spec, fmt.Sprintf(`from cohort %q`, strutil.ElliptLeft(opts.CohortKey, 10)))
	}
	if opts.Basename != "" {
		spec = append(spec, fmt.Sprintf("to %q", opts.Basename+".snap"))
	}
	if opts.TargetDir != "" {
		spec = append(spec, fmt.Sprintf("in %q", opts.TargetDir))
	}
	return strings.Join(spec, " ")
}

type DownloadedSnap struct {
	Path            string
	Info            *snap.Info
	RedirectChannel string
	Components      []*DownloadedComponent
}

// DownloadSnap downloads the snap/components with the given names and options.
// It returns the final full paths of the snap/components and infos for them
// and optionally a channel the snap got redirected to wrapped in
// DownloadedSnap.
func (tsto *ToolingStore) DownloadSnap(name string, comps []string, opts DownloadSnapOptions) (downloadedSnap *DownloadedSnap, err error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	sto := tsto.sto

	if opts.TargetDir == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		opts.TargetDir = pwd
	}

	compsMsg := ""
	if len(comps) > 0 {
		compsMsg = fmt.Sprintf(" and component(s) %s", strutil.Quoted(comps))
	}
	logger.Debugf("Going to download snap %q%s %s.", name, compsMsg, &opts)

	actions := []*store.SnapAction{{
		Action:       "download",
		InstanceName: name,
		Revision:     opts.Revision,
		CohortKey:    opts.CohortKey,
		Channel:      opts.Channel,
	}}

	sars, _, err := sto.SnapAction(context.TODO(), nil, actions, nil, nil,
		&store.RefreshOptions{IncludeResources: true})
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return nil, err
	}
	sar := &sars[0]

	baseName := opts.Basename
	if baseName == "" {
		baseName = sar.Info.Filename()
	} else {
		baseName += ".snap"
	}
	targetFn := filepath.Join(opts.TargetDir, baseName)

	if opts.OnlyComponents {
		// No path, we use it to contain components information
		downloadedSnap = &DownloadedSnap{
			Path:            "",
			Info:            sar.Info,
			RedirectChannel: sar.RedirectChannel,
		}
	} else {
		downloadedSnap, err = tsto.snapDownload(targetFn, sar, opts)
		if err != nil {
			return nil, err
		}
	}

	// Download specified components
	downloadedSnap.Components, err = tsto.downloadComponents(comps, sar, nil, opts)
	if err != nil {
		return nil, err
	}

	return downloadedSnap, err
}

func (tsto *ToolingStore) downloadComponents(comps []string, sar *store.SnapActionResult, downloadDirs map[string]string, snapOpts DownloadSnapOptions) ([]*DownloadedComponent, error) {
	downloadedComps := make([]*DownloadedComponent, len(comps))
	for i, comp := range comps {
		srr := sar.ResourceResult(comp)
		if srr == nil {
			return nil, fmt.Errorf("%s component for %s not found in store",
				comp, sar.SnapName())
		}
		baseName := snapOpts.Basename
		if baseName == "" {
			cpi := snap.MinimalComponentContainerPlaceInfo(
				srr.Name, snap.R(srr.Revision), sar.SnapName())
			baseName = cpi.Filename()
		} else {
			baseName += fmt.Sprintf("+%s.comp", srr.Name)
		}

		var targetDir string
		if downloadDirs != nil {
			targetDir = downloadDirs[comp]
		}

		// if the caller doesn't provide a specific target dir, use the one
		// that the snap was downloaded to
		if targetDir == "" {
			targetDir = snapOpts.TargetDir
		}

		targetFn := filepath.Join(targetDir, baseName)

		downloadedComp, err := tsto.componentDownload(targetFn, sar.SnapName(), srr, snapOpts)
		if err != nil {
			return nil, err
		}
		downloadedComps[i] = downloadedComp
	}

	return downloadedComps, nil
}

func (tsto *ToolingStore) downloadWithProgressBar(download func(progress.Meter) error) error {
	pb := progress.MakeProgressBar(tsto.Stdout)
	defer pb.Finished()

	// Intercept sigint
	c := make(chan os.Signal, 3)
	signal.Notify(c, syscall.SIGINT)
	defer signal.Reset(syscall.SIGINT)
	go func() {
		<-c
		pb.Finished()
		os.Exit(1)
	}()

	return download(pb)
}

func (tsto *ToolingStore) snapDownload(targetFn string, sar *store.SnapActionResult, opts DownloadSnapOptions) (downloadedSnap *DownloadedSnap, err error) {
	snap := sar.Info
	redirectChannel := sar.RedirectChannel

	// check if we already have the right file
	if osutil.FileExists(targetFn) {
		sha3_384Dgst, size, err := osutil.FileDigest(targetFn, crypto.SHA3_384)
		if err == nil && size == uint64(snap.DownloadInfo.Size) && fmt.Sprintf("%x", sha3_384Dgst) == snap.DownloadInfo.Sha3_384 {
			logger.Debugf("not downloading, using existing file %s", targetFn)
			return &DownloadedSnap{
				Path:            targetFn,
				Info:            snap,
				RedirectChannel: redirectChannel,
			}, nil
		}
		logger.Debugf("File exists but has wrong hash, ignoring (here).")
	}

	download := func(pb progress.Meter) error {
		dlOpts := &store.DownloadOptions{LeavePartialOnError: opts.LeavePartialOnError}
		return tsto.sto.Download(context.TODO(), snap.SnapName(), targetFn,
			&snap.DownloadInfo, pb, nil, dlOpts)
	}
	if err := tsto.downloadWithProgressBar(download); err != nil {
		return nil, err
	}

	return &DownloadedSnap{
		Path:            targetFn,
		Info:            snap,
		RedirectChannel: redirectChannel,
	}, nil
}

type SnapToDownload struct {
	Snap      naming.SnapRef
	Channel   string
	Revision  snap.Revision
	CohortKey string
	// ValidationSets is an optional array of validation set primary keys.
	ValidationSets []snapasserts.ValidationSetKey
	// CompsToDownload has the names of components we wish to dowload.
	CompsToDownload []string
}

type CurrentSnap struct {
	SnapName string
	SnapID   string
	Revision snap.Revision
	Channel  string
	Epoch    snap.Epoch
}

type DownloadManyOptions struct {
	BeforeDownloadFunc func(*snap.Info, map[string]*snap.ComponentInfo) (targetPath string, compPaths map[string]string, err error)
	EnforceValidation  bool
}

// DownloadMany downloads the specified snaps.
// curSnaps are meant to represent already downloaded snaps that will
// be installed in conjunction with the snaps to download, this is needed
// if enforcing validations (ops.EnforceValidation set to true) to
// have cross-gating work.
func (tsto *ToolingStore) DownloadMany(toDownload []SnapToDownload, curSnaps []*CurrentSnap, opts DownloadManyOptions) (downloadedSnaps map[string]*DownloadedSnap, err error) {
	if len(toDownload) == 0 {
		// nothing to do
		return nil, nil
	}
	if opts.BeforeDownloadFunc == nil {
		return nil, fmt.Errorf("internal error: DownloadManyOptions.BeforeDownloadFunc must be set")
	}

	actionFlag := store.SnapActionIgnoreValidation
	if opts.EnforceValidation {
		actionFlag = store.SnapActionEnforceValidation
	}

	downloadedSnaps = make(map[string]*DownloadedSnap, len(toDownload))
	current := make([]*store.CurrentSnap, 0, len(curSnaps))
	for _, csnap := range curSnaps {
		ch := "stable"
		if csnap.Channel != "" {
			ch = csnap.Channel
		}
		current = append(current, &store.CurrentSnap{
			InstanceName:     csnap.SnapName,
			SnapID:           csnap.SnapID,
			Revision:         csnap.Revision,
			TrackingChannel:  ch,
			Epoch:            csnap.Epoch,
			IgnoreValidation: !opts.EnforceValidation,
		})
	}

	actions := make([]*store.SnapAction, 0, len(toDownload))
	for _, sn := range toDownload {
		actions = append(actions, &store.SnapAction{
			Action:         "download",
			InstanceName:   sn.Snap.SnapName(), // XXX consider using snap-id first
			Channel:        sn.Channel,
			Revision:       sn.Revision,
			CohortKey:      sn.CohortKey,
			Flags:          actionFlag,
			ValidationSets: sn.ValidationSets,
		})
	}

	sars, _, err := tsto.sto.SnapAction(context.TODO(), current, actions, nil, nil,
		&store.RefreshOptions{IncludeResources: true})
	if err != nil {
		// err will be 'cannot download snap "foo": <reasons>'
		return nil, err
	}

	for i, sar := range sars {
		// Create component infos from resource data for the components we will download
		cinfos := make(map[string]*snap.ComponentInfo, len(sar.Resources))
		for _, res := range sar.Resources {
			if !strutil.ListContains(toDownload[i].CompsToDownload, res.Name) {
				continue
			}

			ctyp, err := store.ResourceToComponentType(res.Type)
			if err != nil {
				return nil, err
			}

			cref := naming.NewComponentRef(sar.SnapName(), res.Name)
			csi := snap.NewComponentSideInfo(cref, snap.R(res.Revision))
			cinfos[res.Name] = snap.NewComponentInfo(
				cref, ctyp, res.Version, "", "", sar.Provenance(), csi)
		}

		targetPath, compPaths, err := opts.BeforeDownloadFunc(sar.Info, cinfos)
		if err != nil {
			return nil, err
		}
		dlSnap, err := tsto.snapDownload(targetPath, &sar, DownloadSnapOptions{})
		if err != nil {
			return nil, err
		}
		downloadedSnaps[sar.SnapName()] = dlSnap

		downloadDirs := make(map[string]string, len(compPaths))
		for compName, compPath := range compPaths {
			downloadDirs[compName] = filepath.Dir(compPath)
		}

		// Download components
		downloadedSnapComps, err := tsto.downloadComponents(
			toDownload[i].CompsToDownload, &sar, downloadDirs,
			DownloadSnapOptions{TargetDir: filepath.Dir(targetPath)})
		if err != nil {
			return nil, err
		}
		downloadedSnaps[sar.SnapName()].Components = downloadedSnapComps
	}

	return downloadedSnaps, nil
}

// AssertionFetcher creates an asserts.Fetcher for assertions, the fetcher will
// add assertions in the given database and after that also call save for each of them.
func (tsto *ToolingStore) AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher {
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return tsto.sto.Assertion(ref.Type, ref.PrimaryKey, nil)
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

// AssertionSequenceFormingFetcher creates an asserts.SequenceFormingFetcher for
// fetching assertions. The fetcher will then store the fetched assertions in the
// given db and call save for each of them.
func (tsto *ToolingStore) AssertionSequenceFormingFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.SequenceFormingFetcher {
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return tsto.sto.Assertion(ref.Type, ref.PrimaryKey, nil)
	}
	retrieveSeq := func(seq *asserts.AtSequence) (asserts.Assertion, error) {
		return tsto.sto.SeqFormingAssertion(seq.Type, seq.SequenceKey, seq.Sequence, nil)
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
	return asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, save2)
}

// Find provides the snapsserts.Finder interface for snapasserts.DerviceSideInfo
func (tsto *ToolingStore) Find(at *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error) {
	pk, err := asserts.PrimaryKeyFromHeaders(at, headers)
	if err != nil {
		return nil, err
	}
	return tsto.sto.Assertion(at, pk, nil)
}

// SetAssertionMaxFormats sets the assertion max formats to use with Assertion and SnapAction.
func (tsto *ToolingStore) SetAssertionMaxFormats(maxFormats map[string]int) {
	tsto.sto.SetAssertionMaxFormats(maxFormats)
	tsto.assertMaxFormats = maxFormats
}

// AssertionMaxFormats returns the max formats set with SetAssertionMaxFormats or nil.
func (tsto *ToolingStore) AssertionMaxFormats() map[string]int {
	return tsto.assertMaxFormats
}

// MockToolingStore creates a ToolingStore that uses the provided StoreImpl
// implementation for Download, SnapAction and Assertion methods.
// For testing.
func MockToolingStore(sto StoreImpl) *ToolingStore {
	return &ToolingStore{sto: sto}
}

type DownloadedComponent struct {
	Path string
	Info *snap.ComponentInfo
}

func (tsto *ToolingStore) componentDownload(targetFn string, snapName string, srr *store.SnapResourceResult, opts DownloadSnapOptions) (downloadedComp *DownloadedComponent, err error) {
	// Check if this is a component we can handle
	ctyp, err := store.ResourceToComponentType(srr.Type)
	if err != nil {
		return nil, err
	}

	// check if we already have the right file
	if osutil.FileExists(targetFn) {
		sha3_384Dgst, size, err := osutil.FileDigest(targetFn, crypto.SHA3_384)
		if err == nil && size == uint64(srr.DownloadInfo.Size) &&
			fmt.Sprintf("%x", sha3_384Dgst) == srr.DownloadInfo.Sha3_384 {

			logger.Debugf("not downloading, using existing file %s", targetFn)
			return &DownloadedComponent{
				Path: targetFn,
			}, nil
		}
		logger.Debugf("File exists but has wrong hash, ignoring (here).")
	}

	cref := naming.NewComponentRef(snapName, srr.Name)
	download := func(pb progress.Meter) error {
		dlOpts := &store.DownloadOptions{LeavePartialOnError: opts.LeavePartialOnError}
		return tsto.sto.Download(context.TODO(), cref.String(), targetFn,
			&srr.DownloadInfo, pb, nil, dlOpts)
	}
	if err := tsto.downloadWithProgressBar(download); err != nil {
		return nil, err
	}

	return &DownloadedComponent{
		Path: targetFn,
		Info: &snap.ComponentInfo{
			Component: cref,
			Type:      ctyp,
			Version:   srr.Version,
			ComponentSideInfo: snap.ComponentSideInfo{
				Component: cref,
				Revision:  snap.R(srr.Revision),
			},
		},
	}, nil
}
