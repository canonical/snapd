// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

// Package snapasserts offers helpers to handle snap related assertions and their checking for installation.
package snapasserts

import (
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

type Finder interface {
	// Find an assertion based on arbitrary headers.  Provided
	// headers must contain the primary key for the assertion
	// type.  It returns a asserts.NotFoundError if the assertion
	// cannot be found.
	Find(assertionType *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error)
	// FindMany finds assertions based on arbitrary headers.
	// It returns a NotFoundError if no assertion can be found.
	FindMany(assertionType *asserts.AssertionType, headers map[string]string) ([]asserts.Assertion, error)
}

func findSnapDeclaration(snapID, name string, db Finder) (*asserts.SnapDeclaration, error) {
	a := mylog.Check2(db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": snapID,
	}))

	snapDecl := a.(*asserts.SnapDeclaration)

	if snapDecl.SnapName() == "" {
		return nil, fmt.Errorf("cannot install snap %q with a revoked snap declaration", name)
	}

	return snapDecl, nil
}

// CrossCheck tries to cross check the instance name, hash digest, provenance
// and size of a snap plus its metadata in a SideInfo with the relevant
// snap assertions in a database that should have been populated with
// them.
// The optional model assertion must be passed to have full cross
// checks in the case of delegated authority snap-revisions before
// installing a snap.
// It returns the corresponding cross-checked snap-revision.
// Ultimately the provided provenance (if not default) must be checked
// with the provenance in the snap metadata by the caller as well, if
// the provided provenance was not read safely from there already.
func CrossCheck(instanceName, snapSHA3_384, provenance string, snapSize uint64, si *snap.SideInfo, model *asserts.Model, db Finder) (snapRev *asserts.SnapRevision, err error) {
	// get relevant assertions and do cross checks
	headers := map[string]string{
		"snap-sha3-384": snapSHA3_384,
	}
	if provenance != "" {
		headers["provenance"] = provenance
	}
	a := mylog.Check2(db.Find(asserts.SnapRevisionType, headers))

	snapRev = a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return nil, fmt.Errorf("snap %q file does not have expected size according to signatures (download is broken or tampered): %d != %d", instanceName, snapSize, snapRev.SnapSize())
	}

	snapID := si.SnapID

	if snapRev.SnapID() != snapID || snapRev.SnapRevision() != si.Revision.N {
		return nil, fmt.Errorf("snap %q does not have expected ID or revision according to assertions (metadata is broken or tampered): %s / %s != %d / %s", instanceName, si.Revision, snapID, snapRev.SnapRevision(), snapRev.SnapID())
	}

	snapDecl := mylog.Check2(findSnapDeclaration(snapID, instanceName, db))

	if snapDecl.SnapName() != snap.InstanceSnap(instanceName) {
		return nil, fmt.Errorf("cannot install %q, snap %q is undergoing a rename to %q", instanceName, snap.InstanceSnap(instanceName), snapDecl.SnapName())
	}
	mylog.Check2(CrossCheckProvenance(instanceName, snapRev, snapDecl, model, db))

	return snapRev, nil
}

// CrossCheckProvenance tries to cross check the given snap-revision
// if it has a non default provenance with the revision-authority
// constraints of the given snap-declaration including any device
// scope constraints using model (and implied store).
// It also returns the provenance if it is different from the default.
// Ultimately if not default the provenance must also be checked
// with the provenance in the snap metadata by the caller.
func CrossCheckProvenance(instanceName string, snapRev *asserts.SnapRevision, snapDecl *asserts.SnapDeclaration, model *asserts.Model, db Finder) (signedProvenance string, err error) {
	if snapRev.Provenance() == "global-upload" {
		// nothing to check
		return "", nil
	}
	var store *asserts.Store
	if model != nil && model.Store() != "" {
		a := mylog.Check2(db.Find(asserts.StoreType, map[string]string{
			"store": model.Store(),
		}))
		if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
			return "", err
		}
		if a != nil {
			store = a.(*asserts.Store)
		}
	}
	ras := snapDecl.RevisionAuthority(snapRev.Provenance())
	matchingRevAuthority := false
	for _, ra := range ras {
		if mylog.Check(ra.Check(snapRev, model, store)); err == nil {
			matchingRevAuthority = true
			break
		}
	}
	if !matchingRevAuthority {
		return "", fmt.Errorf("snap %q revision assertion with provenance %q is not signed by an authority authorized on this device: %s", instanceName, snapRev.Provenance(), snapRev.AuthorityID())
	}
	return snapRev.Provenance(), nil
}

