// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

// Package seedwrite implements writing image seeds.
package seedwriter

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
)

// Options holds the options for a Writer.
type Options struct {
	RootDir string
	SeedDir string

	DefaultChannel string

	// Architecture to use if none is specified by the model,
	// useful only for classic mode. If set must match the model otherwise.
	Architecture string
}

// OptionSnap represents an options-referred snap with its option values.
// E.g. a snap passed to ubuntu-image via --snap.
// If Name is set the snap is from the store. If Path is set the snap
// is local at Path location.
// XXX|TODO: for further clarity rename to OptionsSnap
type OptionSnap struct {
	Name    string
	SnapID  string
	Path    string
	Channel string
}

func (s *OptionSnap) SnapName() string {
	return s.Name
}

func (s *OptionSnap) ID() string {
	return s.SnapID
}

var _ naming.SnapRef = (*OptionSnap)(nil)

// SeedSnap holds details of a snap being added to a seed.
type SeedSnap struct {
	naming.SnapRef
	Channel string
	Path    string

	// Info is the *snap.Info for the seed snap, filling this is
	// delegated to the Writer using code, via Writer.SetInfo.
	Info *snap.Info
	// ARefs are references to the snap assertions if applicable,
	// filling these is delegated to the Writer using code, the
	// assumption is that the corresponding assertions can be
	// found in the database passed to Writer.Start.
	ARefs []*asserts.Ref

	local      bool
	modelSnap  *asserts.ModelSnap
	optionSnap *OptionSnap
}

var _ naming.SnapRef = (*SeedSnap)(nil)

/* Writer writes Core 16/18 and Core 20 seeds.

Its methods need to be called in sequences that match prescribed
flows.

Some methods can be skipped given some conditions.

SnapsToDownload and Downloaded needs to be called in a loop where the
SeedSnaps returned by SnapsToDownload get SetInfo called with
*snap.Info retrieved from the store and then the snaps can be
downloaded at SeedSnap.Path, after which Downloaded must be invoked
and the flow breaks out of the loop only when it returns complete =
true. In the loop as well assertions for the snaps can be fetched and
SeedSnap.ARefs set.

Optionally a similar but simpler mechanism covers local snaps, where
LocalSnaps returns SeedSnaps that can be filled with information
derived from the snap at SeedSnap.Path, then InfoDerived is called.

                      V-------->\
                      |         |
               SetOptionsSnaps  |
                      |         v
                      | ________/
                      v
         /          Start       \
         |            |         |
         |            v         |
   no    |   /    LocalSnaps    | no option
   local |   |        |         | snaps
   snaps |   |        v         |
         |   |    InfoDerived   |
         |   |        |         |
         |   \        |         /
          >   > SnapsToDownload<
                      |     ^
                      |     |
                      |     | complete = false
                      v     /
                  Downloaded
                      |
                      | complete = true
                      |
                      v
                  SeedSnaps (copy files)
                      |
                      v
                  WriteMeta
                      |
                      v
                  XXX gadget stuff ...

*/
type Writer struct {
	model  *asserts.Model
	opts   *Options
	policy policy
	tree   tree

	db asserts.RODatabase

	expectedStep writerStep

	modelRefs []*asserts.Ref

	// XXX optionsSnaps []*OptionSnap

	byNameOptSnaps *naming.SnapSet

	localSnaps      []*SeedSnap
	byRefLocalSnaps *naming.SnapSet

	availableSnaps *naming.SnapSet

	// toDownload tracks which set of snaps SnapsToDownload should compute
	// next
	toDownload              snapsToDownloadSet
	toDownloadConsideredNum int

	snapsFromModel []*SeedSnap
	extraSnaps     []*SeedSnap
}

