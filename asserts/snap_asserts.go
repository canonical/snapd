// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"fmt"
	"time"

	_ "golang.org/x/crypto/sha3" // expected for digests

	"github.com/snapcore/snapd/release"
)

// TODO: adjust to new designs!

// SnapDeclaration holds a snap-declaration assertion, declaring a
// snap binding its identifying snap-id to a name, asserting its
// publisher and its other properties.
type SnapDeclaration struct {
	assertionBase
	timestamp time.Time
}

// Series returns the series for which the snap is being declared.
func (snapdcl *SnapDeclaration) Series() string {
	return snapdcl.HeaderString("series")
}

// SnapID returns the snap id of the declared snap.
func (snapdcl *SnapDeclaration) SnapID() string {
	return snapdcl.HeaderString("snap-id")
}

// SnapName returns the declared snap name.
func (snapdcl *SnapDeclaration) SnapName() string {
	return snapdcl.HeaderString("snap-name")
}

// PublisherID returns the identifier of the publisher of the declared snap.
func (snapdcl *SnapDeclaration) PublisherID() string {
	return snapdcl.HeaderString("publisher-id")
}

// Timestamp returns the time when the snap-declaration was issued.
func (snapdcl *SnapDeclaration) Timestamp() time.Time {
	return snapdcl.timestamp
}

// Implement further consistency checks.
func (snapdcl *SnapDeclaration) checkConsistency(db RODatabase, acck *AccountKey) error {
	if !db.IsTrustedAccount(snapdcl.AuthorityID()) {
		return fmt.Errorf("snap-declaration assertion for %q (id %q) is not signed by a directly trusted authority: %s", snapdcl.SnapName(), snapdcl.SnapID(), snapdcl.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": snapdcl.PublisherID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-declaration assertion for %q (id %q) does not have a matching account assertion for the publisher %q", snapdcl.SnapName(), snapdcl.SnapID(), snapdcl.PublisherID())
	}
	if err != nil {
		return err
	}
	return nil
}

// sanity
var _ consistencyChecker = (*SnapDeclaration)(nil)

// Prerequisites returns references to this snap-declaration's prerequisite assertions.
func (snapdcl *SnapDeclaration) Prerequisites() []*Ref {
	return []*Ref{
		&Ref{Type: AccountType, PrimaryKey: []string{snapdcl.PublisherID()}},
	}
}

func assembleSnapDeclaration(assert assertionBase) (Assertion, error) {
	_, err := checkExistsString(assert.headers, "snap-name")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "publisher-id")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &SnapDeclaration{
		assertionBase: assert,
		timestamp:     timestamp,
	}, nil
}

// SnapBuild holds a snap-build assertion, asserting the properties of a snap
// at the time it was built by the developer.
type SnapBuild struct {
	assertionBase
	size      uint64
	timestamp time.Time
}

// SnapSHA3_384 returns the SHA3-384 digest of the snap.
func (snapbld *SnapBuild) SnapSHA3_384() string {
	return snapbld.HeaderString("snap-sha3-384")
}

// SnapID returns the snap id of the snap.
func (snapbld *SnapBuild) SnapID() string {
	return snapbld.HeaderString("snap-id")
}

// SnapSize returns the size of the snap.
func (snapbld *SnapBuild) SnapSize() uint64 {
	return snapbld.size
}

// Grade returns the grade of the snap: devel|stable
func (snapbld *SnapBuild) Grade() string {
	return snapbld.HeaderString("grade")
}

// Timestamp returns the time when the snap-build assertion was created.
func (snapbld *SnapBuild) Timestamp() time.Time {
	return snapbld.timestamp
}

func assembleSnapBuild(assert assertionBase) (Assertion, error) {
	_, err := checkDigest(assert.headers, "snap-sha3-384", crypto.SHA3_384)
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "snap-id")
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "grade")
	if err != nil {
		return nil, err
	}

	size, err := checkUint(assert.headers, "snap-size", 64)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}
	// ignore extra headers and non-empty body for future compatibility
	return &SnapBuild{
		assertionBase: assert,
		size:          size,
		timestamp:     timestamp,
	}, nil
}

