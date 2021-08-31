// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package seed

/* ATTN this should *not* use:

* dirs package: it is passed an explicit directory to work on

* release.OnClassic: it assumes classic based on the model classic
  option; consistency between system and model can/must be enforced
  elsewhere

*/

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

type seed20 struct {
	systemDir string

	db asserts.RODatabase

	model *asserts.Model

	snapDeclsByID   map[string]*asserts.SnapDeclaration
	snapDeclsByName map[string]*asserts.SnapDeclaration

	snapRevsByID map[string]*asserts.SnapRevision

	optSnaps    []*internal.Snap20
	optSnapsIdx int

	auxInfos map[string]*internal.AuxInfo20

	metaFilesLoaded bool

	essCache map[string]*Snap

	snaps []*Snap
	// modes holds a matching applicable modes set for each snap in snaps
	modes             [][]string
	essentialSnapsNum int
}

func (s *seed20) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if db == nil {
		// a db was not provided, create an internal temporary one
		var err error
		db, commitTo, err = newMemAssertionsDB(nil)
		if err != nil {
			return err
		}
	}

	assertsDir := filepath.Join(s.systemDir, "assertions")
	// collect assertions that are not the model
	var declRefs []*asserts.Ref
	var revRefs []*asserts.Ref
	checkAssertion := func(ref *asserts.Ref) error {
		switch ref.Type {
		case asserts.ModelType:
			return fmt.Errorf("system cannot have any model assertion but the one in the system model assertion file")
		case asserts.SnapDeclarationType:
			declRefs = append(declRefs, ref)
		case asserts.SnapRevisionType:
			revRefs = append(revRefs, ref)
		}
		return nil
	}

	batch, err := loadAssertions(assertsDir, checkAssertion)
	if err != nil {
		return err
	}

	refs, err := readAsserts(batch, filepath.Join(s.systemDir, "model"))
	if err != nil {
		return fmt.Errorf("cannot read model assertion: %v", err)
	}
	if len(refs) != 1 || refs[0].Type != asserts.ModelType {
		return fmt.Errorf("system model assertion file must contain exactly the model assertion")
	}
	modelRef := refs[0]

	if len(declRefs) != len(revRefs) {
		return fmt.Errorf("system unexpectedly holds a different number of snap-declaration than snap-revision assertions")
	}

	// this also verifies the consistency of all of them
	if err := commitTo(batch); err != nil {
		return err
	}

	find := func(ref *asserts.Ref) (asserts.Assertion, error) {
		a, err := ref.Resolve(db.Find)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot find just accepted assertion %v: %v", ref, err)
		}
		return a, nil
	}

	a, err := find(modelRef)
	if err != nil {
		return err
	}
	modelAssertion := a.(*asserts.Model)

	snapDeclsByName := make(map[string]*asserts.SnapDeclaration, len(declRefs))
	snapDeclsByID := make(map[string]*asserts.SnapDeclaration, len(declRefs))

	for _, declRef := range declRefs {
		a, err := find(declRef)
		if err != nil {
			return err
		}
		snapDecl := a.(*asserts.SnapDeclaration)
		snapDeclsByID[snapDecl.SnapID()] = snapDecl
		if snapDecl1 := snapDeclsByName[snapDecl.SnapName()]; snapDecl1 != nil {
			return fmt.Errorf("cannot have multiple snap-declarations for the same snap-name: %s", snapDecl.SnapName())
		}
		snapDeclsByName[snapDecl.SnapName()] = snapDecl
	}

	snapRevsByID := make(map[string]*asserts.SnapRevision, len(revRefs))

	for _, revRef := range revRefs {
		a, err := find(revRef)
		if err != nil {
			return err
		}
		snapRevision := a.(*asserts.SnapRevision)
		snapRevision1 := snapRevsByID[snapRevision.SnapID()]
		if snapRevision1 != nil {
			if snapRevision1.SnapRevision() != snapRevision.SnapRevision() {
				return fmt.Errorf("cannot have multiple snap-revisions for the same snap-id: %s", snapRevision1.SnapID())
			}
		} else {
			snapRevsByID[snapRevision.SnapID()] = snapRevision
		}
	}

	// remember db for later use
	s.db = db
	// remember
	s.model = modelAssertion
	s.snapDeclsByID = snapDeclsByID
	s.snapDeclsByName = snapDeclsByName
	s.snapRevsByID = snapRevsByID

	return nil
}