type policy interface {
	checkDefaultChannel(channel.Channel) error
	checkSnapChannel(ch channel.Channel, whichSnap string) error

	systemSnap() *asserts.ModelSnap

	checkBase(*snap.Info, *naming.SnapSet) error

	needsImplicitSnaps(*naming.SnapSet) (bool, error)
	implicitSnaps(*naming.SnapSet) []*asserts.ModelSnap
}

type tree interface {
	mkFixedDirs() error

	// XXX might need to differentiate for extra snaps
	snapsDir() string

	localSnapPath(*SeedSnap) string

	writeAssertions(db asserts.RODatabase, modelRefs []*asserts.Ref, snapsFromModel []*SeedSnap) error

	writeMeta(snapsFromModel []*SeedSnap) error
}

// New returns a Writer to write a seed for the given model and using
// the given Options.
func New(model *asserts.Model, opts *Options) (*Writer, error) {
	if opts == nil {
		return nil, fmt.Errorf("internal error: Writer *Options is nil")
	}
	pol := &policy16{model: model, opts: opts}

	if opts.DefaultChannel != "" {
		deflCh, err := channel.ParseVerbatim(opts.DefaultChannel, "_")
		if err != nil {
			return nil, fmt.Errorf("cannot use global default option channel: %v", err)
		}
		if err := pol.checkDefaultChannel(deflCh); err != nil {
			return nil, err
		}
	}

	return &Writer{
		model:  model,
		opts:   opts,
		policy: pol,
		tree:   &tree16{opts: opts},

		expectedStep: setOptionsSnapsStep,

		byNameOptSnaps:  naming.NewSnapSet(nil),
		byRefLocalSnaps: naming.NewSnapSet(nil),
	}, nil
}

type writerStep int

const (
	setOptionsSnapsStep = iota
	startStep
	localSnapsStep
	infoDerivedStep
	snapsToDownloadStep
	downloadedStep
	seedSnapsStep
	writeMetaStep
)

var writerStepNames = map[writerStep]string{
	startStep:           "Start",
	setOptionsSnapsStep: "SetOptionsSnaps",
	localSnapsStep:      "LocalSnaps",
	infoDerivedStep:     "InfoDerived",
	snapsToDownloadStep: "SnapsToDownload",
	downloadedStep:      "Downloaded",
	seedSnapsStep:       "SeedSnaps",
	writeMetaStep:       "WriteMeta",
}

func (ws writerStep) String() string {
	name := writerStepNames[ws]
	if name == "" {
		panic(fmt.Sprintf("unknown writerStep: %d", ws))
	}
	return name
}

func (w *Writer) checkStep(thisStep writerStep) error {
	if thisStep != w.expectedStep {
		// exceptions
		alright := false
		switch thisStep {
		case startStep:
			if w.expectedStep == setOptionsSnapsStep {
				alright = true
			}
		case snapsToDownloadStep:
			if w.expectedStep == localSnapsStep {
				if len(w.localSnaps) != 0 {
					break
				}
				alright = true
			} else if w.expectedStep == infoDerivedStep {
				if len(w.localSnaps) != 0 {
					break
				}
				alright = true
			}
		}
		if !alright {
			expected := w.expectedStep.String()
			switch w.expectedStep {
			case setOptionsSnapsStep:
				expected = "Start|SetOptionsSnaps"
			case localSnapsStep:
				if len(w.localSnaps) == 0 {
					expected = "SnapsToDownload|LocalSnaps"
				}
			}
			return fmt.Errorf("internal error: seedwriter.Writer expected %s to be invoked on it at this point, not %v", expected, thisStep)
		}
	}
	w.expectedStep = thisStep + 1
	return nil
}

