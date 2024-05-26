// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2023 Canonical Ltd
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
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// Options holds the options for a Writer.
type Options struct {
	SeedDir string

	DefaultChannel string

	// The label for the recovery system for Core20 models
	Label string

	// TestSkipCopyUnverifiedModel is set to support naive tests
	// using an unverified model, the resulting image is broken
	TestSkipCopyUnverifiedModel bool

	// Manifest is used to track snaps and validation sets that have
	// been seeded. It can be pre-provided to provide specific revisions
	// and validation-set sequences.
	Manifest *Manifest
	// ManifestPath if set, specifies the file path where the
	// seed.manifest file should be written.
	ManifestPath string
}

// manifest returns either the manifest already provided by the
// options, or if not provided, returns a newly initialized manifest.
func (opts *Options) manifest() *Manifest {
	if opts.Manifest == nil {
		return NewManifest()
	}
	return opts.Manifest
}

// OptionsSnap represents an options-referred snap with its option values.
// E.g. a snap passed to ubuntu-image via --snap.
// If Name is set the snap is from the store. If Path is set the snap
// is local at Path location.
type OptionsSnap struct {
	Name    string
	SnapID  string
	Path    string
	Channel string
}

func (s *OptionsSnap) SnapName() string {
	return s.Name
}

func (s *OptionsSnap) ID() string {
	return s.SnapID
}

var _ naming.SnapRef = (*OptionsSnap)(nil)

// SeedSnap holds details of a snap being added to a seed.
type SeedSnap struct {
	naming.SnapRef
	Channel string
	Path    string

	// Info is the *snap.Info for the seed snap, filling this is
	// delegated to the Writer using code, via Writer.SetInfo.
	Info *snap.Info
	// aRefs are references to the snap assertions if applicable,
	// these are filled invoking a AssertsFetchFunc passed to Downloaded.
	// The assumption is that the corresponding assertions can be found in
	// the database passed to Writer.Start.
	aRefs []*asserts.Ref

	local      bool
	modelSnap  *asserts.ModelSnap
	optionSnap *OptionsSnap
}

func (sn *SeedSnap) modes() []string {
	if sn.modelSnap == nil {
		// run is the assumed mode for extra snaps not listed
		// in the model
		return []string{"run"}
	}
	return sn.modelSnap.Modes
}

var _ naming.SnapRef = (*SeedSnap)(nil)

// Writer writes Core 16/18 and Core 20 seeds. Its methods need to be called in
// sequences that match prescribed flows.
// Some methods can be skipped given some conditions.
//
// SnapsToDownload and Downloaded needs to be called in a loop where the
// SeedSnaps returned by SnapsToDownload get SetInfo called with *snap.Info
// retrieved from the store and then the snaps can be downloaded at
// SeedSnap.Path, after which Downloaded must be invoked and the flow breaks
// out of the loop only when it returns complete = true.

// Downloaded must be passed an AssertsFetchFunc responsible for fetching or
// retrieving snap assertions when applicable.
//
// Optionally a similar but simpler mechanism covers local snaps, where
// LocalSnaps returns SeedSnaps that can be filled with information derived
// from the snap at SeedSnap.Path, then InfoDerived is called.
//
//	                    V-------->\
//	                    |         |
//	             SetOptionsSnaps  |
//	                    |         v
//	                    | ________/
//	                    v
//	       /          Start       \
//	       |            |         |
//	       |            v         |
//	       |   /    LocalSnaps    |
//	 no    |   |        |         |
//	 local |   |        v         | no option
//	 snaps |   |     SetInfo*     | snaps
//	       |   |        |         |
//	       |   |        v         |
//	       |   |    InfoDerived   |
//	       |   |        |         |
//	       \   \        |         /
//	        >   > SnapsToDownload<
//	                    |     ^
//	                    v     |
//	                 SetInfo* |
//	                    |     | complete = false
//	                    v     /
//	                Downloaded
//	                    |
//	                    | complete = true
//	                    |
//	                    v
//	                SeedSnaps (copy files)
//	                    |
//	                    v
//	                WriteMeta
//
//	* = 0 or many calls (as needed)
type Writer struct {
	model  *asserts.Model
	opts   *Options
	policy policy
	tree   tree

	// warnings keep a list of warnings produced during the
	// process, no more warnings should be produced after
	// Downloaded signaled complete
	warnings []string

	db asserts.RODatabase

	expectedStep writerStep

	modelRefs []*asserts.Ref

	optionsSnaps []*OptionsSnap
	// consumedOptSnapNum counts which options snaps have been consumed
	// by either cross matching or matching with a model snap
	consumedOptSnapNum int
	// extraSnapsGuessNum is essentially #(optionsSnaps) -
	// consumedOptSnapNum
	extraSnapsGuessNum int

	byNameOptSnaps *naming.SnapSet

	localSnaps      map[*OptionsSnap]*SeedSnap
	byRefLocalSnaps *naming.SnapSet

	availableSnaps  *naming.SnapSet
	availableByMode map[string]*naming.SnapSet
	byModeSnaps     map[string][]*SeedSnap

	// toDownload tracks which set of snaps SnapsToDownload should compute
	// next
	toDownload              snapsToDownloadSet
	toDownloadConsideredNum int

	snapsFromModel []*SeedSnap
	extraSnaps     []*SeedSnap

	consideredForAssertionsIndex int

	consideredForSnapdCarryingIndex int
	systemSnap                      *SeedSnap
	kernelSnap                      *SeedSnap
	noKernelSnap                    bool
	// manifest is the manifest of the seed used to track
	// seeded snaps and validation-sets. It may either be
	// initialized from the one provided in options, or it
	// may be initialized to a new copy.
	manifest *Manifest
}

