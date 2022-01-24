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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

// maximum number of entries kept in validation-sets-history in the state
var maxValidationSetsHistorySize = 50

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

	// LocalOnly indicates that the assertion was only available locally at the
	// time it was applied for monitor mode. This tells bulk refresh logic not
	// to error out on such assertion if it's not in the store.
	// This flag makes sense only in monitor mode and if pinned.
	LocalOnly bool `json:"local-only,omitempty"`
}

func (vs *ValidationSetTracking) sameAs(tr *ValidationSetTracking) bool {
	return vs.AccountID == tr.AccountID && vs.Current == tr.Current &&
		vs.LocalOnly == tr.LocalOnly && vs.Mode == tr.Mode &&
		vs.Name == tr.Name && vs.PinnedAt == tr.PinnedAt
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

// ForgetValidationSet deletes a validation set for the given accountID and name.
// It is not an error to delete a non-existing one.
func ForgetValidationSet(st *state.State, accountID, name string) error {
	var vsmap map[string]*json.RawMessage
	err := st.Get("validation-sets", &vsmap)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal validation set tracking state: " + err.Error())
	}
	if len(vsmap) == 0 {
		return nil
	}
	delete(vsmap, ValidationSetKey(accountID, name))
	st.Set("validation-sets", vsmap)

	return addCurrentTrackingToValidationSetsHistory(st)
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

// EnforcedValidationSets returns ValidationSets object with all currently tracked
// validation sets that are in enforcing mode.
func EnforcedValidationSets(st *state.State) (*snapasserts.ValidationSets, error) {
	valsets, err := ValidationSets(st)
	if err != nil {
		return nil, err
	}

	db := DB(st)
	sets := snapasserts.NewValidationSets()

	for _, vs := range valsets {
		if vs.Mode != Enforce {
			continue
		}

		sequence := vs.Current
		if vs.PinnedAt > 0 {
			sequence = vs.PinnedAt
		}
		headers := map[string]string{
			"series":     release.Series,
			"account-id": vs.AccountID,
			"name":       vs.Name,
			"sequence":   fmt.Sprintf("%d", sequence),
		}

		as, err := db.Find(asserts.ValidationSetType, headers)
		if err != nil {
			return nil, err
		}

		vsetAssert := as.(*asserts.ValidationSet)
		sets.Add(vsetAssert)
	}

	return sets, err
}

// addCurrentTrackingToValidationSetsHistory stores the current state of validation-sets
// tracking on top of the validation sets history.
func addCurrentTrackingToValidationSetsHistory(st *state.State) error {
	current, err := ValidationSets(st)
	if err != nil {
		return err
	}
	return addToValidationSetsHistory(st, current)
}

func addToValidationSetsHistory(st *state.State, validationSets map[string]*ValidationSetTracking) error {
	vshist, err := ValidationSetsHistory(st)
	if err != nil {
		return err
	}

	// if nothing is being tracked and history is empty (meaning nothing was
	// tracked before), then don't store anything.
	// if nothing is being tracked but history is not empty, then we want to
	// store empty tracking - this means snap validate --forget was used and
	// we need to remember such empty state in the history.
	if len(validationSets) == 0 && len(vshist) == 0 {
		return nil
	}

	var matches bool
	if len(vshist) > 0 {
		// only add to the history if it's different than topmost entry
		top := vshist[len(vshist)-1]
		if len(top) == len(validationSets) {
			matches = true
			for vskey, vset := range validationSets {
				prev, ok := top[vskey]
				if !ok || !prev.sameAs(vset) {
					matches = false
					break
				}
			}
		}
	}
	if !matches {
		vshist = append(vshist, validationSets)
	}
	if len(vshist) > maxValidationSetsHistorySize {
		vshist = vshist[len(vshist)-maxValidationSetsHistorySize:]
	}
	st.Set("validation-sets-history", &vshist)
	return nil
}

// validationSetsHistoryTop returns the topmost validation sets tracking state from
// the validations sets tracking history.
func validationSetsHistoryTop(st *state.State) (map[string]*ValidationSetTracking, error) {
	var vshist []*json.RawMessage
	if err := st.Get("validation-sets-history", &vshist); err != nil && err != state.ErrNoState {
		return nil, err
	}
	if len(vshist) == 0 {
		return nil, nil
	}
	// decode just the topmost entry
	raw := vshist[len(vshist)-1]
	var top map[string]*ValidationSetTracking
	if err := json.Unmarshal([]byte(*raw), &top); err != nil {
		return nil, fmt.Errorf("cannot unmarshal validation set tracking state: %v", err)
	}
	return top, nil
}

// ValidationSetsHistory returns the complete history of validation sets tracking.
func ValidationSetsHistory(st *state.State) ([]map[string]*ValidationSetTracking, error) {
	var vshist []map[string]*ValidationSetTracking
	if err := st.Get("validation-sets-history", &vshist); err != nil && err != state.ErrNoState {
		return nil, err
	}
	return vshist, nil
}

// RestoreValidationSetsTracking restores validation-sets state to the last state
// stored in the validation-sets-stack. It should only be called when the stack
// is not empty, otherwise an error is returned.
func RestoreValidationSetsTracking(st *state.State) error {
	trackingState, err := validationSetsHistoryTop(st)
	if err != nil {
		return err
	}
	if len(trackingState) == 0 {
		// we should never be called when there is nothing in the stack
		return state.ErrNoState
	}
	st.Set("validation-sets", trackingState)
	return nil
}
