// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package asserts

import (
	"crypto"
	"errors"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/naming"
)

// SnapResourceRevision holds a snap-resource-revision assertion, which is a
// statement by the store acknowledging the receipt of data for a resource of a
// snap and labeling it with a resource revision.
type SnapResourceRevision struct {
	assertionBase
	resourceSize     uint64
	resourceRevision int
	timestamp        time.Time

	// TODO: integrity when the format is stabilized again
}

// ResourceSHA3_384 returns the SHA3-384 digest of the snap resource.
func (resrev *SnapResourceRevision) ResourceSHA3_384() string {
	return resrev.HeaderString("resource-sha3-384")
}

// Provenance returns the optional provenance of the snap (defaults to
// global-upload (naming.DefaultProvenance)).
func (resrev *SnapResourceRevision) Provenance() string {
	return resrev.HeaderString("provenance")
}

// SnapID returns the snap id of the snap for the resource.
func (resrev *SnapResourceRevision) SnapID() string {
	return resrev.HeaderString("snap-id")
}

// ResourceName returns the name of the snap resource.
func (resrev *SnapResourceRevision) ResourceName() string {
	return resrev.HeaderString("resource-name")
}

// ResourceSize returns the size in bytes of the snap resource submitted to the store.
func (resrev *SnapResourceRevision) ResourceSize() uint64 {
	return resrev.resourceSize
}

// ResourceRevision returns the revision assigned to this upload of the snap resource.
func (resrev *SnapResourceRevision) ResourceRevision() int {
	return resrev.resourceRevision
}

// DeveloperID returns the id of the developer that submitted the snap resource.
func (resrev *SnapResourceRevision) DeveloperID() string {
	return resrev.HeaderString("developer-id")
}

// Timestamp returns the time when the snap-resource-revision was issued.
func (resrev *SnapResourceRevision) Timestamp() time.Time {
	return resrev.timestamp
}

// Implement further consistency checks.
func (resrev *SnapResourceRevision) checkConsistency(db RODatabase, acck *AccountKey) error {
	otherProvenance := resrev.Provenance() != naming.DefaultProvenance
	if !otherProvenance && !db.IsTrustedAccount(resrev.AuthorityID()) {
		// delegating global-upload revisions is not allowed
		return fmt.Errorf("snap-resource-revision assertion for snap id %q is not signed by a store: %s", resrev.SnapID(), resrev.AuthorityID())
	}
	_ := mylog.Check2(db.Find(AccountType, map[string]string{
		"account-id": resrev.DeveloperID(),
	}))
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("snap-resource-revision assertion for snap id %q does not have a matching account assertion for the developer %q", resrev.SnapID(), resrev.DeveloperID())
	}

	a := mylog.Check2(db.Find(SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": resrev.SnapID(),
	}))
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("snap-resource-revision assertion for snap id %q does not have a matching snap-declaration assertion", resrev.SnapID())
	}

	if otherProvenance {
		decl := a.(*SnapDeclaration)
		ras := decl.RevisionAuthority(resrev.Provenance())
		matchingRevAuthority := false
		for _, ra := range ras {
			// model==store==nil, we do not perform device-specific
			// checks at this level, those are performed at
			// higher-level guarding installing actual components
			if mylog.Check(ra.CheckResourceRevision(resrev, nil, nil)); err == nil {
				matchingRevAuthority = true
				break
			}
		}
		if !matchingRevAuthority {
			return fmt.Errorf("snap-resource-revision assertion with provenance %q for snap id %q is not signed by an authorized authority: %s", resrev.Provenance(), resrev.SnapID(), resrev.AuthorityID())
		}
	}
	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*SnapResourceRevision)(nil)

// Prerequisites returns references to this snap-resource-revision's prerequisite assertions.
func (resrev *SnapResourceRevision) Prerequisites() []*Ref {
	return []*Ref{
		{Type: SnapDeclarationType, PrimaryKey: []string{release.Series, resrev.SnapID()}},
		{Type: AccountType, PrimaryKey: []string{resrev.DeveloperID()}},
	}
}

func checkResourceName(headers map[string]interface{}) error {
	resName := mylog.Check2(checkNotEmptyString(headers, "resource-name"))
	mylog.Check(

		// same format as snap names
		naming.ValidateSnap(resName))

	return nil
}

func assembleSnapResourceRevision(assert assertionBase) (Assertion, error) {
	mylog.Check(checkResourceName(assert.headers))

	_ := mylog.Check2(checkDigest(assert.headers, "resource-sha3-384", crypto.SHA3_384))

	_ = mylog.Check2(checkStringMatches(assert.headers, "provenance", naming.ValidProvenance))

	resourceSize := mylog.Check2(checkUint(assert.headers, "resource-size", 64))

	resourceRevision := mylog.Check2(checkSnapRevisionWhat(assert.headers, "resource-revision", "header"))

	_ = mylog.Check2(checkNotEmptyString(assert.headers, "developer-id"))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	// TODO: implement integrity stanza when format is stabilized again

	return &SnapResourceRevision{
		assertionBase:    assert,
		resourceSize:     resourceSize,
		resourceRevision: resourceRevision,
		timestamp:        timestamp,
	}, nil
}