type policy interface {
	allowsDangerousFeatures() error

	checkDefaultChannel(channel.Channel) error
	checkSnapChannel(ch channel.Channel, whichSnap string) error

	systemSnap() *asserts.ModelSnap

	modelSnapDefaultChannel() string
	extraSnapDefaultChannel() string

	checkBase(s *snap.Info, modes []string, availableByMode map[string]*naming.SnapSet) error

	checkClassicSnap(sn *SeedSnap) error

	needsImplicitSnaps(availableByMode map[string]*naming.SnapSet) (bool, error)
	implicitSnaps(availableByMode map[string]*naming.SnapSet) []*asserts.ModelSnap
	implicitExtraSnaps(availableByMode map[string]*naming.SnapSet) []*OptionsSnap
	recordSnapNameUsage(snapName string)
	isSystemSnapCandidate(sn *SeedSnap) bool
	ignoreUndeterminedSystemSnap() bool
}

type tree interface {
	mkFixedDirs() error

	snapPath(*SeedSnap) (string, error)

	localSnapPath(*SeedSnap) (string, error)

	writeAssertions(db asserts.RODatabase, modelRefs []*asserts.Ref, snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error

	writeMeta(snapsFromModel []*SeedSnap, extraSnaps []*SeedSnap) error
}

// New returns a Writer to write a seed for the given model and using
// the given Options.
func New(model *asserts.Model, opts *Options) (*Writer, error) {
	if opts == nil {
		return nil, fmt.Errorf("internal error: Writer *Options is nil")
	}
	w := &Writer{
		model: model,
		opts:  opts,

		expectedStep: setOptionsSnapsStep,

		byNameOptSnaps:  naming.NewSnapSet(nil),
		byRefLocalSnaps: naming.NewSnapSet(nil),
		manifest:        opts.manifest(),
	}

	var treeImpl tree
	var pol policy
	if model.Grade() != asserts.ModelGradeUnset {
		// Core 20
		if opts.Label == "" {
			return nil, fmt.Errorf("internal error: cannot write UC20+ seed without Options.Label set")
		}
		mylog.Check(asserts.IsValidSystemLabel(opts.Label))

		pol = &policy20{model: model, opts: opts, warningf: w.warningf}
		treeImpl = &tree20{grade: model.Grade(), opts: opts}
	} else {
		pol = &policy16{model: model, opts: opts, warningf: w.warningf}
		treeImpl = &tree16{opts: opts}
	}

	if opts.DefaultChannel != "" {
		deflCh := mylog.Check2(channel.ParseVerbatim(opts.DefaultChannel, "_"))
		mylog.Check(pol.checkDefaultChannel(deflCh))

	}

	w.tree = treeImpl
	w.policy = pol
	return w, nil
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
			if w.expectedStep == localSnapsStep || w.expectedStep == infoDerivedStep {
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

// warningf adds a warning that can be later retrieved via Warnings.
func (w *Writer) warningf(format string, a ...interface{}) {
	w.warnings = append(w.warnings, fmt.Sprintf(format, a...))
}

// SetOptionsSnaps accepts options-referred snaps represented as OptionsSnap.
func (w *Writer) SetOptionsSnaps(optSnaps []*OptionsSnap) error {
	mylog.Check(w.checkStep(setOptionsSnapsStep))

	if len(optSnaps) == 0 {
		return nil
	}

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
			mylog.Check(naming.ValidateSnap(snapName))

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
			ch := mylog.Check2(channel.ParseVerbatim(sn.Channel, "_"))
			mylog.Check(w.policy.checkSnapChannel(ch, whichSnap))

		}
		if local {
			if w.localSnaps == nil {
				w.localSnaps = make(map[*OptionsSnap]*SeedSnap)
			}
			w.localSnaps[sn] = &SeedSnap{
				SnapRef: nil,
				Path:    sn.Path,

				local:      true,
				optionSnap: sn,
			}
		}
	}

	// used later to determine extra snaps
	w.optionsSnaps = optSnaps

	return nil
}

// SystemAlreadyExistsError is an error returned when given seed system already
// exists.
type SystemAlreadyExistsError struct {
	label string
}

func (e *SystemAlreadyExistsError) Error() string {
	return fmt.Sprintf("system %q already exists", e.label)
}

func IsSytemDirectoryExistsError(err error) bool {
	_, ok := err.(*SystemAlreadyExistsError)
	return ok
}

func (w *Writer) validationSetFromManifest(vsm *asserts.ModelValidationSet) *ManifestValidationSet {
	for _, vs := range w.manifest.AllowedValidationSets() {
		if vs.AccountID == vsm.AccountID && vs.Name == vsm.Name {
			return vs
		}
	}
	return nil
}

// finalValidationSetAtSequence returns the final AtSequence for an
// validation set. If any restrictions have been set in the manifest
// then we must use the sequence and pinning status from that instead
// of whats set in the model.
func (w *Writer) finalValidationSetAtSequence(vsm *asserts.ModelValidationSet) (*asserts.AtSequence, error) {
	atSeq := vsm.AtSequence()

	// Check the manifest for a matching entry, to handle any restrictions that
	// might have been setup.
	vs := w.validationSetFromManifest(vsm)
	if vs == nil {
		return atSeq, nil
	}

	// If the model has the validation-set pinned, this can't be
	// changed by the manifest.
	if vsm.Sequence > 0 && vs.Sequence != vsm.Sequence {
		// It's pinned by the model, then the sequence must match
		return nil, fmt.Errorf("cannot use sequence %d of %q: model requires sequence %d",
			vs.Sequence, vs.Unique(), vsm.Sequence)
	}

	// If the model does not have a sequence set, then we don't allow
	// pinning through the manifest.
	if vsm.Sequence <= 0 && vs.Pinned {
		return nil, fmt.Errorf("pinning of %q is not allowed by the model", vs.Unique())
	}

	atSeq.Sequence = vs.Sequence
	return atSeq, nil
}

func (w *Writer) fetchValidationSets(f SeedAssertionFetcher) error {
	for _, vs := range w.model.ValidationSets() {
		atSeq := mylog.Check2(w.finalValidationSetAtSequence(vs))
		mylog.Check(f.FetchSequence(atSeq))

	}
	return nil
}

// Start starts the seed writing, and fetches the necessary model assertions using
// the provided SeedAssertionFetcher (See MakeSeedAssertionFetcher). The provided
// fetcher must support the FetchSequence in case the model refers to any validation
// sets. The seed-writer assumes that the snap assertions will end up in the given db
// (writing assertions database). When the system seed directory is already present,
// SystemAlreadyExistsError is returned.
func (w *Writer) Start(db asserts.RODatabase, f SeedAssertionFetcher) error {
	mylog.Check(w.checkStep(startStep))

	if db == nil {
		return fmt.Errorf("internal error: Writer *asserts.RODatabase is nil")
	}
	if f == nil {
		return fmt.Errorf("internal error: Writer fetcher is nil")
	}
	w.db = db
	mylog.Check(f.Save(w.model))

	// Some naive tests including ubuntu-image ones use
	// unverified models

	// fetch device store assertion (and prereqs) if available
	if w.model.Store() != "" {
		mylog.Check(snapasserts.FetchStore(f, w.model.Store()))
	}
	mylog.Check(

		// fetch model validation sets if any
		w.fetchValidationSets(f))

	w.modelRefs = f.Refs()
	mylog.Check(w.tree.mkFixedDirs())

	return nil
}

// LocalSnaps returns a list of seed snaps that are local.  The writer
// delegates to produce *snap.Info for them to then be set via
// SetInfo.
// If matching snap assertions can be found as well, they should be made
// available through the AssertsFetchFunc passed to Downloaded later, the
// assumption is also that they are added to the writing assertion database.
func (w *Writer) LocalSnaps() ([]*SeedSnap, error) {
	mylog.Check(w.checkStep(localSnapsStep))

	if len(w.localSnaps) == 0 {
		return nil, nil
	}

	lsnaps := make([]*SeedSnap, 0, len(w.localSnaps))
	for _, optSnap := range w.optionsSnaps {
		if sn := w.localSnaps[optSnap]; sn != nil {
			lsnaps = append(lsnaps, sn)
		}
	}
	return lsnaps, nil
}

// InfoDerived checks the local snaps metadata provided via setting it
// into the SeedSnaps returned by the previous LocalSnaps.
func (w *Writer) InfoDerived() error {
	mylog.Check(w.checkStep(infoDerivedStep))

	// loop this way to process for consistency in the same order
	// as LocalSnaps result
	for _, optSnap := range w.optionsSnaps {
		sn := w.localSnaps[optSnap]
		if sn == nil {
			continue
		}
		if sn.Info == nil {
			return fmt.Errorf("internal error: before seedwriter.Writer.InfoDerived snap %q Info should have been set", sn.Path)
		}

		if sn.Info.ID() == "" {
			mylog.Check(
				// this check is here in case we relax the checks in
				// SetOptionsSnaps
				w.policy.allowsDangerousFeatures())
		}

		// local snap gets local revision
		if sn.Info.Revision.Unset() {
			sn.Info.Revision = snap.R(-1)
		}

		if w.byRefLocalSnaps.Contains(sn) {
			return fmt.Errorf("local snap %q is repeated in options", sn.SnapName())
		}

		// in case, merge channel given by name separately
		optSnap, _ := w.byNameOptSnaps.Lookup(sn).(*OptionsSnap)
		if optSnap != nil {
			w.consumedOptSnapNum++
		}
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
	if info.NeedsDevMode() {
		mylog.Check(w.policy.allowsDangerousFeatures())
	}
	sn.Info = info

	if sn.local {
		sn.SnapRef = info
		// nothing more to do
		return nil
	}

	p := mylog.Check2(w.tree.snapPath(sn))

	sn.Path = p
	return nil
}

// SetRedirectChannel sets the redirect channel for the SeedSnap
// for the in case there is a default track for it.
func (w *Writer) SetRedirectChannel(sn *SeedSnap, redirectChannel string) error {
	if sn.local {
		return fmt.Errorf("internal error: cannot set redirect channel for local snap %q", sn.Path)
	}
	if sn.Info == nil {
		return fmt.Errorf("internal error: before using seedwriter.Writer.SetRedirectChannel snap %q Info should have been set", sn.SnapName())
	}
	if redirectChannel == "" {
		// nothing to do
		return nil
	}
	_ := mylog.Check2(channel.ParseVerbatim(redirectChannel, "-"))

	sn.Channel = redirectChannel
	return nil
}

// Manifest returns the manifest for the current seed.
func (w *Writer) Manifest() *Manifest {
	return w.manifest
}

// snapsToDownloadSet indicates which set of snaps SnapsToDownload should compute
type snapsToDownloadSet int

const (
	toDownloadModel snapsToDownloadSet = iota
	toDownloadImplicit
	toDownloadExtra
	toDownloadExtraImplicit
)

var errSkipOptional = errors.New("skip")

func (w *Writer) modelSnapToSeed(modSnap *asserts.ModelSnap) (*SeedSnap, error) {
	sn, _ := w.byRefLocalSnaps.Lookup(modSnap).(*SeedSnap)
	var optSnap *OptionsSnap
	if sn == nil {
		// not local, to download
		optSnap, _ = w.byNameOptSnaps.Lookup(modSnap).(*OptionsSnap)
		if modSnap.Presence == "optional" && optSnap == nil {
			// an optional snap that is not confirmed
			// by an OptionsSnap entry is skipped
			return nil, errSkipOptional
		}
		sn = &SeedSnap{
			SnapRef: modSnap,

			local:      false,
			optionSnap: optSnap,
		}
	} else {
		optSnap = sn.optionSnap
	}

	channel := mylog.Check2(w.resolveChannel(modSnap.SnapName(), modSnap, optSnap))

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
		sn := mylog.Check2(w.modelSnapToSeed(modSnap))
		if err == errSkipOptional {
			continue
		}

		if !sn.local {
			toDownload = append(toDownload, sn)
		}
		if sn.optionSnap != nil {
			w.consumedOptSnapNum++
		}
		w.snapsFromModel = append(w.snapsFromModel, sn)
	}
	w.toDownloadConsideredNum = len(w.snapsFromModel) - alreadyConsidered
	w.extraSnapsGuessNum = len(w.optionsSnaps) - w.consumedOptSnapNum

	return toDownload, nil
}

func (w *Writer) modSnaps() ([]*asserts.ModelSnap, error) {
	// model snaps are accumulated/processed in the order
	//  * system snap if implicit
	//  * essential snaps (in Model.EssentialSnaps order)
	//  * not essential snaps
	modSnaps := append([]*asserts.ModelSnap{}, w.model.EssentialSnaps()...)
	if systemSnap := w.policy.systemSnap(); systemSnap != nil {
		prepend := true
		for _, modSnap := range modSnaps {
			if naming.SameSnap(modSnap, systemSnap) {
				prepend = false
				modes := modSnap.Modes
				expectedModes := systemSnap.Modes
				if len(modes) != len(expectedModes) {
					return nil, fmt.Errorf("internal error: system snap %q explicitly listed in model carries wrong modes: %q", systemSnap.SnapName(), modes)
				}
				for _, mod := range expectedModes {
					if !strutil.ListContains(modes, mod) {
						return nil, fmt.Errorf("internal error: system snap %q explicitly listed in model carries wrong modes: %q", systemSnap.SnapName(), modes)
					}
				}
				break
			}
		}
		if prepend {
			modSnaps = append([]*asserts.ModelSnap{systemSnap}, modSnaps...)
		}
	}
	modSnaps = append(modSnaps, w.model.SnapsWithoutEssential()...)
	return modSnaps, nil
}

func (w *Writer) optExtraSnaps() []*OptionsSnap {
	extra := make([]*OptionsSnap, 0, w.extraSnapsGuessNum)
	for _, optSnap := range w.optionsSnaps {
		var snapRef naming.SnapRef = optSnap
		if sn := w.localSnaps[optSnap]; sn != nil {
			snapRef = sn
		}
		if w.availableSnaps.Contains(snapRef) {
			continue
		}
		extra = append(extra, optSnap)
	}
	return extra
}

func (w *Writer) extraSnapToSeed(optSnap *OptionsSnap) (*SeedSnap, error) {
	sn := w.localSnaps[optSnap]
	if sn == nil {
		// not local, to download
		sn = &SeedSnap{
			SnapRef: optSnap,

			local:      false,
			optionSnap: optSnap,
		}
	}
	if sn.SnapName() == "" {
		return nil, fmt.Errorf("internal error: option extra snap has no associated name: %#v %#v", optSnap, sn)
	}

	channel := mylog.Check2(w.resolveChannel(sn.SnapName(), nil, optSnap))

	sn.Channel = channel
	return sn, nil
}

func (w *Writer) extraSnapsToDownload(extraSnaps []*OptionsSnap) (toDownload []*SeedSnap, err error) {
	if w.extraSnaps == nil {
		w.extraSnaps = make([]*SeedSnap, 0, len(extraSnaps))
	}
	toDownload = make([]*SeedSnap, 0, len(extraSnaps))

	alreadyConsidered := len(w.extraSnaps)
	for _, optSnap := range extraSnaps {
		sn := mylog.Check2(w.extraSnapToSeed(optSnap))

		if !sn.local {
			toDownload = append(toDownload, sn)
		}
		w.extraSnaps = append(w.extraSnaps, sn)
	}
	w.toDownloadConsideredNum = len(w.extraSnaps) - alreadyConsidered

	return toDownload, nil
}

// SnapsToDownload returns a list of seed snaps to download. Once that
// is done and their SeedSnaps Info field is set with SetInfo fields
// Downloaded should be called next.
func (w *Writer) SnapsToDownload() (snaps []*SeedSnap, err error) {
	mylog.Check(w.checkStep(snapsToDownloadStep))

	switch w.toDownload {
	case toDownloadModel:
		modSnaps := mylog.Check2(w.modSnaps())

		w.recordUsageWithThePolicy(modSnaps)

		toDownload := mylog.Check2(w.modelSnapsToDownload(modSnaps))

		if w.extraSnapsGuessNum > 0 {
			mylog.Check(
				// this check is here in case we relax the checks in
				// SetOptionsSnaps
				w.policy.allowsDangerousFeatures())
		}
		return toDownload, nil
	case toDownloadImplicit:
		return w.modelSnapsToDownload(w.policy.implicitSnaps(w.availableByMode))
	case toDownloadExtra:
		return w.extraSnapsToDownload(w.optExtraSnaps())
	case toDownloadExtraImplicit:
		return w.extraSnapsToDownload(w.policy.implicitExtraSnaps(w.availableByMode))
	default:
		panic(fmt.Sprintf("unknown to-download set: %d", w.toDownload))
	}
}

func (w *Writer) resolveChannel(whichSnap string, modSnap *asserts.ModelSnap, optSnap *OptionsSnap) (string, error) {
	var optChannel string
	if optSnap != nil {
		optChannel = optSnap.Channel
	}
	if optChannel == "" {
		optChannel = w.opts.DefaultChannel
	}

	if modSnap != nil && modSnap.PinnedTrack != "" {
		resChannel := mylog.Check2(channel.ResolvePinned(modSnap.PinnedTrack, optChannel))
		if err == channel.ErrPinnedTrackSwitch {
			return "", fmt.Errorf("option channel %q for %s has a track incompatible with the pinned track from model assertion: %s", optChannel, whichModelSnap(modSnap, w.model), modSnap.PinnedTrack)
		}

		// shouldn't happen given that we check that
		// the inputs parse before

		return resChannel, nil
	}

	var defaultChannel string
	if modSnap != nil {
		defaultChannel = modSnap.DefaultChannel
		if defaultChannel == "" {
			defaultChannel = w.policy.modelSnapDefaultChannel()
		}
	} else {
		defaultChannel = w.policy.extraSnapDefaultChannel()
	}

	resChannel := mylog.Check2(channel.Resolve(defaultChannel, optChannel))

	// shouldn't happen given that we check that
	// the inputs parse before

	return resChannel, nil
}

func (w *Writer) checkBase(info *snap.Info, modes []string) error {
	// Validity check, note that we could support this case
	// if we have a use-case but it requires changes in the
	// devicestate/firstboot.go ordering code.
	if info.Type() == snap.TypeGadget && !w.model.Classic() && info.Base != w.model.Base() {
		return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, w.model.Base())
	}

	// snap explicitly listed as not needing a base snap (e.g. a content-only snap)
	if info.Base == "none" {
		return nil
	}

	return w.policy.checkBase(info, modes, w.availableByMode)
}

