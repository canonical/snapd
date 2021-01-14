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

package assertstate

import (
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

// ValidationSetMode reflects the mode of respective validation set, which is
// either monitoring or enforcing.
type ValidationSetMode int

const (
	Monitor ValidationSetMode = iota
	Enforce
)

// ValidationSetTracking holds tracking parameters for associated validation set.
type ValidationSetTracking struct {
	AccountID string            `json:"account-id"`
	Name      string            `json:"name"`
	Mode      ValidationSetMode `json:"mode"`

	// PinnedAt is an optional pinned sequence point, or 0 if not pinned.
	PinnedAt int `json:"pinned-at,omitempty"`

	// Current is the current sequence point.
	Current int `json:"current,omitempty"`
}

// ValidationSetKey formats the given account id and name into a validation set key.
func ValidationSetKey(accountID, name string) string {
	return fmt.Sprintf("%s/%s", accountID, name)
}

// UpdateValidationSet updates ValidationSetTracking.
// The method assumes valid tr fields.
func UpdateValidationSet(st *state.State, tr *ValidationSetTracking) {
	var vsmap map[string]*json.RawMessage
	err := st.Get("validation-sets", &vsmap)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal validation set tracking state: " + err.Error())
	}
	if vsmap == nil {
		vsmap = make(map[string]*json.RawMessage)
	}
	data, err := json.Marshal(tr)
	if err != nil {
		panic("internal error: cannot marshal validation set tracking state: " + err.Error())
	}
	raw := json.RawMessage(data)
	key := ValidationSetKey(tr.AccountID, tr.Name)
	vsmap[key] = &raw
	st.Set("validation-sets", vsmap)
}

// DeleteValidationSet deletes a validation set for the given accoundID and name.
// It is not an error to delete a non-existing one.
func DeleteValidationSet(st *state.State, accountID, name string) {
	var vsmap map[string]*json.RawMessage
	err := st.Get("validation-sets", &vsmap)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal validation set tracking state: " + err.Error())
	}
	if len(vsmap) == 0 {
		return
	}
	delete(vsmap, ValidationSetKey(accountID, name))
	st.Set("validation-sets", vsmap)
	return
}

// GetValidationSet retrieves the ValidationSetTracking for the given account and name.
func GetValidationSet(st *state.State, accountID, name string, tr *ValidationSetTracking) error {
	if tr == nil {
		return fmt.Errorf("internal error: tr is nil")
	}

	*tr = ValidationSetTracking{}

	var vset map[string]*json.RawMessage
	err := st.Get("validation-sets", &vset)
	if err != nil {
		return err
	}
	key := ValidationSetKey(accountID, name)
	raw, ok := vset[key]
	if !ok {
		return state.ErrNoState
	}
	// XXX: &tr pointer isn't needed here but it is likely historical (a bug in
	// old JSON marshaling probably) and carried over from snapstate.Get.
	err = json.Unmarshal([]byte(*raw), &tr)
	if err != nil {
		return fmt.Errorf("cannot unmarshal validation set tracking state: %v", err)
	}
	return nil
}

// ValidationSets retrieves all ValidationSetTracking data.
func ValidationSets(st *state.State) (map[string]*ValidationSetTracking, error) {
	var vsmap map[string]*ValidationSetTracking
	if err := st.Get("validation-sets", &vsmap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	return vsmap, nil
}
