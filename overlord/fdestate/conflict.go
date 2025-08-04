// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
package fdestate

import (
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

func isFDETask(t *state.Task) bool {
	return strings.HasPrefix(t.Kind(), "fde-")
}

func checkFDEChangeConflict(st *state.State) error {
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		switch chg.Kind() {
		case "fde-efi-secureboot-db-update":
			return &snapstate.ChangeConflictError{
				Message:    "external EFI DBX update in progress, no other FDE changes allowed until this is done",
				ChangeKind: chg.Kind(),
				ChangeID:   chg.ID(),
			}
		case "fde-replace-recovery-key":
			return &snapstate.ChangeConflictError{
				Message:    "replacing recovery key in progress, no other FDE changes allowed until this is done",
				ChangeKind: chg.Kind(),
				ChangeID:   chg.ID(),
			}
		case "fde-change-passphrase":
			return &snapstate.ChangeConflictError{
				Message:    "changing passphrase in progress, no other FDE changes allowed until this is done",
				ChangeKind: chg.Kind(),
				ChangeID:   chg.ID(),
			}
		default:
			// try to catch changes/tasks that could have been missed
			// and log a warning.
			for _, t := range chg.Tasks() {
				if isFDETask(t) {
					logger.Noticef("internal error: unexpected FDE change found %q", chg.Kind())
					return &snapstate.ChangeConflictError{
						Message:    "FDE change in progress, no other FDE changes allowed until this is done",
						ChangeKind: chg.Kind(),
						ChangeID:   chg.ID(),
					}
				}
			}
		}
	}
	return nil
}

func dbxUpdateAffectedSnaps(t *state.Task) ([]string, error) {
	// TODO:FDEM: check if we have sealed keys at all

	// DBX updates cause a reseal, so any snaps which are either directly
	// measured or their content is measured during the boot will count as
	// affected

	// XXX this effectively blocks updates of gadget, kernel & base until the
	// change completes

	return fdeRelevantSnaps(t.State())
}

// checkDBXChangeConflicts check that there are no conflicting
// changes for DBX updates.
func checkDBXChangeConflicts(st *state.State) error {
	// TODO:FDEM: check if we have sealed keys at all

	snaps, err := fdeRelevantSnaps(st)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		return nil
	}

	// make sure that there are no other DBX changes in progress
	op, err := findFirstPendingExternalOperationByKind(st, "fde-efi-secureboot-db-update")
	if err != nil {
		return err
	}

	if op != nil {
		return &snapstate.ChangeConflictError{
			ChangeKind: "fde-efi-secureboot-db-update",
			Message:    "cannot start a new DBX update when conflicting actions are in progress",
		}
	}

	// make sure that there are no changes for the snaps that are relevant for
	// FDE
	return snapstate.CheckChangeConflictMany(st, snaps, "")
}

func addProtectedKeysAffectedSnaps(t *state.Task) ([]string, error) {
	// adding a TPM protected key requires populating the role parameters
	// in the FDE state (ensureParametersLoaded), those parameters could
	// be updated as a result of a reseal caused by a refresh of any snap
	// which is either directly measured or its content is measured during
	// the boot.

	// XXX this effectively blocks updates of gadget, kernel & base until the
	// change completes

	return fdeRelevantSnaps(t.State())
}

// checkFDEParametersChangeConflicts check that there are no conflicting
// changes affecting the FDE parameters.
func checkFDEParametersChangeConflicts(st *state.State) error {
	snaps, err := fdeRelevantSnaps(st)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		return nil
	}

	// XXX: make sure that there are no external DBX changes in progress?

	// make sure that there are no changes for the snaps that are relevant for
	// FDE
	return snapstate.CheckChangeConflictMany(st, snaps, "")
}