// SetOptionsSnaps accepts options-referred snaps represented as OptionSnap.
func (w *Writer) SetOptionsSnaps(optSnaps []*OptionSnap) error {
	if err := w.checkStep(setOptionsSnapsStep); err != nil {
		return err
	}

	// XXX check with policy if local snaps are ok

	for _, sn := range optSnaps {
		var whichSnap string
		local := false
		if sn.Name != "" {
			if sn.Path != "" {
				return fmt.Errorf("cannot specify both name and path for option snap %q", sn.Name)
			}
			snapName := sn.Name
			whichSnap = snapName
			if _, instanceKey := snap.SplitInstanceName(snapName); instanceKey != "" {
				// be specific about this error
				return fmt.Errorf("cannot use snap %q, parallel snap instances are unsupported", snapName)
			}
			if err := naming.ValidateSnap(snapName); err != nil {
				return err
			}

			if w.byNameOptSnaps.Contains(sn) {
				return fmt.Errorf("snap %q is repeated in options", snapName)
			}
			w.byNameOptSnaps.Add(sn)
		} else {
			if !strings.HasSuffix(sn.Path, ".snap") {
				return fmt.Errorf("local option snap %q does not end in .snap", sn.Path)
			}
			if !osutil.FileExists(sn.Path) {
				return fmt.Errorf("local option snap %q does not exist", sn.Path)
			}

			whichSnap = sn.Path
			local = true
		}
		if sn.Channel != "" {
			ch, err := channel.ParseVerbatim(sn.Channel, "_")
			if err != nil {
				return fmt.Errorf("cannot use option channel for snap %q: %v", whichSnap, err)
			}
			if err := w.policy.checkSnapChannel(ch, whichSnap); err != nil {
				return err
			}
		}
		if local {
			w.localSnaps = append(w.localSnaps, &SeedSnap{
				SnapRef: nil,
				Path:    sn.Path,

				local:      true,
				optionSnap: sn,
			})
		}
	}

	return nil
}

// Start starts the seed writing. It creates a RefAssertsFetcher using
// newFetcher and uses it to fetch model related assertions. For
// convenience it returns the fetcher possibly for use to fetch seed
// snap assertions, a task that the writer delegates as well as snap
// downloading. The writer assumes that the snap assertions will end up
// in the given db (writing assertion database).
func (w *Writer) Start(db asserts.RODatabase, newFetcher NewFetcherFunc) (RefAssertsFetcher, error) {
	if err := w.checkStep(startStep); err != nil {
		return nil, err
	}
	if db == nil {
		return nil, fmt.Errorf("internal error: Writer *asserts.RODatabase is nil")
	}
	if newFetcher == nil {
		return nil, fmt.Errorf("internal error: Writer newFetcherFunc is nil")
	}
	w.db = db

	f := MakeRefAssertsFetcher(newFetcher)

	// XXX support UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL ?
	if err := f.Save(w.model); err != nil {
		return nil, fmt.Errorf("cannot fetch and check prerequisites for the model assertion: %v", err)
	}

	w.modelRefs = f.Refs()

	// XXX get if needed the store assertion

	if err := w.tree.mkFixedDirs(); err != nil {
		return nil, err
	}

	return f, nil
}

// LocalSnaps returns a list of seed snaps that are local.  The writer
// delegates to produce *snap.Info for them to then be set via
// SetInfo. If matching snap assertions can be found as well they can
// be passed into SeedSnap ARefs, assuming they were added to the
// writing assertion database.
func (w *Writer) LocalSnaps() ([]*SeedSnap, error) {
	if err := w.checkStep(localSnapsStep); err != nil {
		return nil, err
	}

	return w.localSnaps, nil
}

