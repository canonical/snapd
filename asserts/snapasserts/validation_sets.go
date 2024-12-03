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
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

// InstalledSnap holds the minimal details about an installed snap required to
// check it against validation sets.
type InstalledSnap struct {
	naming.SnapRef
	Revision   snap.Revision
	Components []InstalledComponent
}

type InstalledComponent struct {
	naming.ComponentRef
	Revision snap.Revision
}

// NewInstalledSnap creates InstalledSnap.
func NewInstalledSnap(name, snapID string, revision snap.Revision, components []InstalledComponent) *InstalledSnap {
	return &InstalledSnap{
		SnapRef:    naming.NewSnapRef(name, snapID),
		Revision:   revision,
		Components: components,
	}
}

// ValidationSetsConflictError describes an error where multiple
// validation sets are in conflict about snaps.
type ValidationSetsConflictError struct {
	Sets       map[string]*asserts.ValidationSet
	Snaps      map[string]error
	Components map[string]map[string]error
}

func (e *ValidationSetsConflictError) Error() string {
	buf := bytes.NewBufferString("validation sets are in conflict:")
	seen := make(map[string]bool)
	for id, err := range e.Snaps {
		fmt.Fprintf(buf, "\n- %v", err)
		seen[id] = true

		// if we have any component errors, we should put them next to the snap
		// errors
		for _, err := range e.Components[id] {
			fmt.Fprintf(buf, "\n- %v", err)
		}
	}

	for id, errs := range e.Components {
		// if we've already seen the snap, then we have already printed the
		// component errors
		if seen[id] {
			continue
		}
		for _, err := range errs {
			fmt.Fprintf(buf, "\n- %v", err)
		}
	}

	return buf.String()
}

func (e *ValidationSetsConflictError) Is(err error) bool {
	_, ok := err.(*ValidationSetsConflictError)
	return ok
}

// ValidationSetsValidationError describes an error arising
// from validation of snaps against ValidationSets.
type ValidationSetsValidationError struct {
	// MissingSnaps maps missing snap names to the expected revisions and respective validation sets requiring them.
	// Revisions may be unset if no specific revision is required
	MissingSnaps map[string]map[snap.Revision][]string
	// InvalidSnaps maps snap names to the validation sets declaring them invalid.
	InvalidSnaps map[string][]string
	// WronRevisionSnaps maps snap names to the expected revisions and respective
	// validation sets that require them.
	WrongRevisionSnaps map[string]map[snap.Revision][]string
	// Sets maps validation set keys to all validation sets assertions considered
	// in the failed check.
	Sets map[string]*asserts.ValidationSet
	// ComponentErrors is a map of snap names to ValidationSetsComponentValidationError values.
	ComponentErrors map[string]*ValidationSetsComponentValidationError
}

// ValidationSetsComponentValidationError describes an error arising from
// validation of components of snaps against ValidationSets.
type ValidationSetsComponentValidationError struct {
	// MissingComponents maps missing component names to the expected revisions
	// and respective validation sets requiring them. Revisions may be unset if
	// no specific revision is required
	MissingComponents map[string]map[snap.Revision][]string
	// InvalidComponents maps component names to the validation sets declaring
	// them invalid.
	InvalidComponents map[string][]string
	// WronRevisionComponents maps component names to the expected revisions and
	// respective validation sets that require them.
	WrongRevisionComponents map[string]map[snap.Revision][]string
}

// ValidationSetKey is a string-backed primary key for a validation set assertion.
type ValidationSetKey string

// NewValidationSetKey returns a validation set key for a validation set.
func NewValidationSetKey(vs *asserts.ValidationSet) ValidationSetKey {
	return ValidationSetKey(strings.Join(vs.Ref().PrimaryKey, "/"))
}

func (k ValidationSetKey) String() string {
	return string(k)
}

// Components returns the components of the validation set's primary key (see
// assertion types in asserts/asserts.go).
func (k ValidationSetKey) Components() []string {
	return strings.Split(k.String(), "/")
}

// ValidationSetKeySlice can be used to sort slices of ValidationSetKey.
type ValidationSetKeySlice []ValidationSetKey