func (w *Writer) recordUsageWithThePolicy(modSnaps []*asserts.ModelSnap) {
	for _, modSnap := range modSnaps {
		w.policy.recordSnapNameUsage(modSnap.Name)
	}

	for _, optSnap := range w.optionsSnaps {
		snapName := optSnap.Name
		sn := w.localSnaps[optSnap]
		if sn != nil {
			snapName = sn.Info.SnapName()
		}
		w.policy.recordSnapNameUsage(snapName)
	}
}

func isKernelSnap(sn *SeedSnap) bool {
	return sn.modelSnap != nil && sn.modelSnap.SnapType == "kernel"
}

// snapdCarryingSnapsKnown returns true once all the snaps that carry snapd
// or parts of it have been captured; they need to be known before assertions
// can be fetched with the correct max formats
func (w *Writer) snapdCarryingSnapsKnown() bool {
	return w.systemSnap != nil && (w.noKernelSnap || w.kernelSnap != nil)
}

func (w *Writer) considerForSnapdCarrying(sn *SeedSnap) {
	if w.systemSnap == nil {
		if w.policy.isSystemSnapCandidate(sn) {
			w.systemSnap = sn
			return
		}
	}
	if w.noKernelSnap {
		return
	}
	if isKernelSnap(sn) {
		w.kernelSnap = sn
		return
	}
	w.noKernelSnap = true
}

