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
	"fmt"
	"time"
)

// SnapBuild holds a snap-build assertion, asserting the properties of a snap
// at the time it was built by the developer.
type SnapBuild struct {
	assertionBase
	size      uint64
	timestamp time.Time
}

// SnapID returns the snap id of the snap.
func (snapdcl *SnapBuild) SnapID() string {
	return snapdcl.Header("snap-id")
}

// SnapDigest returns the digest of the snap. The digest is prefixed with the
// algorithm used to generate it.
func (snapdcl *SnapBuild) SnapDigest() string {
	return snapdcl.Header("snap-digest")
}

// SnapSize returns the size of the snap.
func (snapdcl *SnapBuild) SnapSize() uint64 {
	return snapdcl.size
}

// Grade returns the grade of the snap: devel|stable
func (snapdcl *SnapBuild) Grade() string {
	return snapdcl.Header("grade")
}

// Timestamp returns the time when the snap-build assertion was created.
func (snapdcl *SnapBuild) Timestamp() time.Time {
	return snapdcl.timestamp
}

// implement further consistency checks
func (snapdcl *SnapBuild) checkConsistency(db *Database, acck *AccountKey) error {
	if !acck.isKeyValidAt(snapdcl.timestamp) {
		return fmt.Errorf("snap-build timestamp outside of signing key validity")
	}
	return nil
}

func assembleSnapBuild(assert assertionBase) (Assertion, error) {
	// TODO: more parsing/checking of snap-digest

	_, err := checkMandatory(assert.headers, "grade")
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
// store acknowledging the receipt of a snap build and labeling it with a snap
// revision.
type SnapRevision struct {
	assertionBase
	snapRevision uint64
	timestamp    time.Time
}

// SnapID returns the snap id of the snap.
func (assert *SnapRevision) SnapID() string {
	return assert.Header("snap-id")
}

// SnapDigest returns the digest of the snap submitted to the store. The digest
// is prefixed with the algorithm used to generate it.
func (assert *SnapRevision) SnapDigest() string {
	return assert.Header("snap-digest")
}

// SnapRevision returns the revision of the snap-id assigned to this build.
func (assert *SnapRevision) SnapRevision() uint64 {
	return assert.snapRevision
}

// SnapBuild returns the digest of the associated snap-build.
func (assert *SnapRevision) SnapBuild() string {
	return assert.Header("snap-build")
}

// DeveloperID returns the id of the developer that submitted the snap build to
// the store.
func (assert *SnapRevision) DeveloperID() string {
	return assert.Header("developer-id")
}

// Timestamp returns the time when the snap-revision was issued.
func (assert *SnapRevision) Timestamp() time.Time {
	return assert.timestamp
}

// Implement further consistency checks.
func (assert *SnapRevision) checkConsistency(db *Database, acck *AccountKey) error {
	// TODO: check the associated snap-build exists.
	// TODO: check the associated snap-build's digest.
	// TODO: check developer-id matches snap-build's authority-id.
	if !acck.isKeyValidAt(assert.timestamp) {
		return fmt.Errorf("snap-revision timestamp outside of signing key validity")
	}
	return nil
}

func assembleSnapRevision(assert assertionBase) (Assertion, error) {
	// TODO: more parsing/checking of snap-digest

	snapRevision, err := checkUint(assert.headers, "snap-revision", 64)
	if err != nil {
		return nil, err
	}

	_, err = checkMandatory(assert.headers, "snap-build")
	if err != nil {
		return nil, err
	}

	_, err = checkMandatory(assert.headers, "developer-id")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &SnapRevision{
		assertionBase: assert,
		snapRevision:  snapRevision,
		timestamp:     timestamp,
	}, nil
}