func (s *seed20) Model() *asserts.Model {
	if s.model == nil {
		panic("internal error: model assertion unset (LoadAssertions not called)")
	}
	return s.model
}

func (s *seed20) Brand() (*asserts.Account, error) {
	return findBrand(s, s.db)
}

func (s *seed20) UsesSnapdSnap() bool {
	return true
}

func (s *seed20) loadOptions() error {
	if s.model.Grade() != asserts.ModelDangerous {
		// options.yaml is not supported for grade > dangerous
		return nil
	}
	optionsFn := filepath.Join(s.systemDir, "options.yaml")
	if !osutil.FileExists(optionsFn) {
		// missing
		return nil
	}
	options20, err := internal.ReadOptions20(optionsFn)
	if err != nil {
		return err
	}
	s.optSnaps = options20.Snaps
	return nil
}

func (s *seed20) nextOptSnap(modSnap *asserts.ModelSnap) (optSnap *internal.Snap20, done bool) {
	// we can merge model snaps and options snaps because
	// both seed20.go and writer.go follow the order:
	// system snap, model.EssentialSnaps(), model.SnapsWithoutEssential()
	if s.optSnapsIdx == len(s.optSnaps) {
		return nil, true
	}
	next := s.optSnaps[s.optSnapsIdx]
	if modSnap == nil || naming.SameSnap(next, modSnap) {
		s.optSnapsIdx++
		return next, false
	}
	return nil, false
}

func (s *seed20) loadAuxInfos() error {
	auxInfoFn := filepath.Join(s.systemDir, "snaps", "aux-info.json")
	if !osutil.FileExists(auxInfoFn) {
		// missing
		return nil
	}

	f, err := os.Open(auxInfoFn)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if err := dec.Decode(&s.auxInfos); err != nil {
		return fmt.Errorf("cannot decode aux-info.json: %v", err)
	}
	return nil
}

type noSnapDeclarationError struct {
	snapRef naming.SnapRef
}

func (e *noSnapDeclarationError) Error() string {
	snapID := e.snapRef.ID()
	if snapID != "" {
		return fmt.Sprintf("cannot find snap-declaration for snap-id: %s", snapID)
	}
	return fmt.Sprintf("cannot find snap-declaration for snap name: %s", e.snapRef.SnapName())
}

func (s *seed20) lookupVerifiedRevision(snapRef naming.SnapRef, snapsDir string) (snapPath string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, err error) {
	snapID := snapRef.ID()
	if snapID != "" {
		snapDecl = s.snapDeclsByID[snapID]
		if snapDecl == nil {
			return "", nil, nil, &noSnapDeclarationError{snapRef}
		}
	} else {
		if s.model.Grade() != asserts.ModelDangerous {
			return "", nil, nil, fmt.Errorf("all system snaps must be identified by snap-id, missing for %q", snapRef.SnapName())
		}
		snapName := snapRef.SnapName()
		snapDecl = s.snapDeclsByName[snapName]
		if snapDecl == nil {
			return "", nil, nil, &noSnapDeclarationError{snapRef}
		}
		snapID = snapDecl.SnapID()
	}

	snapRev = s.snapRevsByID[snapID]
	if snapRev == nil {
		return "", nil, nil, fmt.Errorf("internal error: cannot find snap-revision for snap-id: %s", snapID)
	}

	snapName := snapDecl.SnapName()
	snapPath = filepath.Join(s.systemDir, snapsDir, fmt.Sprintf("%s_%d.snap", snapName, snapRev.SnapRevision()))

	fi, err := os.Stat(snapPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("cannot stat snap: %v", err)
	}

	if fi.Size() != int64(snapRev.SnapSize()) {
		return "", nil, nil, fmt.Errorf("cannot validate %q for snap %q (snap-id %q), wrong size", snapPath, snapName, snapID)
	}

	snapSHA3_384, _, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return "", nil, nil, err
	}

	if snapSHA3_384 != snapRev.SnapSHA3_384() {
		return "", nil, nil, fmt.Errorf("cannot validate %q for snap %q (snap-id %q), hash mismatch with snap-revision", snapPath, snapName, snapID)

	}

	return snapPath, snapRev, snapDecl, nil
}