// An AssertsFetchFunc should fetch appropriate assertions for the snap sn, it
// can take into account format constraints caused by the given systemSnap and
// kernelSnap if set. The returned references are expected to be resolvable
// in the writing assertion database.
type AssertsFetchFunc func(sn, systemsSnap, kernelSnap *SeedSnap) ([]*asserts.Ref, error)

func (w *Writer) ensureARefs(upToSnap *SeedSnap, fetchAsserts AssertsFetchFunc) error {
	n := len(w.snapsFromModel)
	xn := len(w.extraSnaps)

	// apply f on the combined lists w.snapsFromModel and w.extraSnaps,
	// starting from combined index start and until hitting
	// upToSnap or the end
	applyFromUpTo := func(start int, f func(sn *SeedSnap) error) (indexAter int, err error) {
		i, indexAfter := start, start
		snaps := w.snapsFromModel
		for ; i < n+xn; i++ {
			j := i
			if j >= n {
				snaps = w.extraSnaps
				j -= n
			}
			sn := snaps[j]
			mylog.Check(f(sn))

			indexAfter++
			if sn == upToSnap {
				break
			}
		}
		return indexAfter, nil
	}

	if !w.snapdCarryingSnapsKnown() {
		// no error expected from considerForSnapdCarrying
		w.consideredForSnapdCarryingIndex, _ = applyFromUpTo(w.consideredForSnapdCarryingIndex, func(sn *SeedSnap) error {
			w.considerForSnapdCarrying(sn)
			return nil
		})
	}
	// check whether after applying considerForSnapdCarrying the snaps
	// carrying snapd are known. if they are we can start fetching
	// assertions if they aren't return here or ignore or error
	// as appropriate
	if !w.snapdCarryingSnapsKnown() {
		if upToSnap == nil && n+xn > 0 {
			if !w.policy.ignoreUndeterminedSystemSnap() {
				return fmt.Errorf("internal error: unable to determine system snap after all the snaps were considered")
			}
			// proceed anyway, ignore case
		} else {
			// not known yet, cannot proceed
			return nil
		}
	}

	indexAfter := mylog.Check2(applyFromUpTo(w.consideredForAssertionsIndex, func(sn *SeedSnap) error {
		if sn.Info.ID() == "" {
			return nil
		}
		aRefs := mylog.Check2(fetchAsserts(sn, w.systemSnap, w.kernelSnap))

		if aRefs == nil {
			return fmt.Errorf("internal error: fetching assertions for snap %q returned empty", sn.SnapName())
		}
		sn.aRefs = aRefs
		return w.checkPublisher(sn)
	}))

	w.consideredForAssertionsIndex = indexAfter
	return nil
}

