// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package snapstate

import (
	"fmt"
	"reflect"

	"github.com/snapcore/snapd/overlord/state"
)

// FinalTasks are task kinds for final tasks in a change which means no further
// change work should be performed afterward, usually these are tasks that
// commit a full system transition.
var FinalTasks = []string{"mark-seeded", "set-model"}

// ChangeConflictError represents an error because of snap conflicts between changes.
type ChangeConflictError struct {
	Snap       string
	ChangeKind string
	// a Message is optional, otherwise one is composed from the other information
	Message string
	// ChangeID can optionally be set to the ID of the change with which the operation conflicts
	ChangeID string
}

func (e *ChangeConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.ChangeKind != "" {
		return fmt.Sprintf("snap %q has %q change in progress", e.Snap, e.ChangeKind)
	}
	return fmt.Sprintf("snap %q has changes in progress", e.Snap)
}

// An AffectedSnapsFunc returns a list of affected snap names for the given supported task.
type AffectedSnapsFunc func(*state.Task) ([]string, error)

var (
	affectedSnapsByAttr = make(map[string]AffectedSnapsFunc)
	affectedSnapsByKind = make(map[string]AffectedSnapsFunc)
)

// AddAffectedSnapsByAttr registers an AffectedSnapsFunc for returning the affected snaps for tasks sporting the given identifying attribute, to use in conflicts detection.
func AddAffectedSnapsByAttr(attr string, f AffectedSnapsFunc) {
	affectedSnapsByAttr[attr] = f
}

// AddAffectedSnapsByKind registers an AffectedSnapsFunc for returning the affected snaps for tasks of the given kind, to use in conflicts detection. Whenever possible using AddAffectedSnapsByAttr should be preferred.
func AddAffectedSnapsByKind(kind string, f AffectedSnapsFunc) {
	affectedSnapsByKind[kind] = f
}

func affectedSnaps(t *state.Task) ([]string, error) {
	// snapstate's own styled tasks
	if t.Has("snap-setup") || t.Has("snap-setup-task") {
		snapsup, err := TaskSnapSetup(t)
		if err != nil {
			return nil, fmt.Errorf("internal error: cannot obtain snap setup from task: %s", t.Summary())
		}
		return []string{snapsup.InstanceName()}, nil
	}

	if f := affectedSnapsByKind[t.Kind()]; f != nil {
		return f(t)
	}

	for attrKey, f := range affectedSnapsByAttr {
		if t.Has(attrKey) {
			return f(t)
		}
	}

	return nil, nil
}

func checkChangeConflictExclusiveKinds(st *state.State, newExclusiveChangeKind, ignoreChangeID string) error {
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		switch chg.Kind() {
		case "transition-ubuntu-core":
			return &ChangeConflictError{
				Message:    "ubuntu-core to core transition in progress, no other changes allowed until this is done",
				ChangeKind: "transition-ubuntu-core",
				ChangeID:   chg.ID(),
			}
		case "transition-to-snapd-snap":
			return &ChangeConflictError{
				Message:    "transition to snapd snap in progress, no other changes allowed until this is done",
				ChangeKind: "transition-to-snapd-snap",
				ChangeID:   chg.ID(),
			}
		case "remodel":
			if ignoreChangeID != "" && chg.ID() == ignoreChangeID {
				continue
			}
			return &ChangeConflictError{
				Message:    "remodeling in progress, no other changes allowed until this is done",
				ChangeKind: "remodel",
				ChangeID:   chg.ID(),
			}
		case "create-recovery-system":
			if ignoreChangeID != "" && chg.ID() == ignoreChangeID {
				continue
			}
			return &ChangeConflictError{
				Message:    "creating recovery system in progress, no other changes allowed until this is done",
				ChangeKind: "create-recovery-system",
				ChangeID:   chg.ID(),
			}
		default:
			if newExclusiveChangeKind != "" {
				// we want to run a new exclusive change, but other
				// changes are in progress already
				msg := fmt.Sprintf("other changes in progress (conflicting change %q), change %q not allowed until they are done", chg.Kind(),
					newExclusiveChangeKind)
				return &ChangeConflictError{
					Message:    msg,
					ChangeKind: chg.Kind(),
					ChangeID:   chg.ID(),
				}
			}
		}
	}
	return nil
}

// CheckChangeConflictRunExclusively checks for conflicts with a new change which
// must be run when no other changes are running.
func CheckChangeConflictRunExclusively(st *state.State, newChangeKind string) error {
	return checkChangeConflictExclusiveKinds(st, newChangeKind, "")
}

// CheckChangeConflictMany ensures that for the given instanceNames no other
// changes that alters the snaps (like remove, install, refresh) are in
// progress. If a conflict is detected an error is returned.
//
// It's like CheckChangeConflict, but for multiple snaps, and does not
// check snapst.
func CheckChangeConflictMany(st *state.State, instanceNames []string, ignoreChangeID string) error {
	snapMap := make(map[string]bool, len(instanceNames))
	for _, k := range instanceNames {
		snapMap[k] = true
	}

	// check whether there are other changes that need to run exclusively
	if err := checkChangeConflictExclusiveKinds(st, "", ignoreChangeID); err != nil {
		return err
	}

	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg == nil || chg.Status().Ready() {
			continue
		}
		if ignoreChangeID != "" && chg.ID() == ignoreChangeID {
			continue
		}
		if chg.Kind() == "become-operational" {
			// become-operational will be retried until success
			// and on its own just runs a hook on gadget:
			// do not make it interfere with user requests
			// TODO: consider a use vs change modeling of
			// conflicts
			continue
		}

		snaps, err := affectedSnaps(task)
		if err != nil {
			return err
		}

		for _, snap := range snaps {
			if snapMap[snap] {
				return &ChangeConflictError{
					Snap:       snap,
					ChangeKind: chg.Kind(),
					ChangeID:   chg.ID(),
				}
			}
		}
	}

	return nil
}

// CheckChangeConflict ensures that for the given instanceName no other
// changes that alters the snap (like remove, install, refresh) are in
// progress. It also ensures that snapst (if not nil) did not get
// modified. If a conflict is detected an error is returned.
func CheckChangeConflict(st *state.State, instanceName string, snapst *SnapState) error {
	return checkChangeConflictIgnoringOneChange(st, instanceName, snapst, "")
}

func checkChangeConflictIgnoringOneChange(st *state.State, instanceName string, snapst *SnapState, ignoreChangeID string) error {
	if err := CheckChangeConflictMany(st, []string{instanceName}, ignoreChangeID); err != nil {
		return err
	}

	if snapst != nil {
		// caller wants us to also make sure the SnapState in state
		// matches the one they provided. Necessary because we need to
		// unlock while talking to the store, during which a change can
		// sneak in (if it's before the taskset is created) (e.g. for
		// install, while getting the snap info; for refresh, when
		// getting what needs refreshing).
		var cursnapst SnapState
		if err := Get(st, instanceName, &cursnapst); err != nil && err != state.ErrNoState {
			return err
		}

		// TODO: implement the rather-boring-but-more-performant SnapState.Equals
		if !reflect.DeepEqual(snapst, &cursnapst) {
			return &ChangeConflictError{Snap: instanceName}
		}
	}

	return nil
}
