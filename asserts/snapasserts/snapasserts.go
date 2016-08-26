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

// Package snapasserts offers helpers to handle snap assertions and their checking.
package snapasserts

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// CrossCheck tries to cross check the name, hash digest and size of a snap plus its metadata in a SideInfo with the relevant snap assertions in a database that should have been populated with them.
func CrossCheck(name, snapSHA3_384 string, snapSize uint64, si *snap.SideInfo, db asserts.RODatabase) error {
	// get relevant assertions and do cross checks
	a, err := db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": snapSHA3_384,
	})
	if err != nil {
		return fmt.Errorf("internal error: cannot find pre-populated snap-revision assertion for %q: %s", name, snapSHA3_384)
	}
	snapRev := a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return fmt.Errorf("snap %q file does not have expected size according to signatures (download is broken or tampered): %d != %d", name, snapSize, snapRev.SnapSize())
	}

	snapID := si.SnapID

	if snapRev.SnapID() != snapID || snapRev.SnapRevision() != si.Revision.N {
		// we have at least 2 cases here, what's the best message?
		// - an unsuccesufl MITM
		// - broken store metadata resulting into broken assertions
		//   (more likely if it is snap-revision not matching)
		//   people would need to report this
		return fmt.Errorf("snap %q does not have expected ID or revision according to assertions (metadata is broken or tampered): %s / %s != %d / %s", name, si.Revision, snapID, snapRev.SnapRevision(), snapRev.SnapID())
	}

	a, err = db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	})
	if err != nil {
		return fmt.Errorf("internal error: cannot find pre-populated snap declaration for %q: %s", name, snapID)
	}
	snapDecl := a.(*asserts.SnapDeclaration)

	if snapDecl.SnapName() == "" {
		// TODO: trigger a global sanity check
		// that will generate the changes to deal with this
		return fmt.Errorf("cannot install snap %q with a revoked snap declaration", name)
	}

	if snapDecl.SnapName() != name {
		// TODO: trigger a global sanity check
		// that will generate the changes to deal with this
		return fmt.Errorf("cannot install snap %q that is undergoing a rename to %q", name, snapDecl.SnapName())
	}

	return nil
}