func (w *Writer) downloaded(seedSnaps []*SeedSnap, fetchAsserts AssertsFetchFunc) error {
	if w.availableSnaps == nil {
		w.availableSnaps = naming.NewSnapSet(nil)
		w.availableByMode = make(map[string]*naming.SnapSet)
		w.availableByMode["run"] = naming.NewSnapSet(nil)
		w.byModeSnaps = make(map[string][]*SeedSnap)
	}

	for _, sn := range seedSnaps {
		if sn.Info == nil {
			return fmt.Errorf("internal error: before seedwriter.Writer.Downloaded snap %q Info should have been set", sn.SnapName())
		}
		w.availableSnaps.Add(sn)
		for _, mode := range sn.modes() {
			byMode := w.availableByMode[mode]
			if byMode == nil {
				byMode = naming.NewSnapSet(nil)
				w.availableByMode[mode] = byMode
			}
			if byMode.Contains(sn) {
				continue
			}
			byMode.Add(sn)
			w.byModeSnaps[mode] = append(w.byModeSnaps[mode], sn)
		}
	}

	for _, sn := range seedSnaps {
		info := sn.Info
		if !sn.local {
			if info.ID() == "" {
				return fmt.Errorf("internal error: before seedwriter.Writer.Downloaded snap %q snap-id should have been set", sn.SnapName())
			}
		}
		mylog.Check(checkType(sn, w.model))

		if info.ID() != "" {
			mylog.Check(w.ensureARefs(sn, fetchAsserts))
		}

		// TODO: optionally check that model snap name and
		// info snap name match

		needsClassic := info.NeedsClassic()
		if needsClassic {
			if !w.model.Classic() {
				return fmt.Errorf("cannot use classic snap %q in a core system", info.SnapName())
			}
			mylog.Check(w.policy.checkClassicSnap(sn))

		}

		modes := sn.modes()
		mylog.Check(w.checkBase(info, modes))

	}

	return nil
}

