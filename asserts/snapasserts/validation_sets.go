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

package snapasserts

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

// InstalledSnap holds the minimal details about an installed snap required to
// check it against validation sets.
type InstalledSnap struct {
	naming.SnapRef
	Revision snap.Revision
}

// NewInstalledSnap creates InstalledSnap.
func NewInstalledSnap(name, snapID string, revision snap.Revision) *InstalledSnap {
	return &InstalledSnap{
		SnapRef:  naming.NewSnapRef(name, snapID),
		Revision: revision,
	}
}

// ValidationSetsConflictError describes an error where multiple
// validation sets are in conflict about snaps.
type ValidationSetsConflictError struct {
	Sets  map[string]*asserts.ValidationSet
	Snaps map[string]error
}

func (e *ValidationSetsConflictError) Error() string {
	buf := bytes.NewBufferString("validation sets are in conflict:")
	for _, err := range e.Snaps {
		fmt.Fprintf(buf, "\n- %v", err)
	}
	return buf.String()
}

// ValidationSetsValidationError describes an error arising
// from validation of snaps against ValidationSets.
type ValidationSetsValidationError struct {
	// MissingSnaps maps missing snap names to the validation sets requiring them.
	MissingSnaps map[string][]string
	// InvalidSnaps maps snap names to the validation sets declaring them invalid.
	InvalidSnaps map[string][]string
	// WronRevisionSnaps maps snap names to the expected revisions and respective
	// validation sets that require them.
	WrongRevisionSnaps map[string]map[snap.Revision][]string
	// Sets maps validation set keys referenced by above maps to actual
	// validation sets.
	Sets map[string]*asserts.ValidationSet
}

type byRevision []snap.Revision

func (b byRevision) Len() int           { return len(b) }
func (b byRevision) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byRevision) Less(i, j int) bool { return b[i].N < b[j].N }

func (e *ValidationSetsValidationError) Error() string {
	buf := bytes.NewBufferString("validation sets assertions are not met:")
	printDetails := func(header string, details map[string][]string,
		printSnap func(snapName string, keys []string) string) {
		if len(details) == 0 {
			return
		}
		fmt.Fprintf(buf, "\n- %s:", header)
		for snapName, validationSetKeys := range details {
			fmt.Fprintf(buf, "\n  - %s", printSnap(snapName, validationSetKeys))
		}
	}

	printDetails("missing required snaps", e.MissingSnaps, func(snapName string, validationSetKeys []string) string {
		return fmt.Sprintf("%s (required by sets %s)", snapName, strings.Join(validationSetKeys, ","))
	})
	printDetails("invalid snaps", e.InvalidSnaps, func(snapName string, validationSetKeys []string) string {
		return fmt.Sprintf("%s (invalid for sets %s)", snapName, strings.Join(validationSetKeys, ","))
	})

	if len(e.WrongRevisionSnaps) > 0 {
		fmt.Fprint(buf, "\n- snaps at wrong revisions:")
		for snapName, revisions := range e.WrongRevisionSnaps {
			revisionsSorted := make([]snap.Revision, 0, len(revisions))
			for rev := range revisions {
				revisionsSorted = append(revisionsSorted, rev)
			}
			sort.Sort(byRevision(revisionsSorted))
			t := make([]string, 0, len(revisionsSorted))
			for _, rev := range revisionsSorted {
				keys := revisions[rev]
				t = append(t, fmt.Sprintf("at revision %s by sets %s", rev, strings.Join(keys, ",")))
			}
			fmt.Fprintf(buf, "\n  - %s (required %s)", snapName, strings.Join(t, ", "))
		}
	}

	return buf.String()
}

// ValidationSets can hold a combination of validation-set assertions
// and can check for conflicts or help applying them.
type ValidationSets struct {
	// sets maps sequence keys to validation-set in the combination
	sets map[string]*asserts.ValidationSet
	// snaps maps snap-ids to snap constraints
	snaps map[string]*snapContraints
}

const presConflict asserts.Presence = "conflict"

var unspecifiedRevision = snap.R(0)
var invalidPresRevision = snap.R(-1)

type snapContraints struct {
	name     string
	presence asserts.Presence
	// revisions maps revisions to pairing of ValidationSetSnap
	// and the originating validation-set key
	// * unspecifiedRevision is used for constraints without a
	//   revision
	// * invalidPresRevision is used for constraints that mark
	//   presence as invalid
	revisions map[snap.Revision][]*revConstraint
}

type revConstraint struct {
	validationSetKey string
	asserts.ValidationSetSnap
}

