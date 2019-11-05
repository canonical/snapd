// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed/internal"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/timings"
)

type seed20 struct {
	systemDir string

	db asserts.RODatabase

	model *asserts.Model

	snapDeclsByID   map[string]*asserts.SnapDeclaration
	snapDeclsByName map[string]*asserts.SnapDeclaration

	snapRevsByID map[string]*asserts.SnapRevision

	auxInfos map[string]*internal.AuxInfo20

	snaps             []*Snap
	essentialSnapsNum int
}

func (s *seed20) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	if db == nil {
		// a db was not provided, create an internal temporary one
		var err error
		db, commitTo, err = newMemAssertionsDB()
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

func (s *seed20) Model() (*asserts.Model, error) {
	if s.model == nil {
		return nil, fmt.Errorf("internal error: model assertion unset")
	}
	return s.model, nil
}

func (s *seed20) UsesSnapdSnap() bool {
	return true
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

func (s *seed20) lookupVerifiedRevision(snapRef naming.SnapRef) (snapPath string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, err error) {
	snapID := snapRef.ID()
	if snapID != "" {
		snapDecl = s.snapDeclsByID[snapID]
		if snapDecl == nil {
			return "", nil, nil, fmt.Errorf("cannot find snap-declaration for snap-id: %s", snapID)
		}
	} else {
		if s.model.Grade() != asserts.ModelDangerous && snapRef.SnapName() != "snapd" && snapRef.SnapName() != s.model.Base() /* TODO: use snap-id for snapd*/ {
			return "", nil, nil, fmt.Errorf("all system snaps must be identified by snap-id, missing for %q", snapRef.SnapName())
		}
		snapName := snapRef.SnapName()
		snapDecl = s.snapDeclsByName[snapName]
		if snapDecl == nil {
			return "", nil, nil, fmt.Errorf("cannot find snap-declaration for snap name: %s", snapName)
		}
		snapID = snapDecl.SnapID()
	}

	snapRev = s.snapRevsByID[snapID]
	if snapRev == nil {
		return "", nil, nil, fmt.Errorf("cannot find snap-revision for snap-id: %s", snapID)
	}

	snapName := snapDecl.SnapName()
	snapPath = filepath.Join(s.systemDir, "../../snaps", fmt.Sprintf("%s_%d.snap", snapName, snapRev.SnapRevision()))

	fi, err := os.Stat(snapPath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("cannot stat snap %q: %v", snapPath, err)
	}

	if fi.Size() != int64(snapRev.SnapSize()) {
		return "", nil, nil, fmt.Errorf("cannot validate snap %q for snap %q (snap-id %q), wrong size", snapPath, snapName, snapID)
	}

	snapSHA3_384, _, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return "", nil, nil, err
	}

	if snapSHA3_384 != snapRev.SnapSHA3_384() {
		return "", nil, nil, fmt.Errorf("cannot validate snap %q for snap %q (snap-id %q), hash mismatch with snap-revision", snapPath, snapName, snapID)

	}

	return snapPath, snapRev, snapDecl, nil
}

func (s *seed20) addModelSnap(modelSnap *asserts.ModelSnap, essential bool, tm timings.Measurer) (*Snap, error) {
	// TODO|XXX: support optional snaps correctly
	// XXX consider options.yaml for grade dangerous
	// XXX unasserted case
	var path string
	var sideInfo *snap.SideInfo
	var err error
	timings.Run(tm, "derive-side-info", fmt.Sprintf("hash and derive side info for snap %q", modelSnap.SnapName()), func(nested timings.Measurer) {
		var snapRev *asserts.SnapRevision
		var snapDecl *asserts.SnapDeclaration
		path, snapRev, snapDecl, err = s.lookupVerifiedRevision(modelSnap)
		if err == nil {
			sideInfo = snapasserts.SideInfoFromSnapAssertions(snapDecl, snapRev)
		}
	})
	if err != nil {
		return nil, err
	}

	// complement with aux-info.json information
	auxInfo := s.auxInfos[sideInfo.SnapID]
	if auxInfo != nil {
		sideInfo.Private = auxInfo.Private
		sideInfo.Contact = auxInfo.Contact
	}

	seedSnap := &Snap{
		Path: path,

		SideInfo: sideInfo,

		Essential: essential,
		Required:  essential || modelSnap.Presence == "required",

		Channel: modelSnap.DefaultChannel,
	}

	s.snaps = append(s.snaps, seedSnap)
	if essential {
		s.essentialSnapsNum++
	}

	return seedSnap, nil
}

func (s *seed20) LoadMeta(tm timings.Measurer) error {
	model, err := s.Model()
	if err != nil {
		return err
	}

	if err := s.loadAuxInfos(); err != nil {
		return err
	}

	snapdSnap := internal.MakeSystemSnap("snapd", "latest/stable", []string{"run", "ephemeral"})
	if _, err := s.addModelSnap(snapdSnap, true, tm); err != nil {
		return err
	}

	essential := true
	for _, modelSnap := range model.AllSnaps() {
		seedSnap, err := s.addModelSnap(modelSnap, essential, tm)
		if err != nil {
			return err
		}
		if modelSnap.SnapType == "gadget" {
			// sanity
			snapf, err := snap.Open(seedSnap.Path)
			if err != nil {
				return err
			}
			info, err := snap.ReadInfoFromSnapFile(snapf, seedSnap.SideInfo)
			if err != nil {
				return err
			}
			if info.Base != model.Base() {
				return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, model.Base())
			}
			// TODO: when we allow extend models for classic
			// we need to add the gadget base here

			// done with essential snaps
			essential = false
		}
	}

	return nil
}

func (s *seed20) EssentialSnaps() []*Snap {
	return s.snaps[:s.essentialSnapsNum]
}

func (s *seed20) ModeSnaps(mode string) ([]*Snap, error) {
	if mode != "run" {
		// XXX
		return nil, fmt.Errorf("internal error: only run mode supported atm")
	}
	return s.snaps[s.essentialSnapsNum:], nil
}
