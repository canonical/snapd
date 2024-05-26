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
	"fmt"
	"regexp"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap/naming"
)

// validSystemLabel is the regex describing a valid system label. Typically
// system labels are expected to be date based, eg. 20201116, but for
// completeness follow the same rule as model names (incl. one letter model
// names and thus system labels), with the exception that uppercase letters are
// not allowed, as the systems will often be stored in a FAT filesystem.
var validSystemLabel = regexp.MustCompile("^[a-z0-9](?:-?[a-z0-9])*$")

// IsValidSystemLabel checks whether the string is a valid UC20 seed system
// label.
func IsValidSystemLabel(label string) error {
	if !validSystemLabel.MatchString(label) {
		return fmt.Errorf("invalid seed system label: %q", label)
	}
	return nil
}

// PreseedSnap holds the details about a snap constrained by a preseed assertion.
type PreseedSnap struct {
	Name     string
	SnapID   string
	Revision int
}

// SnapName implements naming.SnapRef.
func (s *PreseedSnap) SnapName() string {
	return s.Name
}

// ID implements naming.SnapRef.
func (s *PreseedSnap) ID() string {
	return s.SnapID
}

// Preseed holds preseed assertion, which is a statement about system-label,
// model, set of snaps and preseed artifact used for preseeding of UC20 system.
type Preseed struct {
	assertionBase
	snaps     []*PreseedSnap
	timestamp time.Time
}

// Series returns the series that this assertion is valid for.
func (p *Preseed) Series() string {
	return p.HeaderString("series")
}

// BrandID returns the brand identifier.
func (p *Preseed) BrandID() string {
	return p.HeaderString("brand-id")
}

// Model returns the model name identifier.
func (p *Preseed) Model() string {
	return p.HeaderString("model")
}

// SystemLabel returns the label of the seeded system.
func (p *Preseed) SystemLabel() string {
	return p.HeaderString("system-label")
}

// Timestamp returns the time when the preseed assertion was issued.
func (p *Preseed) Timestamp() time.Time {
	return p.timestamp
}

// ArtifactSHA3_384 returns the checksum of preseeding artifact.
func (p *Preseed) ArtifactSHA3_384() string {
	return p.HeaderString("artifact-sha3-384")
}

// Snaps returns the snaps for preseeding.
func (p *Preseed) Snaps() []*PreseedSnap {
	return p.snaps
}

func checkPreseedSnap(snap map[string]interface{}) (*PreseedSnap, error) {
	name := mylog.Check2(checkNotEmptyStringWhat(snap, "name", "of snap"))
	mylog.Check(naming.ValidateSnap(name))

	what := fmt.Sprintf("of snap %q", name)

	// snap id can be omitted if the model allows for unasserted snaps
	var snapID string
	if _, ok := snap["id"]; ok {
		snapID = mylog.Check2(checkStringMatchesWhat(snap, "id", what, naming.ValidSnapID))
	}

	var snapRevision int
	if _, ok := snap["revision"]; ok {
		snapRevision = mylog.Check2(checkSnapRevisionWhat(snap, "revision", what))
	}

	if snapID != "" && snapRevision <= 0 {
		return nil, fmt.Errorf("snap revision is required when snap id is set")
	}
	if snapID == "" && snapRevision > 0 {
		return nil, fmt.Errorf("snap id is required when revision is set")
	}

	return &PreseedSnap{
		Name:     name,
		SnapID:   snapID,
		Revision: snapRevision,
	}, nil
}

func checkPreseedSnaps(snapList interface{}) ([]*PreseedSnap, error) {
	const wrongHeaderType = `"snaps" header must be a list of maps`

	entries, ok := snapList.([]interface{})
	if !ok {
		return nil, fmt.Errorf(wrongHeaderType)
	}

	seen := make(map[string]bool, len(entries))
	seenIDs := make(map[string]string, len(entries))
	snaps := make([]*PreseedSnap, 0, len(entries))
	for _, entry := range entries {
		snap, ok := entry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(wrongHeaderType)
		}
		preseedSnap := mylog.Check2(checkPreseedSnap(snap))

		if seen[preseedSnap.Name] {
			return nil, fmt.Errorf("cannot list the same snap %q multiple times", preseedSnap.Name)
		}
		seen[preseedSnap.Name] = true
		snapID := preseedSnap.SnapID
		if snapID != "" {
			if underName := seenIDs[snapID]; underName != "" {
				return nil, fmt.Errorf("cannot specify the same snap id %q multiple times, specified for snaps %q and %q", snapID, underName, preseedSnap.Name)
			}
			seenIDs[snapID] = preseedSnap.Name
		}
		snaps = append(snaps, preseedSnap)
	}

	return snaps, nil
}

func assemblePreseed(assert assertionBase) (Assertion, error) {
	// because the authority-id and model-id can differ (as per the model),
	// authority-id should be validated against allowed IDs when the preseed
	// blob is being checked

	_ := mylog.Check2(checkModel(assert.headers))

	_ = mylog.Check2(checkStringMatches(assert.headers, "system-label", validSystemLabel))

	snapList, ok := assert.headers["snaps"]
	if !ok {
		return nil, fmt.Errorf(`"snaps" header is mandatory`)
	}
	snaps := mylog.Check2(checkPreseedSnaps(snapList))

	_ = mylog.Check2(checkDigest(assert.headers, "artifact-sha3-384", crypto.SHA3_384))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	return &Preseed{
		assertionBase: assert,
		snaps:         snaps,
		timestamp:     timestamp,
	}, nil
}
