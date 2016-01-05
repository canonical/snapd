// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

// SnapBuild holds a snap-build assertion, asserting the
// properties of a built snap by the builder.
type SnapBuild struct {
	assertionBase
	size      uint64
	timestamp time.Time
}

// SnapID returns the snap id of the built snap.
func (snapdcl *SnapBuild) SnapID() string {
	return snapdcl.Header("snap-id")
}

// SnapDigest returns the digest of the built snap.
func (snapdcl *SnapBuild) SnapDigest() string {
	return snapdcl.Header("snap-digest")
}

// SnapSize returns the size of the built snap.
func (snapdcl *SnapBuild) SnapSize() uint64 {
	return snapdcl.size
}

// Grade returns the grade of the built snap: devel|stable
func (snapdcl *SnapBuild) Grade() string {
	return snapdcl.Header("grade")
}

// Timestamp returns the snap-build assertion timestamp.
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

func buildSnapBuild(assert assertionBase) (Assertion, error) {
	_, err := checkMandatory(assert.headers, "snap-id")
	if err != nil {
		return nil, err
	}

	// TODO: more parsing/checking of this here?
	_, err = checkMandatory(assert.headers, "snap-digest")
	if err != nil {
		return nil, err
	}

	_, err = checkMandatory(assert.headers, "grade")
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

func init() {
	typeRegistry[SnapBuildType] = &assertionTypeRegistration{
		builder:    buildSnapBuild,
		primaryKey: []string{"snap-id", "snap-digest"},
	}
}
