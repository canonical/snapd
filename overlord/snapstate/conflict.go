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
	"errors"
	"fmt"
	"reflect"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
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

func (e *ChangeConflictError) Is(err error) bool {
	_, ok := err.(*ChangeConflictError)
	return ok
}

// An AffectedSnapsFunc returns a list of affected snap names for the given supported task.
type AffectedSnapsFunc func(*state.Task) ([]string, error)

var (
	affectedSnapsByAttr = make(map[string]AffectedSnapsFunc)
	affectedSnapsByKind = make(map[string]AffectedSnapsFunc)
)

// RegisterAffectedSnapsByAttr registers an AffectedSnapsFunc for returning the affected snaps for tasks sporting the given identifying attribute, to use in conflicts detection.
func RegisterAffectedSnapsByAttr(attr string, f AffectedSnapsFunc) {
	affectedSnapsByAttr[attr] = f
}

// RegisterAffectedSnapsByKind registers an AffectedSnapsFunc for returning the affected snaps for tasks of the given kind, to use in conflicts detection. Whenever possible using RegisterAffectedSnapsByAttr should be preferred.
func RegisterAffectedSnapsByKind(kind string, f AffectedSnapsFunc) {
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

func snapSetupFromChange(chg *state.Change) (*SnapSetup, error) {
	for _, t := range chg.Tasks() {
		// Check a known task of each change that we know keep snap info.
		if t.Kind() != "prepare-snap" && t.Kind() != "download-snap" {
			continue
		}
		return TaskSnapSetup(t)
	}
	return nil, nil
}

// changeIsSnapdDowngrade returns true if the change provided is a snapd
// setup change with a version lower than what is currently installed. If a change
// is not SnapSetup related this returns false.
func changeIsSnapdDowngrade(st *state.State, chg *state.Change) (bool, error) {
	snapsup, err := snapSetupFromChange(chg)
	if err != nil {
		return false, err
	}
	if snapsup == nil || snapsup.SnapName() != "snapd" {
		return false, nil
	}

	var snapst SnapState
	if err := Get(st, snapsup.InstanceName(), &snapst); err != nil {
		return false, err
	}

	currentInfo, err := snapst.CurrentInfo()
	if err != nil {
		return false, fmt.Errorf("cannot retrieve snap info for current snapd: %v", err)
	}

	// On older snapd's 'Version' might be empty, and in this case we assume
	// that snapd is downgrading as we cannot determine otherwise.
	if snapsup.Version == "" {
		return true, nil
	}
	res, err := strutil.VersionCompare(currentInfo.Version, snapsup.Version)
	if err != nil {
		return false, fmt.Errorf("cannot compare versions of snapd [cur: %s, new: %s]: %v", currentInfo.Version, snapsup.Version, err)
	}
	return res == 1, nil
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
		case "revert-snap", "refresh-snap":
			// Snapd downgrades are exclusive changes
			if ignoreChangeID != "" && chg.ID() == ignoreChangeID {
				continue
			}
			if downgrading, err := changeIsSnapdDowngrade(st, chg); err != nil {
				return err
			} else if !downgrading {
				continue
			}
			return &ChangeConflictError{
				Message:    "snapd downgrade in progress, no other changes allowed until this is done",
				ChangeKind: chg.Kind(),
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

// isIrrelevantChange checks if a change is ready or it can be ignored
// if matching the passed ID, for conflict checking purposes.
func isIrrelevantChange(chg *state.Change, ignoreChangeID string) bool {
	if chg == nil || chg.IsReady() {
		return true
	}
	if ignoreChangeID != "" && chg.ID() == ignoreChangeID {
		return true
	}
	switch chg.Kind() {
	case "pre-download":
		// pre-download changes only have pre-download tasks
		// which don't generate conflicts because they only
		// download the snap and download tasks check for them
		// explicitly
		fallthrough
	case "become-operational":
		// become-operational will be retried until success
		// and on its own just runs a hook on gadget:
		// do not make it interfere with user requests
		// TODO: consider a use vs change modeling of
		// conflicts
		return true
	}

	return false
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
		if isIrrelevantChange(chg, ignoreChangeID) {
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
		if err := Get(st, instanceName, &cursnapst); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		// TODO: implement the rather-boring-but-more-performant SnapState.Equals
		if !reflect.DeepEqual(snapst, &cursnapst) {
			return &ChangeConflictError{Snap: instanceName}
		}
	}

	return nil
}

// CheckUpdateKernelCommandLineConflict checks that no active change other
// than ignoreChangeID has a task that touches the kernel command
// line.
func CheckUpdateKernelCommandLineConflict(st *state.State, ignoreChangeID string) error {
	// check whether there are other changes that need to run exclusively
	if err := checkChangeConflictExclusiveKinds(st, "", ignoreChangeID); err != nil {
		return err
	}

	for _, task := range st.Tasks() {
		chg := task.Change()
		if isIrrelevantChange(chg, ignoreChangeID) {
			continue
		}

		switch task.Kind() {
		case "update-gadget-cmdline":
			return &ChangeConflictError{
				Message:    "kernel command line already being updated, no additional changes for it allowed meanwhile",
				ChangeKind: task.Kind(),
				ChangeID:   chg.ID(),
			}
		case "update-managed-boot-config":
			return &ChangeConflictError{
				Message:    "boot config is being updated, no change in kernel commnd line is allowed meanwhile",
				ChangeKind: task.Kind(),
				ChangeID:   chg.ID(),
			}
		}
	}

	return nil
}
