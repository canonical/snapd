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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
)

type recoverySystem struct {
	dir      string
	snapsDir string

	db *asserts.Database

	model *asserts.Model

	snapDeclsByID   map[string]*asserts.SnapDeclaration
	snapDeclsByName map[string]*asserts.SnapDeclaration

	snapRevsByID map[string]*asserts.SnapRevision
}

var trusted = sysdb.Trusted()

func newRecoverySystem(dir string) (*recoverySystem, error) {
	dir = filepath.Clean(dir)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   trusted,
	})
	if err != nil {
		return nil, err
	}

	return &recoverySystem{
		dir:      dir,
		snapsDir: filepath.Join(filepath.Dir(filepath.Dir(dir)), "snaps"),
		db:       db,
	}, nil
}

// XXX with the new model assertion we will always have the snap-id
func (s *recoverySystem) lookupVerifiedRevisionByName(snapName string) (snapPath string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, err error) {
	snapDecl = s.snapDeclsByName[snapName]
	if snapDecl == nil {
		return "", nil, nil, fmt.Errorf("cannot find snap-declaration for snap name: %s", snapName)
	}

	return s.lookupVerifiedRevisionByID(snapDecl.SnapID())
}

func (s *recoverySystem) verifyGadget() error {
	_, _, snapDecl, err := s.lookupVerifiedRevisionByName(s.model.Gadget())
	if err != nil {
		return err
	}
	if snapDecl.PublisherID() != s.model.BrandID() && snapDecl.PublisherID() != "canonical" {
		return fmt.Errorf("gadget publisher must match the brand-id")
	}

	// TODO: hash is valid: open to verify type etc?
	return nil
}

func (s *recoverySystem) lookupVerifiedRevisionByID(snapID string) (snapPath string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, err error) {
	snapDecl = s.snapDeclsByID[snapID]
	if snapDecl == nil {
		return "", nil, nil, fmt.Errorf("cannot find snap-declaration for snap-id: %s", snapID)
	}
	snapRev = s.snapRevsByID[snapID]
	if snapRev == nil {
		return "", nil, nil, fmt.Errorf("cannot find snap-revision for snap-id: %s", snapID)
	}

	snapName := snapDecl.SnapName()
	snapPath = filepath.Join(s.snapsDir, fmt.Sprintf("%s_%d.snap", snapName, snapRev.SnapRevision()))

	fi, err := os.Stat(snapPath)
	if err != nil {
		// TODO: fallback search based on filesize, digest if not found?
		return "", nil, nil, fmt.Errorf("cannot stat snap %q: %v", snapPath, err)
	}

	if fi.Size() != int64(snapRev.SnapSize()) {
		return "", nil, nil, fmt.Errorf("cannot validate snap %q for snap %q (snap-id %q), wrong size", snapPath, snapName, snapID)
	}

	snapSHA3_384, _, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return "", nil, nil, err
	}

	// TODO: temp cache for digests if we have size based fallbacks
	if snapSHA3_384 != snapRev.SnapSHA3_384() {
		return "", nil, nil, fmt.Errorf("cannot validate snap %q for snap %q (snap-id %q), hash mismatch with snap-revision", snapPath, snapName, snapID)

	}

	return snapPath, snapRev, snapDecl, nil
}

func (s *recoverySystem) loadAssertions() error {
	// collect
	var modelRef *asserts.Ref
	var declRefs []*asserts.Ref
	var revRefs []*asserts.Ref

	batch := NewBatch()

	modelPath := filepath.Join(s.dir, "model")
	refs, err := readAsserts(batch, modelPath)
	if err != nil {
		return fmt.Errorf("cannot read model assertion: %v", err)
	}
	if len(refs) != 1 || refs[0].Type != asserts.ModelType {
		return fmt.Errorf("cannot proceed, expected exactly one model assertion in: %s", modelPath)
	}
	modelRef = refs[0]

	assertDir := filepath.Join(s.dir, "assertions")
	dc, err := ioutil.ReadDir(assertDir)
	if err != nil {
		return fmt.Errorf("cannot read assertions dir: %s", err)
	}
	for _, fi := range dc {
		fn := filepath.Join(assertDir, fi.Name())
		refs, err := readAsserts(batch, fn)
		if err != nil {
			return fmt.Errorf("cannot read assertions: %s", err)
		}
		for _, ref := range refs {
			switch ref.Type {
			case asserts.ModelType:
				// XXX for now, actually we don't want models outside of /model
				if modelRef != nil && modelRef.Unique() != ref.Unique() {
					return fmt.Errorf("cannot have more than one model assertion")
				}
			case asserts.SnapDeclarationType:
				declRefs = append(declRefs, ref)
			case asserts.SnapRevisionType:
				revRefs = append(revRefs, ref)
			}
		}
	}

	db := s.db

	// this also verifies the consistency of all of them
	if err := batch.CommitTo(db); err != nil {
		return err
	}

	find := func(ref *asserts.Ref) (asserts.Assertion, error) {
		a, err := ref.Resolve(db.Find)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot find just accepted assertion %v: %v", modelRef, err)
		}
		return a, nil
	}

	a, err := find(modelRef)
	if err != nil {
		return err
	}
	modelAssertion := a.(*asserts.Model)

	snapDeclsByName := make(map[string]*asserts.SnapDeclaration)
	snapDeclsByID := make(map[string]*asserts.SnapDeclaration)

	for _, declRef := range declRefs {
		a, err := find(declRef)
		if err != nil {
			return err
		}
		snapDecl := a.(*asserts.SnapDeclaration)
		snapDeclsByName[snapDecl.SnapName()] = snapDecl
		snapDeclsByID[snapDecl.SnapID()] = snapDecl
	}

	snapRevsByID := make(map[string]*asserts.SnapRevision)

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

	// remember

	s.model = modelAssertion
	s.snapDeclsByID = snapDeclsByID
	s.snapDeclsByName = snapDeclsByName
	s.snapRevsByID = snapRevsByID

	return nil
}

func readAsserts(batch *Batch, fn string) ([]*asserts.Ref, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return batch.AddStream(f)
}