// InfoDerived checks the local snaps metadata provided via setting it
// into the SeedSnaps returned by the previous LocalSnaps.
func (w *Writer) InfoDerived() error {
	if err := w.checkStep(infoDerivedStep); err != nil {
		return err
	}

	for _, sn := range w.localSnaps {
		if sn.Info == nil {
			return fmt.Errorf("internal error: before seedwriter.Writer.InfoDerived snap %q Info should have been set", sn.Path)
		}
		sn.SnapRef = sn.Info

		// local snap gets local revision
		if sn.Info.Revision.Unset() {
			sn.Info.Revision = snap.R(-1)
		}

		if w.byRefLocalSnaps.Contains(sn) {
			return fmt.Errorf("local snap %q is repeated in options", sn.SnapName())
		}

		// in case, merge channel given by name separately
		optSnap, _ := w.byNameOptSnaps.Lookup(sn).(*OptionSnap)
		if optSnap != nil && optSnap.Channel != "" {
			if sn.optionSnap.Channel != "" {
				if sn.optionSnap.Channel != optSnap.Channel {
					return fmt.Errorf("option snap has different channels specified: %q=%q vs %q=%q", sn.Path, sn.optionSnap.Channel, optSnap.Name, optSnap.Channel)
				}
			} else {
				sn.optionSnap.Channel = optSnap.Channel
			}
		}

		w.byRefLocalSnaps.Add(sn)
	}

	return nil
}

// SetInfo sets Info of the SeedSnap and possibly computes its
// destination Path.
func (w *Writer) SetInfo(sn *SeedSnap, info *snap.Info) error {
	sn.Info = info
	if sn.local {
		// nothing more to do
		return nil
	}

	sn.Path = filepath.Join(w.tree.snapsDir(), filepath.Base(info.MountFile()))
	return nil
}

// snapsToDownloadSet indicates which set of snaps SnapsToDownload should compute
type snapsToDownloadSet int

const (
	toDownloadModel snapsToDownloadSet = iota
	toDownloadImplicit
	// toDownloadExtra
	// toDownloadExtraImplicit
)

func (w *Writer) modelSnapToSeed(modSnap *asserts.ModelSnap) (*SeedSnap, error) {
	sn, _ := w.byRefLocalSnaps.Lookup(modSnap).(*SeedSnap)
	var optSnap *OptionSnap
	if sn == nil {
		// not local, to download
		optSnap, _ = w.byNameOptSnaps.Lookup(modSnap).(*OptionSnap)
		sn = &SeedSnap{
			SnapRef: modSnap,

			local:      false,
			optionSnap: optSnap,
		}
	} else {
		optSnap = sn.optionSnap
	}

	channel, err := w.resolveChannel(modSnap.SnapName(), modSnap, optSnap)
	if err != nil {
		return nil, err
	}
	sn.modelSnap = modSnap
	sn.Channel = channel
	return sn, nil
}

func (w *Writer) modelSnapsToDownload(modSnaps []*asserts.ModelSnap) (toDownload []*SeedSnap, err error) {
	if w.snapsFromModel == nil {
		w.snapsFromModel = make([]*SeedSnap, 0, len(modSnaps))
	}
	toDownload = make([]*SeedSnap, 0, len(modSnaps))

	alreadyConsidered := len(w.snapsFromModel)
	for _, modSnap := range modSnaps {
		sn, err := w.modelSnapToSeed(modSnap)
		if err != nil {
			return nil, err
		}
		if !sn.local {
			toDownload = append(toDownload, sn)
		}
		w.snapsFromModel = append(w.snapsFromModel, sn)
	}
	w.toDownloadConsideredNum = len(w.snapsFromModel) - alreadyConsidered

	return toDownload, nil
}

func (w *Writer) modSnaps() []*asserts.ModelSnap {
	modSnaps := w.model.AllSnaps()
	if systemSnap := w.policy.systemSnap(); systemSnap != nil {
		prepend := true
		for _, modSnap := range modSnaps {
			if naming.SameSnap(modSnap, systemSnap) {
				prepend = false
				// TODO: sanity check modes
				break
			}
		}
		if prepend {
			modSnaps = append([]*asserts.ModelSnap{systemSnap}, modSnaps...)
		}
	}
	return modSnaps
}