func (s ValidationSetKeySlice) Len() int           { return len(s) }
func (s ValidationSetKeySlice) Less(i, j int) bool { return s[i] < s[j] }
func (s ValidationSetKeySlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// CommaSeparated returns the validation set keys separated by commas.
func (s ValidationSetKeySlice) CommaSeparated() string {
	var sb strings.Builder

	for i, vsKey := range s {
		sb.WriteString(vsKey.String())
		if i < len(s)-1 {
			sb.WriteRune(',')
		}
	}

	return sb.String()
}

type byRevision []snap.Revision

func (b byRevision) Len() int           { return len(b) }
func (b byRevision) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byRevision) Less(i, j int) bool { return b[i].N < b[j].N }

func (e *ValidationSetsValidationError) Error() string {
	buf := bytes.NewBufferString("validation sets assertions are not met:")

	if len(e.MissingSnaps) > 0 {
		fmt.Fprintf(buf, "\n- missing required snaps:")
		for name, revisions := range e.MissingSnaps {
			writeMissingError(buf, name, revisions)
		}
	}

	if len(e.InvalidSnaps) > 0 {
		fmt.Fprintf(buf, "\n- invalid snaps:")
		for snapName, validationSetKeys := range e.InvalidSnaps {
			fmt.Fprintf(buf, "\n  - %s (invalid for sets %s)", snapName, strings.Join(validationSetKeys, ","))
		}
	}

	if len(e.WrongRevisionSnaps) > 0 {
		fmt.Fprint(buf, "\n- snaps at wrong revisions:")
		for snapName, revisions := range e.WrongRevisionSnaps {
			writeWrongRevisionError(buf, snapName, revisions)
		}
	}

	// the data structure here isn't really conducive to creating a
	// non-hierarchical error message, maybe worth reorganizing. however, it
	// will probably be better for actually using the error to extract what we
	// need when resolving validation set errors in snapstate. it is also more
	// representative of how the constraints are represented in the actual
	// assertion.

	var missingComps, invalidComps, wrongRevComps strings.Builder
	for snapName, vcerr := range e.ComponentErrors {
		for _, compName := range sortedStringKeys(vcerr.MissingComponents) {
			writeMissingError(
				&missingComps,
				naming.NewComponentRef(snapName, compName).String(),
				vcerr.MissingComponents[compName],
			)
		}

		for _, compName := range sortedStringKeys(vcerr.InvalidComponents) {
			fmt.Fprintf(
				&invalidComps,
				"\n  - %s (invalid for sets %s)",
				naming.NewComponentRef(snapName, compName).String(),
				strings.Join(vcerr.InvalidComponents[compName], ","),
			)
		}

		for _, compName := range sortedStringKeys(vcerr.WrongRevisionComponents) {
			writeWrongRevisionError(
				&wrongRevComps,
				naming.NewComponentRef(snapName, compName).String(),
				vcerr.WrongRevisionComponents[compName],
			)
		}
	}

	if missingComps.Len() > 0 {
		buf.WriteString("\n- missing required components:")
		buf.WriteString(missingComps.String())
	}
	if invalidComps.Len() > 0 {
		buf.WriteString("\n- invalid components:")
		buf.WriteString(invalidComps.String())
	}
	if wrongRevComps.Len() > 0 {
		buf.WriteString("\n- components at wrong revisions:")
		buf.WriteString(wrongRevComps.String())
	}

	return buf.String()
}

func sortedStringKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeWrongRevisionError(w io.Writer, name string, revisions map[snap.Revision][]string) {
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
	fmt.Fprintf(w, "\n  - %s (required %s)", name, strings.Join(t, ", "))
}

func writeMissingError(w io.Writer, name string, revisions map[snap.Revision][]string) {
	revisionsSorted := make([]snap.Revision, 0, len(revisions))
	for rev := range revisions {
		revisionsSorted = append(revisionsSorted, rev)
	}
	sort.Sort(byRevision(revisionsSorted))
	t := make([]string, 0, len(revisionsSorted))
	for _, rev := range revisionsSorted {
		keys := revisions[rev]
		if rev.Unset() {
			t = append(t, fmt.Sprintf("at any revision by sets %s", strings.Join(keys, ",")))
		} else {
			t = append(t, fmt.Sprintf("at revision %s by sets %s", rev, strings.Join(keys, ",")))
		}
	}
	fmt.Fprintf(w, "\n  - %s (required %s)", name, strings.Join(t, ", "))
}