func (c *snapContraints) conflict() *snapConflictsError {
	if c.presence != presConflict {
		return nil
	}

	const dontCare asserts.Presence = ""
	whichSets := func(rcs []*revConstraint, presence asserts.Presence) []string {
		which := make([]string, 0, len(rcs))
		for _, rc := range rcs {
			if presence != dontCare && rc.Presence != presence {
				continue
			}
			which = append(which, rc.validationSetKey)
		}
		if len(which) == 0 {
			return nil
		}
		sort.Strings(which)
		return which
	}

	byRev := make(map[snap.Revision][]string, len(c.revisions))
	for r := range c.revisions {
		pres := dontCare
		switch r {
		case invalidPresRevision:
			pres = asserts.PresenceInvalid
		case unspecifiedRevision:
			pres = asserts.PresenceRequired
		}
		which := whichSets(c.revisions[r], pres)
		if len(which) != 0 {
			byRev[r] = which
		}
	}

	return &snapConflictsError{
		name:      c.name,
		revisions: byRev,
	}
}

type snapConflictsError struct {
	name string
	// revisions maps revisions to validation-set keys of the sets
	// that are in conflict over the revision.
	// * unspecifiedRevision is used for validation-sets conflicting
	//   on the snap by requiring it but without a revision
	// * invalidPresRevision is used for validation-sets that mark
	//   presence as invalid
	// see snapContraints.revisions as well
	revisions map[snap.Revision][]string
}

func (e *snapConflictsError) Error() string {
	whichSets := func(which []string) string {
		return fmt.Sprintf("(%s)", strings.Join(which, ","))
	}

	msg := fmt.Sprintf("cannot constrain snap %q", e.name)
	invalid := false
	if invalidOnes, ok := e.revisions[invalidPresRevision]; ok {
		msg += fmt.Sprintf(" as both invalid %s and required", whichSets(invalidOnes))
		invalid = true
	}

	var revnos []snap.Revision
	for r := range e.revisions {
		if r.N >= 1 {
			revnos = append(revnos, r)
		}
	}
	if len(revnos) == 1 {
		msg += fmt.Sprintf(" at revision %s %s", revnos[0], whichSets(e.revisions[revnos[0]]))
	} else if len(revnos) > 1 {
		sort.Sort(byRevision(revnos))
		l := make([]string, 0, len(revnos))
		for _, rev := range revnos {
			l = append(l, fmt.Sprintf("%s %s", rev, whichSets(e.revisions[rev])))
		}
		msg += fmt.Sprintf(" at different revisions %s", strings.Join(l, ", "))
	}

	if unspecifiedOnes, ok := e.revisions[unspecifiedRevision]; ok {
		which := whichSets(unspecifiedOnes)
		if which != "" {
			if len(revnos) != 0 {
				msg += " or"
			}
			if invalid {
				msg += fmt.Sprintf(" at any revision %s", which)
			} else {
				msg += fmt.Sprintf(" required at any revision %s", which)
			}
		}
	}
	return msg
}

// NewValidationSets returns a new ValidationSets.
func NewValidationSets() *ValidationSets {
	return &ValidationSets{
		sets:  map[string]*asserts.ValidationSet{},
		snaps: map[string]*snapContraints{},
	}
}

func valSetKey(valset *asserts.ValidationSet) string {
	return fmt.Sprintf("%s/%s", valset.AccountID(), valset.Name())
}

// Add adds the given asserts.ValidationSet to the combination.
// It errors if a validation-set with the same sequence key has been
// added already.
func (v *ValidationSets) Add(valset *asserts.ValidationSet) error {
	k := valSetKey(valset)
	if _, ok := v.sets[k]; ok {
		return fmt.Errorf("cannot add a second validation-set under %q", k)
	}
	v.sets[k] = valset
	for _, sn := range valset.Snaps() {
		v.addSnap(sn, k)
	}
	return nil
}