func (s *seed20) lookupSnap(snapRef naming.SnapRef, optSnap *internal.Snap20, channel string, snapsDir string, cache map[string]*Snap, tm timings.Measurer) (*Snap, error) {
	snapKey := fmt.Sprintf("%s:%s", snapRef.ID(), snapRef.SnapName())
	seedSnap := cache[snapKey]
	if seedSnap != nil {
		return seedSnap, nil
	}

	if optSnap != nil && optSnap.Channel != "" {
		channel = optSnap.Channel
	}

	var path string
	var sideInfo *snap.SideInfo
	if optSnap != nil && optSnap.Unasserted != "" {
		path = filepath.Join(s.systemDir, "snaps", optSnap.Unasserted)
		info, err := readInfo(path, nil)
		if err != nil {
			return nil, fmt.Errorf("cannot read unasserted snap: %v", err)
		}
		sideInfo = &snap.SideInfo{RealName: info.SnapName()}
		// suppress channel
		channel = ""
	} else {
		var err error
		timings.Run(tm, "derive-side-info", fmt.Sprintf("hash and derive side info for snap %q", snapRef.SnapName()), func(nested timings.Measurer) {
			var snapRev *asserts.SnapRevision
			var snapDecl *asserts.SnapDeclaration
			path, snapRev, snapDecl, err = s.lookupVerifiedRevision(snapRef, snapsDir)
			if err == nil {
				sideInfo = snapasserts.SideInfoFromSnapAssertions(snapDecl, snapRev)
			}
		})
		if err != nil {
			return nil, err
		}
	}

	// complement with aux-info.json information
	auxInfo := s.auxInfos[sideInfo.SnapID]
	if auxInfo != nil {
		sideInfo.Private = auxInfo.Private
		// TODO: consider whether to use this if we have links
		sideInfo.EditedContact = auxInfo.Contact
	}

	seedSnap = &Snap{
		Path: path,

		SideInfo: sideInfo,

		Channel: channel,
	}

	if cache != nil {
		cache[snapKey] = seedSnap
	}

	return seedSnap, nil
}

func (s *seed20) addSnap(snapRef naming.SnapRef, optSnap *internal.Snap20, modes []string, channel string, snapsDir string, cache map[string]*Snap, tm timings.Measurer) (*Snap, error) {
	seedSnap, err := s.lookupSnap(snapRef, optSnap, channel, snapsDir, cache, tm)
	if err != nil {
		return nil, err
	}

	s.snaps = append(s.snaps, seedSnap)
	s.modes = append(s.modes, modes)

	return seedSnap, nil
}

var errFiltered = errors.New("filtered out")

func (s *seed20) addModelSnap(modelSnap *asserts.ModelSnap, essential bool, filter func(*asserts.ModelSnap) bool, cache map[string]*Snap, tm timings.Measurer) (*Snap, error) {
	optSnap, _ := s.nextOptSnap(modelSnap)
	if filter != nil && !filter(modelSnap) {
		return nil, errFiltered
	}
	seedSnap, err := s.addSnap(modelSnap, optSnap, modelSnap.Modes, modelSnap.DefaultChannel, "../../snaps", cache, tm)
	if err != nil {
		return nil, err
	}

	seedSnap.Essential = essential
	seedSnap.Required = essential || modelSnap.Presence == "required"
	if essential {
		seedSnap.EssentialType = snapTypeFromModel(modelSnap)
		s.essentialSnapsNum++
	}

	return seedSnap, nil
}

