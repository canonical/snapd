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
	"time"
)

// SnapDeclaration holds a snap-declaration assertion, declaring a
// snap binding its identifying snap-id to a name, asserting its
// publisher and its other properties.
type SnapDeclaration struct {
	assertionBase
	gates     []string
	timestamp time.Time
}

// Series returns the series for which the snap is being declared.
func (snapdcl *SnapDeclaration) Series() string {
	return snapdcl.Header("series")
}

// SnapID returns the snap id of the declared snap.
func (snapdcl *SnapDeclaration) SnapID() string {
	return snapdcl.Header("snap-id")
}

// SnapName returns the declared snap name.
func (snapdcl *SnapDeclaration) SnapName() string {
	return snapdcl.Header("snap-name")
}

// PublisherID returns the identifier of the publisher of the declared snap.
func (snapdcl *SnapDeclaration) PublisherID() string {
	return snapdcl.Header("publisher-id")
}

// Gates returns the list of snap-ids gated by this snap.
func (snapdcl *SnapDeclaration) Gates() []string {
	return snapdcl.gates
}

// Timestamp returns the time when the snap-declaration was issued.
func (snapdcl *SnapDeclaration) Timestamp() time.Time {
	return snapdcl.timestamp
}

// XXX: consistency check is signed by canonical

func assembleSnapDeclaration(assert assertionBase) (Assertion, error) {
	_, err := checkMandatory(assert.headers, "snap-name")
	if err != nil {
		return nil, err
	}

	_, err = checkMandatory(assert.headers, "publisher-id")
	if err != nil {
		return nil, err
	}

	gates, err := checkCommaSepList(assert.headers, "gates")
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

	return &SnapDeclaration{
		assertionBase: assert,
		gates:         gates,
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

// Series returns the series for which the snap was built.
func (snapbld *SnapBuild) Series() string {
	return snapbld.Header("series")
}

// SnapID returns the snap id of the snap.
func (snapbld *SnapBuild) SnapID() string {
	return snapbld.Header("snap-id")
}

// SnapDigest returns the digest of the snap. The digest is prefixed with the
// algorithm used to generate it.
func (snapbld *SnapBuild) SnapDigest() string {
	return snapbld.Header("snap-digest")
}

// SnapSize returns the size of the snap.
func (snapbld *SnapBuild) SnapSize() uint64 {
	return snapbld.size
}

// Grade returns the grade of the snap: devel|stable
func (snapbld *SnapBuild) Grade() string {
	return snapbld.Header("grade")
}

// Timestamp returns the time when the snap-build assertion was created.
func (snapbld *SnapBuild) Timestamp() time.Time {
	return snapbld.timestamp
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
// store acknowledging the receipt of a build of a snap and labeling it with a
// snap revision.
type SnapRevision struct {
	assertionBase
	snapSize     uint64
	snapRevision uint64
	timestamp    time.Time
}

// Series returns the series of the snap submitted to and acknowledged by the
// store.
func (snaprev *SnapRevision) Series() string {
	return snaprev.Header("series")
}

// SnapID returns the snap id of the snap.
func (snaprev *SnapRevision) SnapID() string {
	return snaprev.Header("snap-id")
}

// SnapDigest returns the digest of the snap submitted to and acknowledged by
// the store. The digest is prefixed with the algorithm used to generate it.
func (snaprev *SnapRevision) SnapDigest() string {
	return snaprev.Header("snap-digest")
}

// SnapSize returns the size in bytes of the snap submitted to the store.
func (snaprev *SnapRevision) SnapSize() uint64 {
	return snaprev.snapSize
}

// SnapRevision returns the revision assigned to this build of the snap.
func (snaprev *SnapRevision) SnapRevision() uint64 {
	return snaprev.snapRevision
}

// DeveloperID returns the id of the developer that submitted this build of the
// snap.
func (snaprev *SnapRevision) DeveloperID() string {
	return snaprev.Header("developer-id")
}

// Timestamp returns the time when the snap-revision was issued.
func (snaprev *SnapRevision) Timestamp() time.Time {
	return snaprev.timestamp
}

// Implement further consistency checks.
func (snaprev *SnapRevision) checkConsistency(db RODatabase, acck *AccountKey) error {
	return nil
}

// sanity
var _ consistencyChecker = (*SnapRevision)(nil)

func assembleSnapRevision(assert assertionBase) (Assertion, error) {
	// TODO: more parsing/checking of snap-digest

	snapSize, err := checkUint(assert.headers, "snap-size", 64)
	if err != nil {
		return nil, err
	}

	snapRevision, err := checkUint(assert.headers, "snap-revision", 64)
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
		snapSize:      snapSize,
		snapRevision:  snapRevision,
		timestamp:     timestamp,
	}, nil
}