// Downloaded checks the downloaded snaps metadata provided via
// setting it into the SeedSnaps returned by the previous
// SnapsToDownload. It also returns whether the seed snap set is
// complete or SnapsToDownload should be called again.
// An AssertsFetchFunc must be provided for Downloaded to request to fetch snap
// assertions as appropriate.
func (w *Writer) Downloaded(fetchAsserts AssertsFetchFunc) (complete bool, err error) {
	mylog.Check(w.checkStep(downloadedStep))

	var considered []*SeedSnap
	switch w.toDownload {
	default:
		panic(fmt.Sprintf("unknown to-download set: %d", w.toDownload))
	case toDownloadImplicit:
		fallthrough
	case toDownloadModel:
		considered = w.snapsFromModel
	case toDownloadExtraImplicit:
		fallthrough
	case toDownloadExtra:
		considered = w.extraSnaps
	}

	considered = considered[len(considered)-w.toDownloadConsideredNum:]
	mylog.Check(w.downloaded(considered, fetchAsserts))

	switch w.toDownload {
	case toDownloadModel:
		implicitNeeded := mylog.Check2(w.policy.needsImplicitSnaps(w.availableByMode))

		if implicitNeeded {
			w.toDownload = toDownloadImplicit
			w.expectedStep = snapsToDownloadStep
			return false, nil
		}
		fallthrough
	case toDownloadImplicit:
		if w.extraSnapsGuessNum > 0 {
			w.toDownload = toDownloadExtra
			w.expectedStep = snapsToDownloadStep
			return false, nil
		}
	case toDownloadExtra:
		implicitNeeded := mylog.Check2(w.policy.needsImplicitSnaps(w.availableByMode))

		if implicitNeeded {
			w.toDownload = toDownloadExtraImplicit
			w.expectedStep = snapsToDownloadStep
			return false, nil
		}
	case toDownloadExtraImplicit:
		// nothing to do
		// TODO: consider generalizing the logic and optionally asking
		// the policy again
	default:
		panic(fmt.Sprintf("unknown to-download set: %d", w.toDownload))
	}
	mylog.Check(w.ensureARefs(nil, fetchAsserts))
	mylog.Check(w.checkPrereqs())

	return true, nil
}