// SnapsToDownload returns a list of seed snaps to download. Once that
// is done and their SeedSnaps Info with SetInfo and ARefs fields are
// set, Downloaded should be called next.
func (w *Writer) SnapsToDownload() (snaps []*SeedSnap, err error) {
	if err := w.checkStep(snapsToDownloadStep); err != nil {
		return nil, err
	}

	// XXX extra snaps

	switch w.toDownload {
	case toDownloadModel:
		return w.modelSnapsToDownload(w.modSnaps())
	case toDownloadImplicit:
		return w.modelSnapsToDownload(w.policy.implicitSnaps(w.availableSnaps))
	default:
		panic(fmt.Sprintf("unknown to-download set: %d", w.toDownload))
	}

	// XXX compute extra snaps (up to implicit snaps) to error as
	// early as possible
}

func (w *Writer) resolveChannel(whichSnap string, modSnap *asserts.ModelSnap, optSnap *OptionSnap) (string, error) {
	var optChannel string
	if optSnap != nil {
		optChannel = optSnap.Channel
	}
	if optChannel == "" {
		optChannel = w.opts.DefaultChannel
	}

	if modSnap == nil {
		if optChannel == "" {
			return "stable", nil
		}
		return optChannel, nil
	}

	if modSnap.Track != "" {
		resChannel, err := channel.ResolveLocked(modSnap.Track, optChannel)
		if err == channel.ErrLockedTrackSwitch {
			return "", fmt.Errorf("option channel %q for %s has a track incompatible with the track from model assertion: %s", optChannel, whichModelSnap(modSnap, w.model), modSnap.Track)
		}
		if err != nil {
			// shouldn't happen given that we check that
			// the inputs parse before
			return "", fmt.Errorf("internal error: cannot resolve locked track %q and option channel %q for snap %q", modSnap.Track, optChannel, whichSnap)
		}
		return resChannel, nil
	}

	resChannel, err := channel.Resolve(modSnap.DefaultChannel, optChannel)
	if err != nil {
		// shouldn't happen given that we check that
		// the inputs parse before
		return "", fmt.Errorf("internal error: cannot resolve model default channel %q and option channel %q for snap %q", modSnap.DefaultChannel, optChannel, whichSnap)
	}
	return resChannel, nil
}

func (w *Writer) downloaded(seedSnaps []*SeedSnap) error {

	if w.availableSnaps == nil {
		w.availableSnaps = naming.NewSnapSet(nil)
	}

	for _, sn := range seedSnaps {
		if sn.Info == nil {
			return fmt.Errorf("internal error: before seedwriter.Writer.Downloaded snap %q Info should have been set", sn.SnapName())
		}
		w.availableSnaps.Add(sn)
	}

	for _, sn := range seedSnaps {
		info := sn.Info
		if !sn.local {
			if info.ID() == "" {
				return fmt.Errorf("internal error: before seedwriter.Writer.Downloaded snap %q snap-id should have been set", sn.SnapName())
			}
		}
		if info.ID() != "" {
			if sn.ARefs == nil {
				return fmt.Errorf("internal error: before seedwriter.Writer.Downloaded snap %q ARefs should have been set", sn.SnapName())
			}
		}

		// TODO: optionally check that model snap name and
		// info snap name match

		if err := checkType(sn, w.model); err != nil {
			return err
		}

		needsClassic := info.NeedsClassic()
		if needsClassic && !w.model.Classic() {
			return fmt.Errorf("cannot use classic snap %q in a core system", info.SnapName())
		}

		if err := w.policy.checkBase(info, w.availableSnaps); err != nil {
			return err
		}
		// error about missing default providers
		for _, dp := range snap.NeededDefaultProviders(info) {
			if !w.availableSnaps.Contains(naming.Snap(dp)) {
				// TODO: have a way to ignore this issue on a snap by snap basis?
				return fmt.Errorf("cannot use snap %q without its default content provider %q being added explicitly", info.SnapName(), dp)
			}
		}

		if err := w.checkPublisher(sn); err != nil {
			return err
		}
	}

	return nil
}