// SnapRevision holds a snap-revision assertion, which is a statement by the
// store acknowledging the receipt of a build of a snap and labeling it with a
// snap revision.
type SnapRevision struct {
	assertionBase
	snapSize     uint64
	snapRevision int
	timestamp    time.Time
}

// SnapSHA3_384 returns the SHA3-384 digest of the snap.
func (snaprev *SnapRevision) SnapSHA3_384() string {
	return snaprev.HeaderString("snap-sha3-384")
}

// SnapID returns the snap id of the snap.
func (snaprev *SnapRevision) SnapID() string {
	return snaprev.HeaderString("snap-id")
}

// SnapSize returns the size in bytes of the snap submitted to the store.
func (snaprev *SnapRevision) SnapSize() uint64 {
	return snaprev.snapSize
}

// SnapRevision returns the revision assigned to this build of the snap.
func (snaprev *SnapRevision) SnapRevision() int {
	return snaprev.snapRevision
}

// DeveloperID returns the id of the developer that submitted this build of the
// snap.
func (snaprev *SnapRevision) DeveloperID() string {
	return snaprev.HeaderString("developer-id")
}

// Timestamp returns the time when the snap-revision was issued.
func (snaprev *SnapRevision) Timestamp() time.Time {
	return snaprev.timestamp
}

// Implement further consistency checks.
func (snaprev *SnapRevision) checkConsistency(db RODatabase, acck *AccountKey) error {
	// TODO: expand this to consider other stores signing on their own
	if !db.IsTrustedAccount(snaprev.AuthorityID()) {
		return fmt.Errorf("snap-revision assertion for snap id %q is not signed by a store: %s", snaprev.SnapID(), snaprev.AuthorityID())
	}
	_, err := db.Find(AccountType, map[string]string{
		"account-id": snaprev.DeveloperID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-revision assertion for snap id %q does not have a matching account assertion for the developer %q", snaprev.SnapID(), snaprev.DeveloperID())
	}
	if err != nil {
		return err
	}
	_, err = db.Find(SnapDeclarationType, map[string]string{
		// XXX: mediate getting current series through some context object? this gets the job done for now
		"series":  release.Series,
		"snap-id": snaprev.SnapID(),
	})
	if err == ErrNotFound {
		return fmt.Errorf("snap-revision assertion for snap id %q does not have a matching snap-declaration assertion", snaprev.SnapID())
	}
	if err != nil {
		return err
	}
	return nil
}

// sanity
var _ consistencyChecker = (*SnapRevision)(nil)

// Prerequisites returns references to this snap-revision's prerequisite assertions.
func (snaprev *SnapRevision) Prerequisites() []*Ref {
	return []*Ref{
		// XXX: mediate getting current series through some context object? this gets the job done for now
		&Ref{Type: SnapDeclarationType, PrimaryKey: []string{release.Series, snaprev.SnapID()}},
		&Ref{Type: AccountType, PrimaryKey: []string{snaprev.DeveloperID()}},
	}
}

func assembleSnapRevision(assert assertionBase) (Assertion, error) {
	_, err := checkDigest(assert.headers, "snap-sha3-384", crypto.SHA3_384)
	if err != nil {
		return nil, err
	}

	_, err = checkNotEmptyString(assert.headers, "snap-id")
	if err != nil {
		return nil, err
	}

	snapSize, err := checkUint(assert.headers, "snap-size", 64)
	if err != nil {
		return nil, err
	}

	snapRevision, err := checkInt(assert.headers, "snap-revision")
	if err != nil {
		return nil, err
	}
	if snapRevision < 1 {
		return nil, fmt.Errorf(`"snap-revision" header must be >=1: %d`, snapRevision)
	}

	_, err = checkNotEmptyString(assert.headers, "developer-id")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &SnapRevision{
		assertionBase: assert,
		snapSize:      snapSize,
		snapRevision:  snapRevision,
		timestamp:     timestamp,
	}, nil
}