func (s *seed20) LoadMeta(tm timings.Measurer) error {
	if err := s.loadEssentialMeta(nil, tm); err != nil {
		return err
	}
	if err := s.loadModelRestMeta(tm); err != nil {
		return err
	}

	// extra snaps
	runMode := []string{"run"}
	for {
		optSnap, done := s.nextOptSnap(nil)
		if done {
			break
		}

		_, err := s.addSnap(optSnap, optSnap, runMode, "latest/stable", "snaps", nil, tm)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *seed20) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	filterEssential := essentialSnapTypesToModelFilter(essentialTypes)

	if err := s.loadEssentialMeta(filterEssential, tm); err != nil {
		return err
	}

	if s.essentialSnapsNum != len(essentialTypes) {
		// did not find all the explicitly asked essential types
		return fmt.Errorf("model does not specify all the requested essential snaps: %v", essentialTypes)
	}

	return nil
}

func (s *seed20) loadMetaFiles() error {
	if s.metaFilesLoaded {
		return nil
	}

	if err := s.loadOptions(); err != nil {
		return err
	}

	if err := s.loadAuxInfos(); err != nil {
		return err
	}

	s.metaFilesLoaded = true
	return nil
}

func (s *seed20) resetSnaps() {
	// setup essential snaps cache
	if s.essCache == nil {
		// 4 = snapd+base+kernel+gadget
		s.essCache = make(map[string]*Snap, 4)
	}

	s.optSnapsIdx = 0
	s.snaps = nil
	s.modes = nil
	s.essentialSnapsNum = 0
}

func (s *seed20) loadEssentialMeta(filterEssential func(*asserts.ModelSnap) bool, tm timings.Measurer) error {
	model := s.Model()

	if err := s.loadMetaFiles(); err != nil {
		return err
	}

	s.resetSnaps()

	essSnaps := model.EssentialSnaps()
	const essential = true

	// an explicit snapd is the first of all of snaps
	if essSnaps[0].SnapType != "snapd" {
		snapdSnap := internal.MakeSystemSnap("snapd", "latest/stable", []string{"run", "ephemeral"})
		if _, err := s.addModelSnap(snapdSnap, essential, filterEssential, s.essCache, tm); err != nil && err != errFiltered {
			return err
		}
	}

	for _, modelSnap := range essSnaps {
		seedSnap, err := s.addModelSnap(modelSnap, essential, filterEssential, s.essCache, tm)
		if err != nil {
			if err == errFiltered {
				continue
			}
			return err
		}
		if modelSnap.SnapType == "gadget" {
			// sanity
			info, err := readInfo(seedSnap.Path, seedSnap.SideInfo)
			if err != nil {
				return err
			}
			if info.Base != model.Base() {
				return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, model.Base())
			}
			// TODO: when we allow extend models for classic
			// we need to add the gadget base here
		}
	}

	return nil
}

func (s *seed20) loadModelRestMeta(tm timings.Measurer) error {
	model := s.Model()

	const notEssential = false
	for _, modelSnap := range model.SnapsWithoutEssential() {
		_, err := s.addModelSnap(modelSnap, notEssential, nil, nil, tm)
		if err != nil {
			if _, ok := err.(*noSnapDeclarationError); ok && modelSnap.Presence == "optional" {
				// skipped optional snap is ok
				continue
			}
			return err
		}
	}

	return nil
}

func (s *seed20) EssentialSnaps() []*Snap {
	return s.snaps[:s.essentialSnapsNum]
}

func (s *seed20) ModeSnaps(mode string) ([]*Snap, error) {
	snaps := s.snaps[s.essentialSnapsNum:]
	modes := s.modes[s.essentialSnapsNum:]
	nGuess := len(snaps)
	ephemeral := mode != "run"
	if ephemeral {
		nGuess /= 2
	}
	res := make([]*Snap, 0, nGuess)
	for i, snap := range snaps {
		if !strutil.ListContains(modes[i], mode) {
			if !ephemeral || !strutil.ListContains(modes[i], "ephemeral") {
				continue
			}
		}
		res = append(res, snap)
	}
	return res, nil
}