// ValidationSets can hold a combination of validation-set assertions
// and can check for conflicts or help applying them.
type ValidationSets struct {
	// sets maps sequence keys to validation-set in the combination
	sets map[string]*asserts.ValidationSet
	// snaps maps snap-ids to snap constraints
	snaps map[string]*snapConstraints
}

const presConflict asserts.Presence = "conflict"

var unspecifiedRevision = snap.R(0)
var invalidPresRevision = snap.R(-1)

type snapConstraints struct {
	constraints
	componentConstraints map[string]*componentConstraints
}

// TODO: this type isn't needed, maybe makes things a bit clearer though?
type componentConstraints struct {
	constraints
}

type constraints struct {
	// name of the snap or component that is being constrained
	name string
	// presence of the snap or component, considering
	// all of the constraints that impact it. if any of the constraints are in
	// conflict, the presence is set to presConflict.
	presence asserts.Presence
	// revisions maps revisions to pairs of that revision's allowed presence and
	// the originating validation-set key that enforces that presence
	// * unspecifiedRevision is used for constraints without a revision
	// * invalidPresRevision is used for constraints that mark presence as
	//   invalid
	revisions map[snap.Revision][]revConstraint

	// snapRef will always be present.
	snapRef naming.SnapRef
	// compRef will be nil if the constraint is for a snap.
	compRef *naming.ComponentRef
}

type revConstraint struct {
	validationSetKey string
	presence         asserts.Presence
}

