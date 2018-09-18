// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package snapasserts offers helpers to handle snap assertions and their checking for installation.
package snapasserts

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

type Finder interface {
	// Find an assertion based on arbitrary headers.  Provided
	// headers must contain the primary key for the assertion
	// type.  It returns a asserts.NotFoundError if the assertion
	// cannot be found.
	Find(assertionType *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error)
}

func findSnapDeclaration(snapID, name string, db Finder) (*asserts.SnapDeclaration, error) {
	a, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	})
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find snap declaration for %q: %s", name, snapID)
	}
	snapDecl := a.(*asserts.SnapDeclaration)

	if snapDecl.SnapName() == "" {
		return nil, fmt.Errorf("cannot install snap %q with a revoked snap declaration", name)
	}

	return snapDecl, nil
}

// CrossCheck tries to cross check the instance name, hash digest and size of a snap plus its metadata in a SideInfo with the relevant snap assertions in a database that should have been populated with them.
func CrossCheck(instanceName, snapSHA3_384 string, snapSize uint64, si *snap.SideInfo, db Finder) error {
	// get relevant assertions and do cross checks
	a, err := db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": snapSHA3_384,
	})
	if err != nil {
		return fmt.Errorf("internal error: cannot find pre-populated snap-revision assertion for %q: %s", instanceName, snapSHA3_384)
	}
	snapRev := a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return fmt.Errorf("snap %q file does not have expected size according to signatures (download is broken or tampered): %d != %d", instanceName, snapSize, snapRev.SnapSize())
	}

	snapID := si.SnapID

	if snapRev.SnapID() != snapID || snapRev.SnapRevision() != si.Revision.N {
		return fmt.Errorf("snap %q does not have expected ID or revision according to assertions (metadata is broken or tampered): %s / %s != %d / %s", instanceName, si.Revision, snapID, snapRev.SnapRevision(), snapRev.SnapID())
	}

	snapDecl, err := findSnapDeclaration(snapID, instanceName, db)
	if err != nil {
		return err
	}

	if snapDecl.SnapName() != snap.InstanceSnap(instanceName) {
		return fmt.Errorf("cannot install %q, snap %q is undergoing a rename to %q", instanceName, snap.InstanceSnap(instanceName), snapDecl.SnapName())
	}

	return nil
}

// DeriveSideInfo tries to construct a SideInfo for the given snap using its digest to find the relevant snap assertions with the information in the given database. It will fail with an asserts.NotFoundError if it cannot find them.
func DeriveSideInfo(snapPath string, db Finder) (*snap.SideInfo, error) {
	snapSHA3_384, snapSize, err := asserts.SnapFileSHA3_384(snapPath)
	if err != nil {
		return nil, err
	}

	// get relevant assertions and reconstruct metadata
	a, err := db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": snapSHA3_384,
	})
	if err != nil {
		return nil, err
	}

	snapRev := a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return nil, fmt.Errorf("snap %q does not have expected size according to signatures (broken or tampered): %d != %d", snapPath, snapSize, snapRev.SnapSize())
	}

	snapID := snapRev.SnapID()

	snapDecl, err := findSnapDeclaration(snapID, snapPath, db)
	if err != nil {
		return nil, err
	}

	name := snapDecl.SnapName()

	return &snap.SideInfo{
		RealName: name,
		SnapID:   snapID,
		Revision: snap.R(snapRev.SnapRevision()),
	}, nil
}

// FetchSnapAssertions fetches the assertions matching the snap file digest using the given fetcher.
func FetchSnapAssertions(f asserts.Fetcher, snapSHA3_384 string) error {
	// for now starting from the snap-revision will get us all other relevant assertions
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{snapSHA3_384},
	}

	return f.Fetch(ref)
}

// FetchSnapDeclaration fetches the snap declaration and its prerequisites for the given snap id using the given fetcher.
func FetchSnapDeclaration(f asserts.Fetcher, snapID string) error {
	ref := &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{release.Series, snapID},
	}

	return f.Fetch(ref)
}
