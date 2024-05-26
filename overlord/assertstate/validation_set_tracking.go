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
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
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

// Sequence returns the sequence number of the currently used validation set.
func (vs *ValidationSetTracking) Sequence() int {
	// Current was occasionally set to the latest sequence number even when Pinned != 0,
	// this should no longer happen but return PinnedAt anyway to be safe
	if vs.PinnedAt > 0 {
		return vs.PinnedAt
	}

	return vs.Current
}

// ValidationSetKey formats the given account id and name into a validation set key.
func ValidationSetKey(accountID, name string) string {
	return fmt.Sprintf("%s/%s", accountID, name)
}

// UpdateValidationSet updates ValidationSetTracking.
// The method assumes valid tr fields.
func UpdateValidationSet(st *state.State, tr *ValidationSetTracking) {
	var vsmap map[string]*json.RawMessage
	mylog.Check(st.Get("validation-sets", &vsmap))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		panic("internal error: cannot unmarshal validation set tracking state: " + err.Error())
	}
	if vsmap == nil {
		vsmap = make(map[string]*json.RawMessage)
	}
	data := mylog.Check2(json.Marshal(tr))

	raw := json.RawMessage(data)
	key := ValidationSetKey(tr.AccountID, tr.Name)
	vsmap[key] = &raw
	st.Set("validation-sets", vsmap)
}

// verifyForgetAllowedByModelAssertion checks whether a validation-set is controlled by
// the model assertion. If the validation-set is set to 'enforce', then it's not possible
// to forget it.
func verifyForgetAllowedByModelAssertion(st *state.State, accountID, name string) error {
	vs := mylog.Check2(validationSetFromModel(st, accountID, name))

	if vs == nil {
		return nil
	}
	if vs.Mode == asserts.ModelValidationSetModeEnforced {
		return fmt.Errorf("validation-set is enforced by the model")
	}
	return nil
}

// ForgetValidationSetOpts holds options for ForgetValidationSet.
type ForgetValidationSetOpts struct {
	// ForceForget is used to forget a validation set even if it's enforced by
	// the model. This is currently used during remodeling when we need to
	// forget validation sets from the old/new model.
	ForceForget bool
}

// ForgetValidationSet deletes a validation set for the given accountID and name.
// It is not an error to delete a non-existing one. If the validation-set
// is controlled by the model assertion it may not be allowed to forget it.
func ForgetValidationSet(st *state.State, accountID, name string, opts ForgetValidationSetOpts) error {
	var vsmap map[string]*json.RawMessage
	mylog.Check(st.Get("validation-sets", &vsmap))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		panic("internal error: cannot unmarshal validation set tracking state: " + err.Error())
	}
	if len(vsmap) == 0 {
		return nil
	}

	if !opts.ForceForget {
		mylog.Check(verifyForgetAllowedByModelAssertion(st, accountID, name))
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
	mylog.Check(st.Get("validation-sets", &vset))

	key := ValidationSetKey(accountID, name)
	raw, ok := vset[key]
	if !ok {
		return state.ErrNoState
	}
	mylog.
		// XXX: &tr pointer isn't needed here but it is likely historical (a bug in
		// old JSON marshaling probably) and carried over from snapstate.Get.
		Check(json.Unmarshal([]byte(*raw), &tr))

	return nil
}

// ValidationSets retrieves all ValidationSetTracking data.
func ValidationSets(st *state.State) (map[string]*ValidationSetTracking, error) {
	var vsmap map[string]*ValidationSetTracking
	if mylog.Check(st.Get("validation-sets", &vsmap)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	return vsmap, nil
}

// TrackedEnforcedValidationSets returns ValidationSets object with all currently tracked
// validation sets that are in enforcing mode. If extraVss is not nil then they are
// added to the returned set and replaces validation sets with same account/name
// in case they were tracked already.
func TrackedEnforcedValidationSets(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
	sets := snapasserts.NewValidationSets()

	setsToSkip := make(map[string]bool, len(extraVss))
	for _, extraVs := range extraVss {
		sets.Add(extraVs)
		setsToSkip[ValidationSetKey(extraVs.AccountID(), extraVs.Name())] = true
	}

	skip := func(key string) bool {
		// if extraVs matches an already enforced validation set, then skip that one, extraVs has been added
		// before the loop.
		return setsToSkip[key]
	}
	mylog.Check(trackedEnforcedValidationSets(st, skip, sets))

	return sets, nil
}

// TrackedEnforcedValidationSetsForModel returns a ValidationSets object for
// currently tracked validation sets that are in enforcing mode and also
// associated with the specified model.
func TrackedEnforcedValidationSetsForModel(st *state.State, model *asserts.Model) (*snapasserts.ValidationSets, error) {
	modelSets := make(map[string]bool, len(model.ValidationSets()))
	for _, vs := range model.ValidationSets() {
		modelSets[ValidationSetKey(vs.AccountID, vs.Name)] = true
	}

	skip := func(key string) bool {
		return !modelSets[key]
	}

	sets := snapasserts.NewValidationSets()
	mylog.Check(trackedEnforcedValidationSets(st, skip, sets))

	return sets, nil
}

func trackedEnforcedValidationSets(st *state.State, skip func(string) bool, sets *snapasserts.ValidationSets) error {
	valsets := mylog.Check2(ValidationSets(st))

	db := DB(st)

	for _, vs := range valsets {
		if vs.Mode != Enforce {
			continue
		}

		if skip(ValidationSetKey(vs.AccountID, vs.Name)) {
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

		as := mylog.Check2(db.Find(asserts.ValidationSetType, headers))

		vsetAssert := as.(*asserts.ValidationSet)
		mylog.Check(sets.Add(vsetAssert))

	}

	return err
}

// addCurrentTrackingToValidationSetsHistory stores the current state of validation-sets
// tracking on top of the validation sets history.
func addCurrentTrackingToValidationSetsHistory(st *state.State) error {
	current := mylog.Check2(ValidationSets(st))

	return addToValidationSetsHistory(st, current)
}

func addToValidationSetsHistory(st *state.State, validationSets map[string]*ValidationSetTracking) error {
	vshist := mylog.Check2(ValidationSetsHistory(st))

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
	if mylog.Check(st.Get("validation-sets-history", &vshist)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if len(vshist) == 0 {
		return nil, nil
	}
	// decode just the topmost entry
	raw := vshist[len(vshist)-1]
	var top map[string]*ValidationSetTracking
	mylog.Check(json.Unmarshal([]byte(*raw), &top))

	return top, nil
}

// ValidationSetsHistory returns the complete history of validation sets tracking.
func ValidationSetsHistory(st *state.State) ([]map[string]*ValidationSetTracking, error) {
	var vshist []map[string]*ValidationSetTracking
	if mylog.Check(st.Get("validation-sets-history", &vshist)); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	return vshist, nil
}

// RestoreValidationSetsTracking restores validation-sets state to the last state
// stored in the validation-sets-stack. It should only be called when the stack
// is not empty, otherwise an error is returned.
func RestoreValidationSetsTracking(st *state.State) error {
	trackingState := mylog.Check2(validationSetsHistoryTop(st))

	if len(trackingState) == 0 {
		// we should never be called when there is nothing in the stack
		return state.ErrNoState
	}
	st.Set("validation-sets", trackingState)
	return nil
}