func (v *ValidationSets) addSnap(sn *asserts.ValidationSetSnap, validationSetKey string) {
	rev := snap.R(sn.Revision)
	if sn.Presence == asserts.PresenceInvalid {
		rev = invalidPresRevision
	}

	rc := &revConstraint{
		validationSetKey:  validationSetKey,
		ValidationSetSnap: *sn,
	}

	cs := v.snaps[sn.SnapID]
	if cs == nil {
		v.snaps[sn.SnapID] = &snapContraints{
			name:     sn.Name,
			presence: sn.Presence,
			revisions: map[snap.Revision][]*revConstraint{
				rev: {rc},
			},
		}
		return
	}

	cs.revisions[rev] = append(cs.revisions[rev], rc)
	if cs.presence == presConflict {
		// nothing to check anymore
		return
	}
	// this counts really different revisions or invalid
	ndiff := len(cs.revisions)
	if _, ok := cs.revisions[unspecifiedRevision]; ok {
		ndiff--
	}
	switch {
	case cs.presence == asserts.PresenceOptional:
		cs.presence = sn.Presence
		fallthrough
	case cs.presence == sn.Presence || sn.Presence == asserts.PresenceOptional:
		if ndiff > 1 {
			if cs.presence == asserts.PresenceRequired {
				// different revisions required/invalid
				cs.presence = presConflict
				return
			}
			// multiple optional at different revisions => invalid
			cs.presence = asserts.PresenceInvalid
		}
		return
	}
	// we are left with a combo of required and invalid => conflict
	cs.presence = presConflict
}

// Conflict returns a non-nil error if the combination is in conflict,
// nil otherwise.
func (v *ValidationSets) Conflict() error {
	sets := make(map[string]*asserts.ValidationSet)
	snaps := make(map[string]error)

	for snapID, snConstrs := range v.snaps {
		snConflictsErr := snConstrs.conflict()
		if snConflictsErr != nil {
			snaps[snapID] = snConflictsErr
			for _, valsetKeys := range snConflictsErr.revisions {
				for _, valsetKey := range valsetKeys {
					sets[valsetKey] = v.sets[valsetKey]
				}
			}
		}
	}

	if len(snaps) != 0 {
		return &ValidationSetsConflictError{
			Sets:  sets,
			Snaps: snaps,
		}
	}
	return nil
}

// CheckInstalledSnaps checks installed snaps against the validation sets.
func (v *ValidationSets) CheckInstalledSnaps(snaps []*InstalledSnap, ignoreValidation map[string]bool) error {
	installed := naming.NewSnapSet(nil)
	for _, sn := range snaps {
		installed.Add(sn)
	}

	// snapName -> validationSet key -> validation set
	invalid := make(map[string]map[string]bool)
	missing := make(map[string]map[string]bool)
	wrongrev := make(map[string]map[snap.Revision]map[string]bool)
	sets := make(map[string]*asserts.ValidationSet)

	for _, cstrs := range v.snaps {
		for rev, revCstr := range cstrs.revisions {
			for _, rc := range revCstr {
				sn := installed.Lookup(rc)
				isInstalled := sn != nil

				if isInstalled && ignoreValidation[rc.Name] {
					continue
				}

				switch {
				case !isInstalled && (cstrs.presence == asserts.PresenceOptional || cstrs.presence == asserts.PresenceInvalid):
					// not installed, but optional or not required
				case isInstalled && cstrs.presence == asserts.PresenceInvalid:
					// installed but not expected to be present
					if invalid[rc.Name] == nil {
						invalid[rc.Name] = make(map[string]bool)
					}
					invalid[rc.Name][rc.validationSetKey] = true
					sets[rc.validationSetKey] = v.sets[rc.validationSetKey]
				case isInstalled:
					// presence is either optional or required
					if rev != unspecifiedRevision && rev != sn.(*InstalledSnap).Revision {
						// expected a different revision
						if wrongrev[rc.Name] == nil {
							wrongrev[rc.Name] = make(map[snap.Revision]map[string]bool)
						}
						if wrongrev[rc.Name][rev] == nil {
							wrongrev[rc.Name][rev] = make(map[string]bool)
						}
						wrongrev[rc.Name][rev][rc.validationSetKey] = true
						sets[rc.validationSetKey] = v.sets[rc.validationSetKey]
					}
				default:
					// not installed but required.
					// note, not checking ignoreValidation here because it's not a viable scenario (it's not
					// possible to have enforced validation set while not having the required snap at all - it
					// is only possible to have it with a wrong revision, or installed while invalid, in both
					// cases through --ignore-validation flag).
					if missing[rc.Name] == nil {
						missing[rc.Name] = make(map[string]bool)
					}
					missing[rc.Name][rc.validationSetKey] = true
					sets[rc.validationSetKey] = v.sets[rc.validationSetKey]
				}
			}
		}
	}

	setsToLists := func(in map[string]map[string]bool) map[string][]string {
		if len(in) == 0 {
			return nil
		}
		out := make(map[string][]string)
		for snap, sets := range in {
			out[snap] = make([]string, 0, len(sets))
			for validationSetKey := range sets {
				out[snap] = append(out[snap], validationSetKey)
			}
			sort.Strings(out[snap])
		}
		return out
	}

	if len(invalid) > 0 || len(missing) > 0 || len(wrongrev) > 0 {
		verr := &ValidationSetsValidationError{
			InvalidSnaps: setsToLists(invalid),
			MissingSnaps: setsToLists(missing),
			Sets:         sets,
		}
		if len(wrongrev) > 0 {
			verr.WrongRevisionSnaps = make(map[string]map[snap.Revision][]string)
			for snapName, revs := range wrongrev {
				verr.WrongRevisionSnaps[snapName] = make(map[snap.Revision][]string)
				for rev, keys := range revs {
					for key := range keys {
						verr.WrongRevisionSnaps[snapName][rev] = append(verr.WrongRevisionSnaps[snapName][rev], key)
					}
					sort.Strings(verr.WrongRevisionSnaps[snapName][rev])
				}
			}
		}
		return verr
	}
	return nil
}

