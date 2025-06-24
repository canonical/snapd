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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

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
		default:
			for _, t := range chg.Tasks() {
				if t.Has("keyslots") {
					return &snapstate.ChangeConflictError{
						Message:    "key slot task in progress, no other FDE changes allowed until this is done",
						ChangeKind: chg.Kind(),
						ChangeID:   chg.ID(),
					}
				}
			}
		}
	}
	return nil
}
