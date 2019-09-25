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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
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

	Info  *snap.Info
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

	byNameOptSnaps *naming.SnapSet

	availableSnaps *naming.SnapSet

	snapsFromModel []*SeedSnap
	implicitSnaps  []*SeedSnap // only for Core 16/18 we allow for these
	extraSnaps     []*SeedSnap
}

type policy interface {
	systemSnap() *asserts.ModelSnap

	checkBase(*snap.Info, *naming.SnapSet) error
}

type tree interface {
	mkFixedDirs() error

	// XXX might need to differentiate for local, extra snaps
	snapsDir() string
}

// New returns a Writer to write a seed for the given model and using
// the given Options.
func New(model *asserts.Model, opts *Options) (*Writer, error) {
	if opts == nil {
		return nil, fmt.Errorf("internal error: Writer *Options is nil")
	}
	return &Writer{
		model:  model,
		opts:   opts,
		policy: &policy16{model: model, opts: opts},
		tree:   &tree16{opts: opts},

		expectedStep: setOptionsSnapsStep,

		byNameOptSnaps: naming.NewSnapSet(nil),
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
				// XXX no local snaps!
				alright = true
			} else if w.expectedStep == infoDerivedStep {
				// XXX no local snaps!
				alright = true
			}
		}
		if !alright {
			expected := w.expectedStep.String()
			switch w.expectedStep {
			case setOptionsSnapsStep:
				expected = "Start|SetOptionsSnaps"
			case localSnapsStep:
				expected = "SnapsToDownload|LocalSnaps"
			}
			return fmt.Errorf("internal error: seedwriter.Writer expected %s to be invoked on it at this point, not %v", expected, thisStep)
		}
	}
	w.expectedStep = thisStep + 1
	return nil

}

func (w *Writer) Start(db asserts.RODatabase, newFetcher NewFetcherFunc) error {
	if err := w.checkStep(startStep); err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("internal error: Writer *asserts.RODatabsae is nil")

	}
	if newFetcher == nil {
		return fmt.Errorf("internal error: Writer newFetcherFunc is nil")
	}
	w.db = db

	f := MakeRefAssertsFetcher(newFetcher)

	// XXX support UBUNTU_IMAGE_SKIP_COPY_UNVERIFIED_MODEL ?
	if err := f.Save(w.model); err != nil {
		return fmt.Errorf("cannot fetch and check prerequisites for the model assertion: %v", err)
	}

	w.modelRefs = f.Refs()

	// XXX get if needed the store assertion

	return w.tree.mkFixedDirs()
}

// SetOptionsSnaps accepts options-referred snaps represented as OptionSnap.
func (w *Writer) SetOptionsSnaps(optSnaps []*OptionSnap) error {
	if err := w.checkStep(setOptionsSnapsStep); err != nil {
		return err
	}

	for _, sn := range optSnaps {
		if sn.Name != "" {
			snapName := sn.Name
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
		}
	}

	return nil
}

// LocalSnaps()
// InfoDerived()

// SetInfo sets Info of the SeedSnap and possibly computes its
// destination Path.
func (w *Writer) SetInfo(sn *SeedSnap, info *snap.Info) error {
	if sn.local {
		panic("NOT IMPLEMENTED YET")
	}

	sn.Info = info
	sn.Path = filepath.Join(w.tree.snapsDir(), filepath.Base(info.MountFile()))
	return nil
}

// SnapsToDownload returns a list of seed snaps to download. Once that
// is done and their SeedSnaps Info with SetInfo and ARefs fields are
// set, Downloaded should be called next.
func (w *Writer) SnapsToDownload() (snaps []*SeedSnap, err error) {
	if err := w.checkStep(snapsToDownloadStep); err != nil {
		return nil, err
	}

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

	snapsFromModel := make([]*SeedSnap, 0, len(modSnaps))

	// XXX local snaps, extra snaps

	for _, modSnap := range modSnaps {
		optSnap, _ := w.byNameOptSnaps.Lookup(modSnap).(*OptionSnap)
		// XXX channel = s.policy.ResolveChannel...
		sn := SeedSnap{
			SnapRef: modSnap,

			local:      false,
			modelSnap:  modSnap,
			optionSnap: optSnap,
		}
		snapsFromModel = append(snapsFromModel, &sn)
	}

	// XXX compute extra snaps (up to implicit snaps) to error as
	// early as possible

	w.snapsFromModel = snapsFromModel
	// XXX once we have local snaps this will not be all of the snaps
	return snapsFromModel, nil
}

// Downloaded checks the downloaded snaps metadata provided via
// setting it in the SeedSnaps returned by the previous SnapsToDownload.
// It also returns whether the seed snap set is complete or
// SnapsToDownload should be called again.
func (w *Writer) Downloaded() (complete bool, err error) {
	if err := w.checkStep(downloadedStep); err != nil {
		return false, err
	}

	// XXX consider empty w.snapsFromModel

	w.availableSnaps = naming.NewSnapSet(nil)

	for _, sn := range w.snapsFromModel {
		if sn.Info == nil {
			return false, fmt.Errorf("internal error: at this point snap %q Info should have been set", sn.SnapName())
		}
		w.availableSnaps.Add(sn)
	}

	for _, sn := range w.snapsFromModel {
		info := sn.Info
		if !sn.local {
			if sn.ARefs == nil {
				return false, fmt.Errorf("internal error: at this point snap %q ARefs should have been set", sn.SnapName())
			}
		}

		// TODO: optionally check that model snap name and
		// info snap name match

		if err := checkType(sn, w.model); err != nil {
			return false, err
		}

		needsClassic := info.NeedsClassic()
		if needsClassic && !w.model.Classic() {
			return false, fmt.Errorf("cannot use classic snap %q in a core system", info.SnapName())
		}

		if err := w.policy.checkBase(info, w.availableSnaps); err != nil {
			return false, err
		}
		// error about missing default providers
		for _, dp := range snap.NeededDefaultProviders(info) {
			if !w.availableSnaps.Contains(naming.Snap(dp)) {
				// TODO: have a way to ignore this issue on a snap by snap basis?
				return false, fmt.Errorf("cannot use snap %q without its default content provider %q being added explicitly", info.SnapName(), dp)
			}
		}

		if err := w.checkPublisher(sn); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (w *Writer) checkPublisher(sn *SeedSnap) error {
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

// SeedSnaps checks seed snaps and possibly copies local snaps into
// the seed XXX.
func (w *Writer) SeedSnaps() error {
	if err := w.checkStep(seedSnapsStep); err != nil {
		return err
	}

	snapsDir := w.tree.snapsDir()

	for _, sn := range w.snapsFromModel {
		info := sn.Info
		if !sn.local {
			expectedPath := filepath.Join(snapsDir, filepath.Base(info.MountFile()))
			if sn.Path != expectedPath {
				return fmt.Errorf("internal error: at this point snap %q Path should have been set to %q", sn.SnapName(), expectedPath)
			}
			if !osutil.FileExists(expectedPath) {
				return fmt.Errorf("internal error: at this point snap file %q should exist", expectedPath)
			}
		}
	}

	// XXX implement this fully
	return nil
}

// WriteMeta writes seed metadata and assertions into the seed.
func (w *Writer) WriteMeta() error {
	if err := w.checkStep(writeMetaStep); err != nil {
		return err
	}

	// XXX implement this
	return nil
}
