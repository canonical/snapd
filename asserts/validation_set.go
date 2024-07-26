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
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

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

func checkOptionalPresence(headers map[string]interface{}, which string, valid []string) (Presence, error) {
	presence, err := checkOptionalStringWhat(headers, "presence", which)
	if err != nil {
		return Presence(""), err
	}
	if presence != "" && !strutil.ListContains(valid, presence) {
		return Presence(""), fmt.Errorf("presence %s must be one of %s", which, strings.Join(valid, "|"))
	}
	return Presence(presence), nil
}

func checkPresence(headers map[string]interface{}, which string, valid []string) (Presence, error) {
	presence, err := checkExistsStringWhat(headers, "presence", which)
	if err != nil {
		return "", err
	}
	if presence != "" && !strutil.ListContains(valid, presence) {
		return "", fmt.Errorf("presence %s must be one of %s", which, strings.Join(valid, "|"))
	}
	return Presence(presence), nil
}

// ValidationSetSnap holds the details about a snap constrained by a validation-set assertion.
type ValidationSetSnap struct {
	Name   string
	SnapID string

	Presence Presence

	Revision   int
	Components map[string]ValidationSetComponent
}

type ValidationSetComponent struct {
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
	name, err := checkNotEmptyStringWhat(snap, "name", "of snap")
	if err != nil {
		return nil, err
	}
	if err := naming.ValidateSnap(name); err != nil {
		return nil, fmt.Errorf("invalid snap name %q", name)
	}

	what := fmt.Sprintf("of snap %q", name)

	snapID, err := checkStringMatchesWhat(snap, "id", what, naming.ValidSnapID)
	if err != nil {
		return nil, err
	}

	presence, err := checkOptionalPresence(snap, what, validValidationSetSnapPresences)
	if err != nil {
		return nil, err
	}

	var snapRevision int
	if _, ok := snap["revision"]; ok {
		var err error
		snapRevision, err = checkSnapRevisionWhat(snap, "revision", what)
		if err != nil {
			return nil, err
		}
	}
	if snapRevision != 0 && presence == PresenceInvalid {
		return nil, fmt.Errorf(`cannot specify revision %s at the same time as stating its presence is invalid`, what)
	}

	components, err := checkValidationSetComponents(snap, what)
	if err != nil {
		return nil, err
	}

	return &ValidationSetSnap{
		Name:       name,
		SnapID:     snapID,
		Presence:   presence,
		Revision:   snapRevision,
		Components: components,
	}, nil
}

func checkValidationSetComponents(snap map[string]interface{}, what string) (map[string]ValidationSetComponent, error) {
	mapping, err := checkMapWhat(snap, "components", what)
	if err != nil {
		return nil, errors.New(`"components" field in "snaps" header must be a map`)
	}

	if len(mapping) == 0 {
		return nil, nil
	}

	components := make(map[string]ValidationSetComponent, len(mapping))
	for name, comp := range mapping {
		var parsed map[string]interface{}
		switch c := comp.(type) {
		case map[string]interface{}:
			parsed = c
		case string:
			parsed = map[string]interface{}{"presence": c}
		default:
			return nil, errors.New(`each field in "components" map must be either a map or a string`)
		}

		component, err := checkValidationSetComponent(parsed, name)
		if err != nil {
			return nil, err
		}
		components[name] = component
	}

	return components, nil
}

func checkValidationSetComponent(comp map[string]interface{}, name string) (ValidationSetComponent, error) {
	if err := naming.ValidateSnap(name); err != nil {
		return ValidationSetComponent{}, fmt.Errorf("invalid component name %q", name)
	}

	what := fmt.Sprintf("of component %q", name)

	presence, err := checkPresence(comp, what, validValidationSetSnapPresences)
	if err != nil {
		return ValidationSetComponent{}, err
	}

	revision, err := checkOptionalSnapRevisionWhat(comp, "revision", what)
	if err != nil {
		return ValidationSetComponent{}, err
	}

	if revision != 0 && presence == PresenceInvalid {
		return ValidationSetComponent{}, fmt.Errorf(`cannot specify component revision %s at the same time as stating its presence is invalid`, what)
	}

	return ValidationSetComponent{
		Presence: presence,
		Revision: revision,
	}, nil
}

func checkValidationSetSnaps(snapList interface{}) ([]*ValidationSetSnap, error) {
	const wrongHeaderType = `"snaps" header must be a list of maps`

	entries, ok := snapList.([]interface{})
	if !ok {
		return nil, errors.New(wrongHeaderType)
	}

	seen := make(map[string]bool, len(entries))
	seenIDs := make(map[string]string, len(entries))
	snaps := make([]*ValidationSetSnap, 0, len(entries))
	for _, entry := range entries {
		snap, ok := entry.(map[string]interface{})
		if !ok {
			return nil, errors.New(wrongHeaderType)
		}
		valSetSnap, err := checkValidationSetSnap(snap)
		if err != nil {
			return nil, err
		}

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
	seqnum, err := checkInt(headers, name)
	if err != nil {
		return -1, err
	}
	if seqnum < 1 {
		return -1, fmt.Errorf("%q must be >=1: %v", name, seqnum)
	}
	return seqnum, nil
}

var (
	validValidationSetName = regexp.MustCompile("^[a-z0-9](?:-?[a-z0-9])*$")
)

func assembleValidationSet(assert assertionBase) (Assertion, error) {
	authorityID := assert.AuthorityID()
	accountID := assert.HeaderString("account-id")
	if accountID != authorityID {
		return nil, fmt.Errorf("authority-id and account-id must match, validation-set assertions are expected to be signed by the issuer account: %q != %q", authorityID, accountID)
	}

	_, err := checkStringMatches(assert.headers, "name", validValidationSetName)
	if err != nil {
		return nil, err
	}

	seq, err := checkSequence(assert.headers, "sequence")
	if err != nil {
		return nil, err
	}

	snapList, ok := assert.headers["snaps"]
	if !ok {
		return nil, fmt.Errorf(`"snaps" header is mandatory`)
	}
	snaps, err := checkValidationSetSnaps(snapList)
	if err != nil {
		return nil, err
	}

	timestamp, err := checkRFC3339Date(assert.headers, "timestamp")
	if err != nil {
		return nil, err
	}

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