// PresenceConstraintError describes an error where presence of the given snap
// has unexpected value, e.g. it's "invalid" while checking for "required".
type PresenceConstraintError struct {
	SnapName string
	Presence asserts.Presence
}

func (e *PresenceConstraintError) Error() string {
	return fmt.Sprintf("unexpected presence %q for snap %q", e.Presence, e.SnapName)
}

func (v *ValidationSets) constraintsForSnap(snapRef naming.SnapRef) *snapContraints {
	if snapRef.ID() != "" {
		return v.snaps[snapRef.ID()]
	}
	// snapID not available, find by snap name
	for _, cstrs := range v.snaps {
		if cstrs.name == snapRef.SnapName() {
			return cstrs
		}
	}
	return nil
}

// CheckPresenceRequired returns the list of all validation sets that declare
// presence of the given snap as required and the required revision (or
// snap.R(0) if no specific revision is required). PresenceConstraintError is
// returned if presence of the snap is "invalid".
// The method assumes that validation sets are not in conflict.
func (v *ValidationSets) CheckPresenceRequired(snapRef naming.SnapRef) ([]string, snap.Revision, error) {
	cstrs := v.constraintsForSnap(snapRef)
	if cstrs == nil {
		return nil, unspecifiedRevision, nil
	}
	if cstrs.presence == asserts.PresenceInvalid {
		return nil, unspecifiedRevision, &PresenceConstraintError{snapRef.SnapName(), cstrs.presence}
	}
	if cstrs.presence != asserts.PresenceRequired {
		return nil, unspecifiedRevision, nil
	}

	snapRev := unspecifiedRevision
	var keys []string
	for rev, revCstr := range cstrs.revisions {
		for _, rc := range revCstr {
			vs := v.sets[rc.validationSetKey]
			if vs == nil {
				return nil, unspecifiedRevision, fmt.Errorf("internal error: no validation set for %q", rc.validationSetKey)
			}
			keys = append(keys, strings.Join(vs.Ref().PrimaryKey, "/"))
			// there may be constraints without revision; only set snapRev if
			// it wasn't already determined. Note that if revisions are set,
			// then they are the same, otherwise validation sets would be in
			// conflict.
			// This is an equivalent of 'if rev != unspecifiedRevision`.
			if snapRev == unspecifiedRevision {
				snapRev = rev
			}
		}
	}

	sort.Strings(keys)
	return keys, snapRev, nil
}

// CheckPresenceInvalid returns the list of all validation sets that declare
// presence of the given snap as invalid. PresenceConstraintError is returned if
// presence of the snap is "optional" or "required".
// The method assumes that validation sets are not in conflict.
func (v *ValidationSets) CheckPresenceInvalid(snapRef naming.SnapRef) ([]string, error) {
	cstrs := v.constraintsForSnap(snapRef)
	if cstrs == nil {
		return nil, nil
	}
	if cstrs.presence != asserts.PresenceInvalid {
		return nil, &PresenceConstraintError{snapRef.SnapName(), cstrs.presence}
	}
	var keys []string
	for _, revCstr := range cstrs.revisions {
		for _, rc := range revCstr {
			if rc.Presence == asserts.PresenceInvalid {
				vs := v.sets[rc.validationSetKey]
				if vs == nil {
					return nil, fmt.Errorf("internal error: no validation set for %q", rc.validationSetKey)
				}
				keys = append(keys, strings.Join(vs.Ref().PrimaryKey, "/"))
			}
		}
	}

	sort.Strings(keys)
	return keys, nil
}