// Downloaded checks the downloaded snaps metadata provided via
// setting it into the SeedSnaps returned by the previous
// SnapsToDownload.  It also returns whether the seed snap set is
// complete or SnapsToDownload should be called again.
func (w *Writer) Downloaded() (complete bool, err error) {
	if err := w.checkStep(downloadedStep); err != nil {
		return false, err
	}

	// TODO: w.policy.resetChecks()

	considered := w.snapsFromModel
	considered = considered[len(considered)-w.toDownloadConsideredNum:]
	err = w.downloaded(considered)
	if err != nil {
		return false, err
	}

	switch w.toDownload {
	case toDownloadModel:
		implicitNeeded, err := w.policy.needsImplicitSnaps(w.availableSnaps)
		if err != nil {
			return false, err
		}
		if implicitNeeded {
			w.toDownload = toDownloadImplicit
			w.expectedStep = snapsToDownloadStep
			return false, nil
		}
	case toDownloadImplicit:
		// nothing more to do
	default:
		panic(fmt.Sprintf("unknown to-download set: %d", w.toDownload))
	}

	return true, nil
}

func (w *Writer) checkPublisher(sn *SeedSnap) error {
	if sn.local && sn.ARefs == nil {
		// nothing to do
		return nil
	}
	info := sn.Info
	var kind string
	switch info.GetType() {
	case snap.TypeKernel:
		kind = "kernel"
	case snap.TypeGadget:
		kind = "gadget"
	default:
		return nil
	}
	// TODO: share helpers with devicestate if the policy becomes much more complicated
	snapDecl, err := w.snapDecl(sn)
	if err != nil {
		return err
	}
	publisher := snapDecl.PublisherID()
	if publisher != w.model.BrandID() && publisher != "canonical" {
		return fmt.Errorf("cannot use %s %q published by %q for model by %q", kind, info.SnapName(), publisher, w.model.BrandID())
	}
	return nil
}

func (w *Writer) snapDecl(sn *SeedSnap) (*asserts.SnapDeclaration, error) {
	for _, ref := range sn.ARefs {
		if ref.Type == asserts.SnapDeclarationType {
			a, err := ref.Resolve(w.db.Find)
			if err != nil {
				return nil, fmt.Errorf("internal error: lost saved assertion")
			}
			return a.(*asserts.SnapDeclaration), nil
		}
	}
	return nil, fmt.Errorf("internal error: snap %q has no snap-declaration set", sn.SnapName())
}

// SeedSnaps checks seed snaps and copies local snaps into the seed using copySnap.
func (w *Writer) SeedSnaps(copySnap func(name, src, dst string) error) error {
	if err := w.checkStep(seedSnapsStep); err != nil {
		return err
	}

	snapsDir := w.tree.snapsDir()

	for _, sn := range w.snapsFromModel {
		info := sn.Info
		if !sn.local {
			expectedPath := filepath.Join(snapsDir, filepath.Base(info.MountFile()))
			if sn.Path != expectedPath {
				return fmt.Errorf("internal error: before seedwriter.Writer.SeedSnaps snap %q Path should have been set to %q", sn.SnapName(), expectedPath)
			}
			if !osutil.FileExists(expectedPath) {
				return fmt.Errorf("internal error: before seedwriter.Writer.SeedSnaps snap file %q should exist", expectedPath)
			}
		} else {
			dst := w.tree.localSnapPath(sn)
			err := copySnap(info.SnapName(), sn.Path, dst)
			if err != nil {
				return err
			}
			// record final destination path
			sn.Path = dst
		}
	}

	return nil
}

// WriteMeta writes seed metadata and assertions into the seed.
func (w *Writer) WriteMeta() error {
	if err := w.checkStep(writeMetaStep); err != nil {
		return err
	}

	snapsFromModel := w.snapsFromModel

	if err := w.tree.writeAssertions(w.db, w.modelRefs, snapsFromModel); err != nil {
		return err
	}

	return w.tree.writeMeta(snapsFromModel)
}