// SnapResourcePair holds a snap-resource-pair assertion, which is a
// statement by the store acknowledging that it received indication
// that the given snap resource revision can work with the given
// snap revision.
type SnapResourcePair struct {
	assertionBase
	resourceRevision int
	snapRevision     int
	timestamp        time.Time
}

// SnapID returns the snap id of the snap for the resource.
func (respair *SnapResourcePair) SnapID() string {
	return respair.HeaderString("snap-id")
}

// ResourceName returns the name of the snap resource.
func (respair *SnapResourcePair) ResourceName() string {
	return respair.HeaderString("resource-name")
}

// ResourceRevision returns the snap resource revision being paired.
func (respair *SnapResourcePair) ResourceRevision() int {
	return respair.resourceRevision
}

// SnapRevision returns the snap revision being paired with.
func (respair *SnapResourcePair) SnapRevision() int {
	return respair.snapRevision
}

// Provenance returns the optional provenance of the snap (defaults to
// global-upload (naming.DefaultProvenance)).
func (respair *SnapResourcePair) Provenance() string {
	return respair.HeaderString("provenance")
}

// DeveloperID returns the id of the developer that submitted the snap resource for the snap revision.
func (respair *SnapResourcePair) DeveloperID() string {
	return respair.HeaderString("developer-id")
}

// Timestamp returns the time when the snap-resource-pair was issued.
func (respair *SnapResourcePair) Timestamp() time.Time {
	return respair.timestamp
}

// Implement further consistency checks.
func (respair *SnapResourcePair) checkConsistency(db RODatabase, acck *AccountKey) error {
	otherProvenance := respair.Provenance() != naming.DefaultProvenance
	if !otherProvenance && !db.IsTrustedAccount(respair.AuthorityID()) {
		// delegating global-upload revisions is not allowed
		return fmt.Errorf("snap-resource-pair assertion for snap id %q is not signed by a store: %s", respair.SnapID(), respair.AuthorityID())
	}
	_ := mylog.Check2(db.Find(AccountType, map[string]string{
		"account-id": respair.DeveloperID(),
	}))
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("snap-resource-pair assertion for snap id %q does not have a matching account assertion for the developer %q", respair.SnapID(), respair.DeveloperID())
	}

	a := mylog.Check2(db.Find(SnapDeclarationType, map[string]string{
		"series":  release.Series,
		"snap-id": respair.SnapID(),
	}))
	if errors.Is(err, &NotFoundError{}) {
		return fmt.Errorf("snap-resource-pair assertion for snap id %q does not have a matching snap-declaration assertion", respair.SnapID())
	}

	if otherProvenance {
		decl := a.(*SnapDeclaration)
		ras := decl.RevisionAuthority(respair.Provenance())
		// check that there's matching delegation using the snap-revision
		matchingRevAuthority := false
		for _, ra := range ras {
			// model==store==nil, we do not perform device-specific
			// checks at this level, those are performed at
			// higher-level guarding installing actual components
			if mylog.Check(ra.checkProvenanceAndRevision(respair, "snap", respair.SnapRevision(), nil, nil)); err == nil {
				matchingRevAuthority = true
				break
			}
		}
		if !matchingRevAuthority {
			return fmt.Errorf("snap-resource-pair assertion with provenance %q for snap id %q is not signed by an authorized authority: %s", respair.Provenance(), respair.SnapID(), respair.AuthorityID())
		}
	}
	return nil
}

// expected interface is implemented
var _ consistencyChecker = (*SnapResourcePair)(nil)

// Prerequisites returns references to this snap-resource-pair's prerequisite assertions.
func (respair *SnapResourcePair) Prerequisites() []*Ref {
	return []*Ref{
		{Type: SnapDeclarationType, PrimaryKey: []string{release.Series, respair.SnapID()}},
	}
}

func assembleSnapResourcePair(assert assertionBase) (Assertion, error) {
	mylog.Check(checkResourceName(assert.headers))

	_ := mylog.Check2(checkStringMatches(assert.headers, "provenance", naming.ValidProvenance))

	resourceRevision := mylog.Check2(checkSnapRevisionWhat(assert.headers, "resource-revision", "header"))

	snapRevision := mylog.Check2(checkSnapRevisionWhat(assert.headers, "snap-revision", "header"))

	_ = mylog.Check2(checkNotEmptyString(assert.headers, "developer-id"))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	return &SnapResourcePair{
		assertionBase:    assert,
		resourceRevision: resourceRevision,
		snapRevision:     snapRevision,
		timestamp:        timestamp,
	}, nil
}
