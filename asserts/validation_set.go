// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"regexp"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// Presence represents a presence constraint.
type Presence string

const (
	PresenceRequired Presence = "required"
	PresenceOptional Presence = "optional"
	PresenceInvalid  Presence = "invalid"
)

func presencesAsStrings(presences ...Presence) []string {
	strs := make([]string, len(presences))
	for i, pres := range presences {
		strs[i] = string(pres)
	}
	return strs
}

var validValidationSetSnapPresences = presencesAsStrings(PresenceRequired, PresenceOptional, PresenceInvalid)

func checkPresence(snap map[string]interface{}, which string, valid []string) (Presence, error) {
	presence := mylog.Check2(checkOptionalStringWhat(snap, "presence", which))

	if presence != "" && !strutil.ListContains(valid, presence) {
		return Presence(""), fmt.Errorf("presence %s must be one of %s", which, strings.Join(valid, "|"))
	}
	return Presence(presence), nil
}

// ValidationSetSnap holds the details about a snap constrained by a validation-set assertion.
type ValidationSetSnap struct {
	Name   string
	SnapID string

	Presence Presence

	Revision int
}

// SnapName implements naming.SnapRef.
func (s *ValidationSetSnap) SnapName() string {
	return s.Name
}

// ID implements naming.SnapRef.
func (s *ValidationSetSnap) ID() string {
	return s.SnapID
}

func checkValidationSetSnap(snap map[string]interface{}) (*ValidationSetSnap, error) {
	name := mylog.Check2(checkNotEmptyStringWhat(snap, "name", "of snap"))
	mylog.Check(naming.ValidateSnap(name))

	what := fmt.Sprintf("of snap %q", name)

	snapID := mylog.Check2(checkStringMatchesWhat(snap, "id", what, naming.ValidSnapID))

	presence := mylog.Check2(checkPresence(snap, what, validValidationSetSnapPresences))

	var snapRevision int
	if _, ok := snap["revision"]; ok {
		snapRevision = mylog.Check2(checkSnapRevisionWhat(snap, "revision", what))
	}
	if snapRevision != 0 && presence == PresenceInvalid {
		return nil, fmt.Errorf(`cannot specify revision %s at the same time as stating its presence is invalid`, what)
	}

	return &ValidationSetSnap{
		Name:     name,
		SnapID:   snapID,
		Presence: presence,
		Revision: snapRevision,
	}, nil
}

func checkValidationSetSnaps(snapList interface{}) ([]*ValidationSetSnap, error) {
	const wrongHeaderType = `"snaps" header must be a list of maps`

	entries, ok := snapList.([]interface{})
	if !ok {
		return nil, fmt.Errorf(wrongHeaderType)
	}

	seen := make(map[string]bool, len(entries))
	seenIDs := make(map[string]string, len(entries))
	snaps := make([]*ValidationSetSnap, 0, len(entries))
	for _, entry := range entries {
		snap, ok := entry.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf(wrongHeaderType)
		}
		valSetSnap := mylog.Check2(checkValidationSetSnap(snap))

		if seen[valSetSnap.Name] {
			return nil, fmt.Errorf("cannot list the same snap %q multiple times", valSetSnap.Name)
		}
		seen[valSetSnap.Name] = true
		snapID := valSetSnap.SnapID
		if underName := seenIDs[snapID]; underName != "" {
			return nil, fmt.Errorf("cannot specify the same snap id %q multiple times, specified for snaps %q and %q", snapID, underName, valSetSnap.Name)
		}
		seenIDs[snapID] = valSetSnap.Name

		if valSetSnap.Presence == "" {
			valSetSnap.Presence = PresenceRequired
		}

		snaps = append(snaps, valSetSnap)
	}

	return snaps, nil
}

// ValidationSet holds a validation-set assertion, which is a
// statement by an account about a set snaps and possibly revisions
// for which an extrinsic/implied property is valid (e.g. they work
// well together). validation-sets are organized in sequences under a
// name.
type ValidationSet struct {
	assertionBase

	seq int

	snaps []*ValidationSetSnap

	timestamp time.Time
}

// SequenceKey returns the sequence key for this validation set.
func (vs *ValidationSet) SequenceKey() string {
	return vsSequenceKey(vs.Series(), vs.AccountID(), vs.Name())
}

func vsSequenceKey(series, accountID, name string) string {
	return fmt.Sprintf("%s/%s/%s", series, accountID, name)
}

// Series returns the series for which the snap in the set are declared.
func (vs *ValidationSet) Series() string {
	return vs.HeaderString("series")
}

// AccountID returns the identifier of the account that signed this assertion.
func (vs *ValidationSet) AccountID() string {
	return vs.HeaderString("account-id")
}

// Name returns the name under which the validation-set is organized.
func (vs *ValidationSet) Name() string {
	return vs.HeaderString("name")
}

// Sequence returns the sequential number of the validation-set in its
// named sequence.
func (vs *ValidationSet) Sequence() int {
	return vs.seq
}

// Snaps returns the constrained snaps by the validation-set.
func (vs *ValidationSet) Snaps() []*ValidationSetSnap {
	return vs.snaps
}

// Timestamp returns the time when the validation-set was issued.
func (vs *ValidationSet) Timestamp() time.Time {
	return vs.timestamp
}

func checkSequence(headers map[string]interface{}, name string) (int, error) {
	seqnum := mylog.Check2(checkInt(headers, name))

	if seqnum < 1 {
		return -1, fmt.Errorf("%q must be >=1: %v", name, seqnum)
	}
	return seqnum, nil
}

var validValidationSetName = regexp.MustCompile("^[a-z0-9](?:-?[a-z0-9])*$")

func assembleValidationSet(assert assertionBase) (Assertion, error) {
	authorityID := assert.AuthorityID()
	accountID := assert.HeaderString("account-id")
	if accountID != authorityID {
		return nil, fmt.Errorf("authority-id and account-id must match, validation-set assertions are expected to be signed by the issuer account: %q != %q", authorityID, accountID)
	}

	_ := mylog.Check2(checkStringMatches(assert.headers, "name", validValidationSetName))

	seq := mylog.Check2(checkSequence(assert.headers, "sequence"))

	snapList, ok := assert.headers["snaps"]
	if !ok {
		return nil, fmt.Errorf(`"snaps" header is mandatory`)
	}
	snaps := mylog.Check2(checkValidationSetSnaps(snapList))

	timestamp := mylog.Check2(checkRFC3339Date(assert.headers, "timestamp"))

	return &ValidationSet{
		assertionBase: assert,
		seq:           seq,
		snaps:         snaps,
		timestamp:     timestamp,
	}, nil
}

func IsValidValidationSetName(name string) bool {
	return validValidationSetName.MatchString(name)
}