// CheckProvenanceWithVerifiedRevision checks that the given snap has
// the same provenance as of the provided snap-revision.
// It is intended to be called safely on snaps for which a matching
// and authorized snap-revision has been already found and cross-checked.
// Its purpose is to check that a blob has not been re-signed under an
// inappropriate provenance.
func CheckProvenanceWithVerifiedRevision(snapPath string, verifiedRev *asserts.SnapRevision) error {
	snapf := mylog.Check2(snapfile.Open(snapPath))

	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, nil))

	if verifiedRev.Provenance() != info.Provenance() {
		return fmt.Errorf("snap %q has been signed under provenance %q different from the metadata one: %q", snapPath, verifiedRev.Provenance(), info.Provenance())
	}
	return nil
}

// DeriveSideInfo tries to construct a SideInfo for the given snap
// using its digest to find the relevant snap assertions with the
// information in the given database. It will fail with an
// asserts.NotFoundError if it cannot find them.
// model is used to cross check that the found snap-revision is applicable
// on the device.
func DeriveSideInfo(snapPath string, model *asserts.Model, db Finder) (*snap.SideInfo, error) {
	snapSHA3_384, snapSize := mylog.Check3(asserts.SnapFileSHA3_384(snapPath))

	return DeriveSideInfoFromDigestAndSize(snapPath, snapSHA3_384, snapSize, model, db)
}

// DeriveSideInfoFromDigestAndSize tries to construct a SideInfo
// using digest and size as provided for the snap to find the relevant
// snap assertions with the information in the given database. It will
// fail with an asserts.NotFoundError if it cannot find them.
// model is used to cross check that the found snap-revision is applicable
// on the device.
func DeriveSideInfoFromDigestAndSize(snapPath string, snapSHA3_384 string, snapSize uint64, model *asserts.Model, db Finder) (*snap.SideInfo, error) {
	// get relevant assertions and reconstruct metadata
	headers := map[string]string{
		"snap-sha3-384": snapSHA3_384,
	}
	a := mylog.Check2(db.Find(asserts.SnapRevisionType, headers))
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return nil, err
	}
	if a == nil {
		// non-default provenance?
		cands := mylog.Check2(db.FindMany(asserts.SnapRevisionType, headers))

		if len(cands) != 1 {
			return nil, fmt.Errorf("safely handling snaps with different provenance but same hash not yet supported")
		}
		a = cands[0]
	}

	snapRev := a.(*asserts.SnapRevision)

	if snapRev.SnapSize() != snapSize {
		return nil, fmt.Errorf("snap %q does not have expected size according to signatures (broken or tampered): %d != %d", snapPath, snapSize, snapRev.SnapSize())
	}

	snapID := snapRev.SnapID()

	snapDecl := mylog.Check2(findSnapDeclaration(snapID, snapPath, db))
	mylog.Check2(CrossCheckProvenance(snapDecl.SnapName(), snapRev, snapDecl, model, db))
	mylog.Check(CheckProvenanceWithVerifiedRevision(snapPath, snapRev))

	return SideInfoFromSnapAssertions(snapDecl, snapRev), nil
}

// SideInfoFromSnapAssertions returns a *snap.SideInfo reflecting the given snap assertions.
func SideInfoFromSnapAssertions(snapDecl *asserts.SnapDeclaration, snapRev *asserts.SnapRevision) *snap.SideInfo {
	return &snap.SideInfo{
		RealName: snapDecl.SnapName(),
		SnapID:   snapDecl.SnapID(),
		Revision: snap.R(snapRev.SnapRevision()),
	}
}

// FetchSnapAssertions fetches the assertions matching the snap file digest and optional provenance using the given fetcher.
func FetchSnapAssertions(f asserts.Fetcher, snapSHA3_384, provenance string) error {
	// for now starting from the snap-revision will get us all other relevant assertions
	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{snapSHA3_384},
	}
	if provenance != "" {
		ref.PrimaryKey = append(ref.PrimaryKey, provenance)
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

// FetchStore fetches the store assertion and its prerequisites for the given store id using the given fetcher.
func FetchStore(f asserts.Fetcher, storeID string) error {
	ref := &asserts.Ref{
		Type:       asserts.StoreType,
		PrimaryKey: []string{storeID},
	}

	return f.Fetch(ref)
}
