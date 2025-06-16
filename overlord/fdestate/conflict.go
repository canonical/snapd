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
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

type ChangeConflictError struct {
	KeyslotRef KeyslotRef
	ChangeKind string
}

func (e *ChangeConflictError) Error() string {
	if e.ChangeKind != "" {
		return fmt.Sprintf("key slot %s has %q change in progress", e.KeyslotRef.String(), e.ChangeKind)
	}
	return fmt.Sprintf("key slot %s has changes in progress", e.KeyslotRef.String())
}

func keyslotsAffectedByTask(t *state.Task) ([]KeyslotRef, error) {
	if !t.Has("keyslots") {
		return nil, nil
	}

	var keyslotRefs []KeyslotRef
	if err := t.Get("keyslots", &keyslotRefs); err != nil {
		return nil, err
	}
	return keyslotRefs, nil
}

func checkChangeConflict(st *state.State, keyslotRefs []KeyslotRef) error {
	refMap := make(map[KeyslotRef]bool, len(keyslotRefs))
	for _, ref := range keyslotRefs {
		refMap[ref] = true
	}

	for _, task := range st.Tasks() {
		if task.Status().Ready() {
			continue
		}

		refs, err := keyslotsAffectedByTask(task)
		if err != nil {
			return err
		}

		for _, ref := range refs {
			if refMap[ref] {
				return &ChangeConflictError{
					KeyslotRef: ref,
					ChangeKind: task.Change().Kind(),
				}
			}
		}
	}

	return nil
}