func (w *Writer) checkPrereqs() error {
	// as we error on the first problem we want to check snaps mode by mode
	// in a fixed order; we start with run then
	// ephemeral as snaps marked as such need to be self-contained
	// then specific modes sorted
	modes := make([]string, 0, len(w.availableByMode))
	modes = append(modes, "run")
	fixed := 1
	if _, ok := w.availableByMode["ephemeral"]; ok {
		modes = append(modes, "ephemeral")
		fixed = 2
	}
	for m := range w.availableByMode {
		if m == "run" || m == "ephemeral" {
			continue
		}
		modes = append(modes, m)
	}
	sort.Strings(modes[fixed:])
	for _, m := range modes {
		mylog.Check(w.checkPrereqsInMode(m))
	}
	return nil
}

func (w *Writer) checkPrereqsInMode(mode string) error {
	nmode := len(w.byModeSnaps[mode])
	nephemeral := len(w.byModeSnaps["ephemeral"])
	var snaps []*snap.Info
	if mode != "run" && mode != "ephemeral" {
		// mode is a concrete ephemeral mode
		// (not run, not ephemeral which is the set of snaps
		//  shared by all ephemeral modes)
		// so we include snap marked for any ephemeral mode
		snaps = make([]*snap.Info, 0, nmode+nephemeral)
		for _, sn := range w.byModeSnaps["ephemeral"] {
			snaps = append(snaps, sn.Info)
		}
	} else {
		snaps = make([]*snap.Info, 0, nmode)
	}
	for _, sn := range w.byModeSnaps[mode] {
		snaps = append(snaps, sn.Info)
	}
	warns, errs := snap.ValidateBasesAndProviders(snaps)
	if errs != nil {
		var errPrefix string
		// XXX TODO: return an error that subsumes all the errors
		if mode == "run" {
			errPrefix = "prerequisites need to be added explicitly"
		} else {
			errPrefix = fmt.Sprintf("prerequisites need to be added explicitly for relevant mode %s", mode)
		}
		return fmt.Errorf("%s: %v", errPrefix, errs[0])
	}
	wfmt := "%v"
	if mode != "run" {
		wfmt = fmt.Sprintf("prerequisites for mode %s: %%v", mode)
	}
	for _, warn := range warns {
		w.warningf(wfmt, warn)
	}
	return nil
}

func (w *Writer) checkPublisher(sn *SeedSnap) error {
	if sn.local && sn.aRefs == nil {
		// nothing to do
		return nil
	}
	info := sn.Info
	var kind string
	switch info.Type() {
	case snap.TypeKernel:
		kind = "kernel"
	case snap.TypeGadget:
		kind = "gadget"
	default:
		return nil
	}
	// TODO: share helpers with devicestate if the policy becomes much more complicated
	snapDecl := mylog.Check2(w.snapDecl(sn))

	publisher := snapDecl.PublisherID()
	if publisher != w.model.BrandID() && publisher != "canonical" {
		return fmt.Errorf("cannot use %s %q published by %q for model by %q", kind, info.SnapName(), publisher, w.model.BrandID())
	}
	return nil
}

func (w *Writer) snapDecl(sn *SeedSnap) (*asserts.SnapDeclaration, error) {
	for _, ref := range sn.aRefs {
		if ref.Type == asserts.SnapDeclarationType {
			a := mylog.Check2(ref.Resolve(w.db.Find))

			return a.(*asserts.SnapDeclaration), nil
		}
	}
	return nil, fmt.Errorf("internal error: snap %q has no snap-declaration set", sn.SnapName())
}

// Warnings returns the warning messages produced so far. No warnings
// should be generated after Downloaded signaled complete.
func (w *Writer) Warnings() []string {
	return w.warnings
}

func (w *Writer) resolveValidationSetAssertion(seq *asserts.AtSequence) (asserts.Assertion, error) {
	if seq.Sequence <= 0 {
		hdrs := mylog.Check2(asserts.HeadersFromSequenceKey(seq.Type, seq.SequenceKey))

		return w.db.FindSequence(seq.Type, hdrs, -1, seq.Type.MaxSupportedFormat())
	}
	return seq.Resolve(w.db.Find)
}

func (w *Writer) validationSetAsserts() (map[*asserts.AtSequence]*asserts.ValidationSet, error) {
	vsAsserts := make(map[*asserts.AtSequence]*asserts.ValidationSet)
	vss := w.model.ValidationSets()
	for _, vs := range vss {
		atSeq := mylog.Check2(w.finalValidationSetAtSequence(vs))

		a := mylog.Check2(w.resolveValidationSetAssertion(atSeq))

		vsAsserts[atSeq] = a.(*asserts.ValidationSet)
	}
	return vsAsserts, nil
}

func (w *Writer) validationSets() (*snapasserts.ValidationSets, error) {
	vss := mylog.Check2(w.validationSetAsserts())

	valsets := snapasserts.NewValidationSets()
	for _, vs := range vss {
		valsets.Add(vs)
	}
	return valsets, nil
}

func (w *Writer) installedSnaps() []*snapasserts.InstalledSnap {
	installedSnap := func(snap *SeedSnap) *snapasserts.InstalledSnap {
		return snapasserts.NewInstalledSnap(snap.SnapName(), snap.ID(), snap.Info.Revision)
	}

	var installedSnaps []*snapasserts.InstalledSnap
	for _, sn := range w.snapsFromModel {
		installedSnaps = append(installedSnaps, installedSnap(sn))
	}
	for _, sn := range w.extraSnaps {
		installedSnaps = append(installedSnaps, installedSnap(sn))
	}
	return installedSnaps
}