func constraintsConflicts(c constraints) *conflictsError {
	if c.presence != presConflict {
		return nil
	}

	const dontCare asserts.Presence = ""
	whichSets := func(rcs []revConstraint, presence asserts.Presence) []string {
		which := make([]string, 0, len(rcs))
		for _, rc := range rcs {
			if presence != dontCare && rc.presence != presence {
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

	containerType := "snap"
	name := c.name
	if c.compRef != nil {
		containerType = "component"
		name = c.compRef.String()
	}

	return &conflictsError{
		name:          name,
		containerType: containerType,
		revisions:     byRev,
	}
}

func (c *snapConstraints) conflicts() (snap *conflictsError, components map[string]*conflictsError) {
	componentConflicts := make(map[string]*conflictsError)
	for _, cstrs := range c.componentConstraints {
		conflict := constraintsConflicts(cstrs.constraints)
		if conflict != nil {
			componentConflicts[cstrs.name] = conflict
		}
	}

	return constraintsConflicts(c.constraints), componentConflicts
}

type conflictsError struct {
	name          string
	containerType string
	// revisions maps revisions to validation-set keys of the sets
	// that are in conflict over the revision.
	// * unspecifiedRevision is used for validation-sets conflicting
	//   on the snap by requiring it but without a revision
	// * invalidPresRevision is used for validation-sets that mark
	//   presence as invalid
	// see snapConstraints.revisions as well
	revisions map[snap.Revision][]string
}

func (e *conflictsError) Error() string {
	whichSets := func(which []string) string {
		return fmt.Sprintf("(%s)", strings.Join(which, ","))
	}

	msg := fmt.Sprintf("cannot constrain %s %q", e.containerType, e.name)
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
		snaps: map[string]*snapConstraints{},
	}
}

func valSetKey(valset *asserts.ValidationSet) string {
	return fmt.Sprintf("%s/%s", valset.AccountID(), valset.Name())
}

// Revisions returns the set of snap revisions that is enforced by the
// validation sets that ValidationSets manages.
func (v *ValidationSets) Revisions() (map[string]snap.Revision, error) {
	if err := v.Conflict(); err != nil {
		return nil, fmt.Errorf("cannot get revisions when validation sets are in conflict: %w", err)
	}

	snapNameToRevision := make(map[string]snap.Revision, len(v.snaps))
	for _, sn := range v.snaps {
		for revision := range sn.revisions {
			switch revision {
			case invalidPresRevision, unspecifiedRevision:
				continue
			default:
				snapNameToRevision[sn.name] = revision
			}
		}
	}
	return snapNameToRevision, nil
}

// Keys returns a slice of ValidationSetKey structs that represent each
// validation set that this ValidationSets knows about.
func (v *ValidationSets) Keys() []ValidationSetKey {
	keys := make(ValidationSetKeySlice, 0, len(v.sets))
	for _, vs := range v.sets {
		keys = append(keys, NewValidationSetKey(vs))
	}
	sort.Sort(keys)
	return keys
}

// Empty returns true if this ValidationSets hasn't had any validation sets
// added to it. An empty ValidationSets doesn't enforce any constraints on the
// state of snaps.
func (v *ValidationSets) Empty() bool {
	return len(v.sets) == 0
}

// Sets returns a slice of all of the validation sets that this ValidationSets
// knows about.
func (v *ValidationSets) Sets() []*asserts.ValidationSet {
	sets := make([]*asserts.ValidationSet, 0, len(v.sets))
	for _, vs := range v.sets {
		sets = append(sets, vs)
	}
	return sets
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

func (sc *snapConstraints) addComponents(comps map[string]asserts.ValidationSetComponent, validationSetKey string) {
	for name, comp := range comps {
		sc.addComponent(name, comp, validationSetKey)
	}
}

func (sc *snapConstraints) addComponent(compName string, comp asserts.ValidationSetComponent, validationSetKey string) {
	rev := snap.R(comp.Revision)
	if comp.Presence == asserts.PresenceInvalid {
		rev = invalidPresRevision
	}

	rc := revConstraint{
		validationSetKey: validationSetKey,
		presence:         comp.Presence,
	}

	compRef := naming.NewComponentRef(sc.name, compName)

	cs := sc.componentConstraints[compName]
	if cs == nil {
		sc.componentConstraints[compName] = &componentConstraints{
			constraints: constraints{
				name:     compName,
				presence: comp.Presence,
				revisions: map[snap.Revision][]revConstraint{
					rev: {rc},
				},
				compRef: &compRef,
				snapRef: sc.snapRef,
			},
		}
		return
	}

	cs.revisions[rev] = append(cs.revisions[rev], rc)

	// this counts really different revisions or invalid
	ndiff := len(cs.revisions)
	if _, ok := cs.revisions[unspecifiedRevision]; ok {
		ndiff--
	}

	cs.presence = derivePresence(cs.presence, comp.Presence, ndiff)
}

func (v *ValidationSets) addSnap(sn *asserts.ValidationSetSnap, validationSetKey string) {
	rev := snap.R(sn.Revision)
	if sn.Presence == asserts.PresenceInvalid {
		rev = invalidPresRevision
	}

	rc := revConstraint{
		validationSetKey: validationSetKey,
		presence:         sn.Presence,
	}

	sc := v.snaps[sn.SnapID]
	if sc == nil {
		v.snaps[sn.SnapID] = &snapConstraints{
			constraints: constraints{
				name:     sn.Name,
				presence: sn.Presence,
				revisions: map[snap.Revision][]revConstraint{
					rev: {rc},
				},
				snapRef: sn,
			},
			componentConstraints: make(map[string]*componentConstraints),
		}
		v.snaps[sn.SnapID].addComponents(sn.Components, validationSetKey)
		return
	}

	sc.addComponents(sn.Components, validationSetKey)

	sc.revisions[rev] = append(sc.revisions[rev], rc)

	// this counts really different revisions or invalid
	ndiff := len(sc.revisions)
	if _, ok := sc.revisions[unspecifiedRevision]; ok {
		ndiff--
	}

	sc.presence = derivePresence(sc.presence, sn.Presence, ndiff)
}

func derivePresence(currentPresence, incomingPresence asserts.Presence, revisions int) asserts.Presence {
	if currentPresence == presConflict {
		// nothing to check anymore
		return presConflict
	}

	if currentPresence == asserts.PresenceOptional {
		currentPresence = incomingPresence
	}

	if currentPresence == incomingPresence || incomingPresence == asserts.PresenceOptional {
		if revisions > 1 {
			if currentPresence == asserts.PresenceRequired {
				// different revisions required/invalid
				return presConflict
			}
			// multiple optional at different revisions => invalid
			return asserts.PresenceInvalid
		}
		return currentPresence
	}

	// we are left with a combo of required and invalid => conflict
	return presConflict
}

// Conflict returns a non-nil error if the combination is in conflict,
// nil otherwise.
func (v *ValidationSets) Conflict() error {
	sets := make(map[string]*asserts.ValidationSet)
	snaps := make(map[string]error)
	components := make(map[string]map[string]error)

	for snapID, snConstrs := range v.snaps {
		snapConflicts, componentConflicts := snConstrs.conflicts()
		if snapConflicts != nil {
			snaps[snapID] = snapConflicts
			for _, valsetKeys := range snapConflicts.revisions {
				for _, valsetKey := range valsetKeys {
					sets[valsetKey] = v.sets[valsetKey]
				}
			}
		}

		if len(componentConflicts) != 0 {
			components[snapID] = make(map[string]error)
		}

		for _, conflicts := range componentConflicts {
			components[snapID][conflicts.name] = conflicts
			for _, valsetKeys := range conflicts.revisions {
				for _, valsetKey := range valsetKeys {
					sets[valsetKey] = v.sets[valsetKey]
				}
			}
		}
	}

	if len(snaps) != 0 || len(components) != 0 {
		return &ValidationSetsConflictError{
			Sets:       sets,
			Snaps:      snaps,
			Components: components,
		}
	}
	return nil
}

type constraintConformity int

const (
	constraintConformityValid constraintConformity = iota
	constraintConformityInvalid
	constraintConformityMissing
	constraintConformityWrongRevision
)

// checkConstraintConformity checks that a given revision conforms with a given
// required revision and presence. if installedRev is unset, we consider the
// constraint target to not be installed.
func checkConstraintConformity(installedRev, requiredRev snap.Revision, presence asserts.Presence) constraintConformity {
	installed := !installedRev.Unset()
	notRequired := presence == asserts.PresenceOptional || presence == asserts.PresenceInvalid
	switch {
	case !installed && notRequired:
		// not installed, but optional or not required
		return constraintConformityValid
	case installed && presence == asserts.PresenceInvalid:
		// installed but not expected to be present
		return constraintConformityInvalid
	case installed:
		// presence is either optional or required
		if requiredRev != unspecifiedRevision && requiredRev != installedRev {
			return constraintConformityWrongRevision
		}
		return constraintConformityValid
	default:
		// not installed but required.
		return constraintConformityMissing
	}
}

// checkConstraints checks that all of the revision constraints contained
// within the given constraints are satisfied by the installed revision of the
// target of the constraint if installedRev is unset, we consider the constraint
// target to be uninstalled.
func checkConstraints(cs constraints, installedRev snap.Revision) (
	// invalid validation set keys
	invalid []string,
	// required revision (could be unspecified) -> invalid validation set keys
	missing map[snap.Revision][]string,
	// required revision (should never be unspecified) -> invalid validation set keys
	wrongrev map[snap.Revision][]string,
	err error,
) {
	invalidSet := make(map[string]bool)
	missingSets := make(map[snap.Revision]map[string]bool)
	wrongrevSets := make(map[snap.Revision]map[string]bool)

	for requiredRev, revCstr := range cs.revisions {
		for _, rc := range revCstr {
			conformity := checkConstraintConformity(installedRev, requiredRev, cs.presence)
			switch conformity {
			case constraintConformityInvalid:
				invalidSet[rc.validationSetKey] = true
			case constraintConformityMissing:
				if missingSets[requiredRev] == nil {
					missingSets[requiredRev] = make(map[string]bool)
				}
				missingSets[requiredRev][rc.validationSetKey] = true
			case constraintConformityWrongRevision:
				if wrongrevSets[requiredRev] == nil {
					wrongrevSets[requiredRev] = make(map[string]bool)
				}
				wrongrevSets[requiredRev][rc.validationSetKey] = true
			case constraintConformityValid:
				continue
			default:
				return nil, nil, nil, fmt.Errorf("internal error: unknown conformity %d", conformity)
			}
		}
	}

	setToLists := func(in map[string]bool) []string {
		if len(in) == 0 {
			return nil
		}
		out := make([]string, 0, len(in))
		for key := range in {
			out = append(out, key)
		}
		sort.Strings(out)
		return out
	}

	invalid = setToLists(invalidSet)
	missing = make(map[snap.Revision][]string)
	for rev, set := range missingSets {
		missing[rev] = setToLists(set)
	}

	wrongrev = make(map[snap.Revision][]string)
	for rev, set := range wrongrevSets {
		wrongrev[rev] = setToLists(set)
	}

	return invalid, missing, wrongrev, nil
}

// CheckInstalledSnaps checks installed snaps against the validation sets.
func (v *ValidationSets) CheckInstalledSnaps(snaps []*InstalledSnap, ignoreValidation map[string]bool) error {
	installed := newInstalledSnapSet(snaps)
	snapInstalled := func(cstrs constraints) (snap.Revision, error) {
		if cstrs.snapRef == nil {
			return snap.Revision{}, errors.New("internal error: snap constraint should have a snap ref")
		}

		sn := installed.Lookup(cstrs.snapRef)
		if sn == nil {
			return snap.R(0), nil
		}
		return sn.Revision, nil
	}

	snapConstraints := make([]constraints, 0, len(v.snaps))
	for _, sc := range v.snaps {
		snapConstraints = append(snapConstraints, sc.constraints)
	}

	invalid, missing, wrongrev, err := checkManyConstraints(snapConstraints, snapInstalled, ignoreValidation)
	if err != nil {
		return err
	}

	missingSnapNames := make(map[string]bool, len(missing))
	for name := range missing {
		missingSnapNames[name] = true
	}

	vcerrs, err := v.checkInstalledComponents(installed, ignoreValidation, missingSnapNames)
	if err != nil {
		return err
	}

	if len(invalid) > 0 || len(missing) > 0 || len(wrongrev) > 0 || len(vcerrs) > 0 {
		return &ValidationSetsValidationError{
			InvalidSnaps:       invalid,
			MissingSnaps:       missing,
			WrongRevisionSnaps: wrongrev,
			Sets:               v.sets,
			ComponentErrors:    vcerrs,
		}
	}
	return nil
}

func (v *ValidationSets) checkInstalledComponents(installedSnaps installedSnapSet, ignore map[string]bool, missingSnaps map[string]bool) (map[string]*ValidationSetsComponentValidationError, error) {
	componentInstalled := func(cstrs constraints) (snap.Revision, error) {
		if cstrs.compRef == nil || cstrs.snapRef == nil {
			return snap.Revision{}, errors.New("internal error: component constraint should have component and snap refs")
		}

		comp := installedSnaps.LookupComponent(cstrs.snapRef, *cstrs.compRef)
		if comp == nil {
			return snap.Revision{}, nil
		}

		return comp.Revision, nil
	}

	var vcerrs map[string]*ValidationSetsComponentValidationError
	for _, sc := range v.snaps {
		// if we're ignoring the snap, then we don't consider its components
		//
		// if the snap is not installed, then nothing can be wrong with the
		// components, since none will be installed. however, for error
		// reporting reasons, we consider components for snaps that are required
		// by the validation sets.
		//
		// note that we consider "required" components to only be required if
		// the snap itself is installed.
		if ignore[sc.name] || (!installedSnaps.Contains(sc.snapRef) && !missingSnaps[sc.name]) {
			continue
		}

		componentConstraints := make([]constraints, 0, len(sc.componentConstraints))
		for _, cstrs := range sc.componentConstraints {
			componentConstraints = append(componentConstraints, cstrs.constraints)
		}

		invalid, missing, wrongrev, err := checkManyConstraints(componentConstraints, componentInstalled, nil)
		if err != nil {
			return nil, err
		}

		if len(invalid) > 0 || len(missing) > 0 || len(wrongrev) > 0 {
			if vcerrs == nil {
				vcerrs = make(map[string]*ValidationSetsComponentValidationError)
			}
			vcerrs[sc.name] = &ValidationSetsComponentValidationError{
				InvalidComponents:       invalid,
				MissingComponents:       missing,
				WrongRevisionComponents: wrongrev,
			}
		}
	}
	return vcerrs, nil
}

// checkManyConstraints checks the given constraints against the installed
// revision of the target of the constraint. if installedRevision returns an
// unset revision, we consider the constraint target to be uninstalled.
func checkManyConstraints(scs []constraints, installedRevision func(constraints) (snap.Revision, error), ignore map[string]bool) (
	// name of constrained -> invalid validation set keys
	invalid map[string][]string,
	// name of constrained -> required revision (could be unspecified) -> invalid validation set keys
	missing map[string]map[snap.Revision][]string,
	// name of constrained -> required revision (should never be unspecified) -> invalid validation set keys
	wrongrev map[string]map[snap.Revision][]string,
	err error,
) {
	if ignore == nil {
		ignore = make(map[string]bool)
	}

	for _, cstrs := range scs {
		installedRev, err := installedRevision(cstrs)
		if err != nil {
			return nil, nil, nil, err
		}

		isInstalled := !installedRev.Unset()
		if isInstalled && ignore[cstrs.name] {
			continue
		}

		// TODO: good names here?
		i, m, w, err := checkConstraints(cstrs, installedRev)
		if err != nil {
			return nil, nil, nil, err
		}

		if len(i) > 0 {
			if invalid == nil {
				invalid = make(map[string][]string)
			}
			invalid[cstrs.name] = i
		}
		if len(m) > 0 {
			if missing == nil {
				missing = make(map[string]map[snap.Revision][]string)
			}
			missing[cstrs.name] = m
		}
		if len(w) > 0 {
			if wrongrev == nil {
				wrongrev = make(map[string]map[snap.Revision][]string)
			}
			wrongrev[cstrs.name] = w
		}
	}
	return invalid, missing, wrongrev, nil
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

func (v *ValidationSets) constraintsForSnap(snapRef naming.SnapRef) *snapConstraints {
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

// CanBePresent returns true if a snap can be present in a situation in which
// these validation sets are being applied.
func (v *ValidationSets) CanBePresent(snapRef naming.SnapRef) bool {
	cstrs := v.constraintsForSnap(snapRef)
	if cstrs == nil {
		return true
	}
	return cstrs.presence != asserts.PresenceInvalid
}

// RequiredSnaps returns a list of the names of all of the snaps that are
// required by any validation set known to this ValidationSets.
func (v *ValidationSets) RequiredSnaps() []string {
	var names []string
	for _, sn := range v.snaps {
		if sn.presence == asserts.PresenceRequired {
			names = append(names, sn.name)
		}
	}
	return names
}

// SnapConstrained returns true if the given snap is constrained by any of the
// validation sets known to this ValidationSets.
func (v *ValidationSets) SnapConstrained(snapRef naming.SnapRef) bool {
	return v.constraintsForSnap(snapRef) != nil
}

// PresenceConstraint represents the allowed presence of a snap or component with
// respect to a set of validation sets that it was derived from.
type PresenceConstraint struct {
	// Presence is the required presence of the snap or component.
	Presence asserts.Presence
	// Revision is the revision that the snap or component must be at if the
	// presence is not invalid.
	Revision snap.Revision
	// Sets is a list of validation sets that the presence is derived from.
	Sets ValidationSetKeySlice
}

// SnapPresenceConstraints contains information about a snap's allowed presence with
// respect to a set of validation sets.
type SnapPresenceConstraints struct {
	PresenceConstraint
	components map[string]PresenceConstraint
}

// Constrained returns true if the snap is constrained in any way by the
// validation sets that this SnapPresence is created from.
// Ultimately, one of these things must be true for a snap to be constrained:
//   - snap has a presence of either "required" or "invalid"
//   - the snap's revision is pinned to a specific revision
//   - either of the above are true for any of the snap's components
func (s *SnapPresenceConstraints) Constrained() bool {
	if s.constrained() {
		return true
	}

	for _, comp := range s.components {
		if comp.constrained() {
			return true
		}
	}
	return false
}

func (p *PresenceConstraint) constrained() bool {
	return p.Presence != asserts.PresenceOptional || !p.Revision.Unset()
}

// Component returns the presence of the given component of the snap. If this
// SnapPresence doesn't know about the component, the component will be
// considered optional and allowed to have any revision.
func (s *SnapPresenceConstraints) Component(name string) PresenceConstraint {
	if s.components == nil {
		return PresenceConstraint{
			Presence: asserts.PresenceOptional,
		}
	}

	cp, ok := s.components[name]
	if !ok {
		return PresenceConstraint{
			Presence: asserts.PresenceOptional,
		}
	}
	return cp
}

// RequiredComponents returns a set of all of the components that are required
// to be installed when this snap is installed.
func (s *SnapPresenceConstraints) RequiredComponents() map[string]PresenceConstraint {
	required := make(map[string]PresenceConstraint)
	for name, pres := range s.components {
		if pres.Presence != asserts.PresenceRequired {
			continue
		}
		required[name] = pres
	}
	return required
}

// Presence returns a SnapPresence for the given snap. The returned struct
// contains information about the allowed presence of the snap, with respect to
// the validation sets that are known to this ValidationSets. If the snap is not
// constrained by any validation sets, the presence will be considered optional.
//
// Note that this method assumes that the validation sets are not in conflict.
// Check with ValidationSets.Conflict() before calling this method.
func (v *ValidationSets) Presence(sn naming.SnapRef) (SnapPresenceConstraints, error) {
	// if this is true, then calling code has a bug
	if snapName := sn.SnapName(); strings.Contains(snapName, "_") {
		return SnapPresenceConstraints{}, fmt.Errorf("internal error: cannot check snap against validation sets with instance name: %q", snapName)
	}

	cstrs := v.constraintsForSnap(sn)
	if cstrs == nil {
		return SnapPresenceConstraints{
			PresenceConstraint: PresenceConstraint{Presence: asserts.PresenceOptional},
		}, nil
	}

	snapPresence, err := presenceFromConstraints(cstrs.constraints, v.sets)
	if err != nil {
		return SnapPresenceConstraints{}, err
	}

	comps := make(map[string]PresenceConstraint, len(cstrs.componentConstraints))
	for _, cstrs := range cstrs.componentConstraints {
		compPresence, err := presenceFromConstraints(cstrs.constraints, v.sets)
		if err != nil {
			return SnapPresenceConstraints{}, err
		}
		comps[cstrs.name] = compPresence
	}

	return SnapPresenceConstraints{
		PresenceConstraint: snapPresence,
		components:         comps,
	}, nil
}

func presenceFromConstraints(cstrs constraints, vsets map[string]*asserts.ValidationSet) (PresenceConstraint, error) {
	p := PresenceConstraint{
		Presence: asserts.PresenceOptional,
	}
	for rev, revCstr := range cstrs.revisions {
		for _, rc := range revCstr {
			vs := vsets[rc.validationSetKey]
			if vs == nil {
				return PresenceConstraint{}, fmt.Errorf("internal error: no validation set for %q", rc.validationSetKey)
			}

			p.Sets = append(p.Sets, NewValidationSetKey(vs))

			if p.Revision == unspecifiedRevision {
				p.Revision = rev
			}

			if p.Presence == asserts.PresenceOptional {
				p.Presence = rc.presence
			}
		}
	}

	sort.Sort(ValidationSetKeySlice(p.Sets))

	return p, nil
}

// ParseValidationSet parses a validation set string (account/name or account/name=sequence)
// and returns its individual components, or an error.
func ParseValidationSet(arg string) (account, name string, seq int, err error) {
	errPrefix := func() string {
		return fmt.Sprintf("cannot parse validation set %q", arg)
	}
	parts := strings.Split(arg, "=")
	if len(parts) > 2 {
		return "", "", 0, fmt.Errorf("%s: expected account/name=seq", errPrefix())
	}
	if len(parts) == 2 {
		seq, err = strconv.Atoi(parts[1])
		if err != nil {
			return "", "", 0, fmt.Errorf("%s: invalid sequence: %v", errPrefix(), err)
		}
	}

	parts = strings.Split(parts[0], "/")
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("%s: expected a single account/name", errPrefix())
	}

	account = parts[0]
	name = parts[1]
	if !asserts.IsValidAccountID(account) {
		return "", "", 0, fmt.Errorf("%s: invalid account ID %q", errPrefix(), account)
	}
	if !asserts.IsValidValidationSetName(name) {
		return "", "", 0, fmt.Errorf("%s: invalid validation set name %q", errPrefix(), name)
	}

	return account, name, seq, nil
}

type installedSnapSet struct {
	snaps *naming.SnapSet
}

func newInstalledSnapSet(snaps []*InstalledSnap) installedSnapSet {
	set := naming.NewSnapSet(nil)
	for _, sn := range snaps {
		set.Add(sn)
	}
	return installedSnapSet{snaps: set}
}

func (s *installedSnapSet) Lookup(ref naming.SnapRef) *InstalledSnap {
	found := s.snaps.Lookup(ref)
	if found == nil {
		return nil
	}
	return found.(*InstalledSnap)
}

func (s *installedSnapSet) Contains(ref naming.SnapRef) bool {
	return s.snaps.Contains(ref)
}

func (s *installedSnapSet) LookupComponent(snapRef naming.SnapRef, compRef naming.ComponentRef) *InstalledComponent {
	snap := s.Lookup(snapRef)
	if snap == nil {
		return nil
	}
	for _, comp := range snap.Components {
		if comp.ComponentRef == compRef {
			return &comp
		}
	}
	return nil
}