func (w *Writer) checkStepCompleted(step writerStep) bool {
	// expectedStep is the next step it needs to perform. If that
	// is higher (as they are int based values), then the step we
	// are checking against has completed.
	return w.expectedStep > step
}

// CheckValidationSets validates all snaps that are to be seeded against any
// specified validation set. Info for all seed snaps must have been derived prior
// to this call.
func (w *Writer) CheckValidationSets() error {
	// It makes no sense to check validation-sets before all required snaps
	// have been resolved and downloaded. Ensure that this is not called before
	// the Downloaded step has completed.
	if !w.checkStepCompleted(downloadedStep) {
		return fmt.Errorf("internal error: seedwriter.Writer cannot check validation-sets before Downloaded signaled complete")
	}

	valsets := mylog.Check2(w.validationSets())
	mylog.Check(

		// Check for validation set conflicts first, then we check all
		// the seeded snaps against them
		valsets.Conflict())

	// Make one aggregated list of snaps we are seeding, then check that
	// against the validation sets
	installedSnaps := w.installedSnaps()
	return valsets.CheckInstalledSnaps(installedSnaps, nil)
}

// SeedSnaps checks seed snaps and copies local snaps into the seed using copySnap.
func (w *Writer) SeedSnaps(copySnap func(name, src, dst string) error) error {
	mylog.Check(w.checkStep(seedSnapsStep))

	seedSnaps := func(snaps []*SeedSnap) error {
		for _, sn := range snaps {
			info := sn.Info
			if !sn.local {
				expectedPath := mylog.Check2(w.tree.snapPath(sn))

				if sn.Path != expectedPath {
					return fmt.Errorf("internal error: before seedwriter.Writer.SeedSnaps snap %q Path should have been set to %q", sn.SnapName(), expectedPath)
				}
				if !osutil.FileExists(expectedPath) {
					return fmt.Errorf("internal error: before seedwriter.Writer.SeedSnaps snap file %q should exist", expectedPath)
				}
			} else {
				var snapPath func(*SeedSnap) (string, error)
				if sn.Info.ID() != "" {
					// actually asserted
					snapPath = w.tree.snapPath
				} else {
					// purely local
					snapPath = w.tree.localSnapPath
				}
				dst := mylog.Check2(snapPath(sn))
				mylog.Check(copySnap(info.SnapName(), sn.Path, dst))

				// record final destination path
				sn.Path = dst
			}
			if !info.Revision.Unset() {
				mylog.Check(w.manifest.MarkSnapRevisionSeeded(sn.Info.SnapName(), sn.Info.Revision))
			}
		}
		return nil
	}
	mylog.Check(seedSnaps(w.snapsFromModel))
	mylog.Check(seedSnaps(w.extraSnaps))

	return nil
}

func (w *Writer) markValidationSetsSeeded() error {
	vsm := mylog.Check2(w.validationSetAsserts())

	for seq, vs := range vsm {
		mylog.Check(w.manifest.MarkValidationSetSeeded(vs, seq.Pinned))
	}
	return nil
}

// WriteMeta writes seed metadata and assertions into the seed.
func (w *Writer) WriteMeta() error {
	mylog.Check(w.checkStep(writeMetaStep))

	if w.opts.ManifestPath != "" {
		mylog.Check(
			// Mark validation sets seeded in the manifest if the options
			// are set to produce a manifest.
			w.markValidationSetsSeeded())
		mylog.Check(w.manifest.Write(w.opts.ManifestPath))

	}

	snapsFromModel := w.snapsFromModel
	extraSnaps := w.extraSnaps
	mylog.Check(w.tree.writeAssertions(w.db, w.modelRefs, snapsFromModel, extraSnaps))

	return w.tree.writeMeta(snapsFromModel, extraSnaps)
}

// query accessors

func (w *Writer) checkSnapsAccessor() error {
	if !w.checkStepCompleted(downloadedStep) {
		return fmt.Errorf("internal error: seedwriter.Writer cannot query seed snaps before Downloaded signaled complete")
	}
	return nil
}

// BootSnaps returns the seed snaps involved in the boot process.
// It can be invoked only after Downloaded returns complete ==
// true. It returns an error for classic models as for those no snaps
// participate in boot before user space.
func (w *Writer) BootSnaps() ([]*SeedSnap, error) {
	mylog.Check(w.checkSnapsAccessor())

	if w.model.Classic() {
		return nil, fmt.Errorf("no snaps participating in boot on classic")
	}
	var bootSnaps []*SeedSnap
	for _, sn := range w.snapsFromModel {
		bootSnaps = append(bootSnaps, sn)
		if sn.Info.Type() == snap.TypeGadget {
			break
		}
	}
	return bootSnaps, nil
}

// UnassertedSnaps returns references for all unasserted snaps in the seed.
// It can be invoked only after Downloaded returns complete ==
// true.
func (w *Writer) UnassertedSnaps() ([]naming.SnapRef, error) {
	mylog.Check(w.checkSnapsAccessor())

	var res []naming.SnapRef
	for _, sn := range w.snapsFromModel {
		if sn.Info.ID() != "" {
			continue
		}
		res = append(res, sn.SnapRef)
	}

	for _, sn := range w.extraSnaps {
		if sn.Info.ID() != "" {
			continue
		}
		res = append(res, sn.SnapRef)
	}
	return res, nil
}
