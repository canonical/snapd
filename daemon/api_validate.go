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

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

var (
	validationSetsListCmd = &Command{
		Path:       "/v2/validation-sets",
		GET:        listValidationSets,
		ReadAccess: authenticatedAccess{},
	}

	validationSetsCmd = &Command{
		Path:        "/v2/validation-sets/{account}/{name}",
		GET:         getValidationSet,
		POST:        applyValidationSet,
		ReadAccess:  authenticatedAccess{},
		WriteAccess: authenticatedAccess{},
	}
)

type validationSetResult struct {
	AccountID string `json:"account-id"`
	Name      string `json:"name"`
	PinnedAt  int    `json:"pinned-at,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Sequence  int    `json:"sequence,omitempty"`
	Valid     bool   `json:"valid"`
	// TODO: attributes for Notes column
}

func modeString(mode assertstate.ValidationSetMode) (string, error) {
	switch mode {
	case assertstate.Monitor:
		return "monitor", nil
	case assertstate.Enforce:
		return "enforce", nil
	}
	return "", fmt.Errorf("internal error: unhandled mode %d", mode)
}

func validationSetNotFound(accountID, name string, sequence int) Response {
	v := map[string]interface{}{
		"account-id": accountID,
		"name":       name,
	}
	if sequence != 0 {
		v["sequence"] = sequence
	}
	return &apiError{
		Status:  404,
		Message: "validation set not found",
		Kind:    client.ErrorKindValidationSetNotFound,
		Value:   v,
	}
}

func listValidationSets(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	validationSets := mylog.Check2(assertstate.ValidationSets(st))

	names := make([]string, 0, len(validationSets))
	for k := range validationSets {
		names = append(names, k)
	}
	sort.Strings(names)

	snaps, _ := mylog.Check3(snapstate.InstalledSnaps(st))

	results := make([]validationSetResult, len(names))
	for i, vs := range names {
		tr := validationSets[vs]
		sets := mylog.Check2(validationSetsForTracking(st, tr))

		// do not pass ignore validation map, we don't want to ignore validation and show invalid ones.
		validErr := checkInstalledSnaps(sets, snaps, nil)
		modeStr := mylog.Check2(modeString(tr.Mode))

		results[i] = validationSetResult{
			AccountID: tr.AccountID,
			Name:      tr.Name,
			PinnedAt:  tr.PinnedAt,
			Mode:      modeStr,
			Sequence:  tr.Sequence(),
			Valid:     validErr == nil,
		}
	}

	return SyncResponse(results)
}

var checkInstalledSnaps = func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
	return vsets.CheckInstalledSnaps(snaps, ignoreValidation)
}

func validationSetResultFromTracking(st *state.State, tr *assertstate.ValidationSetTracking) (*validationSetResult, error) {
	modeStr := mylog.Check2(modeString(tr.Mode))

	sets := mylog.Check2(validationSetsForTracking(st, tr))

	snaps, _ := mylog.Check3(snapstate.InstalledSnaps(st))

	validErr := checkInstalledSnaps(sets, snaps, nil)
	return &validationSetResult{
		AccountID: tr.AccountID,
		Name:      tr.Name,
		PinnedAt:  tr.PinnedAt,
		Mode:      modeStr,
		Sequence:  tr.Sequence(),
		Valid:     validErr == nil,
	}, nil
}

func getValidationSet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	accountID := vars["account"]
	name := vars["name"]

	if !asserts.IsValidAccountID(accountID) {
		return BadRequest("invalid account ID %q", accountID)
	}
	if !asserts.IsValidValidationSetName(name) {
		return BadRequest("invalid name %q", name)
	}

	query := r.URL.Query()

	// sequence is optional
	sequenceStr := query.Get("sequence")
	var sequence int
	if sequenceStr != "" {

		sequence = mylog.Check2(strconv.Atoi(sequenceStr))

		if sequence < 0 {
			return BadRequest("invalid sequence argument: %d", sequence)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var tr assertstate.ValidationSetTracking
	mylog.Check(assertstate.GetValidationSet(st, accountID, name, &tr))
	if errors.Is(err, state.ErrNoState) || (err == nil && sequence != 0 && sequence != tr.PinnedAt) {
		// not available locally, try to find in the store.
		return validateAgainstStore(st, accountID, name, sequence, user)
	}

	// evaluate against installed snaps
	res := mylog.Check2(validationSetResultFromTracking(st, &tr))

	return SyncResponse(*res)
}

type validationSetApplyRequest struct {
	Action   string `json:"action"`
	Mode     string `json:"mode"`
	Sequence int    `json:"sequence,omitempty"`
}

func applyValidationSet(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	accountID := vars["account"]
	name := vars["name"]

	if !asserts.IsValidAccountID(accountID) {
		return BadRequest("invalid account ID %q", accountID)
	}
	if !asserts.IsValidValidationSetName(name) {
		return BadRequest("invalid name %q", name)
	}

	var req validationSetApplyRequest
	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&req))

	if decoder.More() {
		return BadRequest("extra content found in request body")
	}
	if req.Sequence < 0 {
		return BadRequest("invalid sequence argument: %d", req.Sequence)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch req.Action {
	case "forget":
		return forgetValidationSet(st, accountID, name, req.Sequence)
	case "apply":
		return updateValidationSet(st, accountID, name, req.Mode, req.Sequence, user)
	default:
		return BadRequest("unsupported action %q", req.Action)
	}
}

var (
	assertstateMonitorValidationSet               = assertstate.MonitorValidationSet
	assertstateFetchAndApplyEnforcedValidationSet = assertstate.FetchAndApplyEnforcedValidationSet
	assertstateTryEnforcedValidationSets          = assertstate.TryEnforcedValidationSets
)

// updateValidationSet handles snap validate --monitor and --enforce accountId/name[=sequence].
func updateValidationSet(st *state.State, accountID, name string, reqMode string, sequence int, user *auth.UserState) Response {
	var mode assertstate.ValidationSetMode
	switch reqMode {
	case "monitor":
		mode = assertstate.Monitor
	case "enforce":
		mode = assertstate.Enforce
	default:
		return BadRequest("invalid mode %q", reqMode)
	}

	userID := 0
	if user != nil {
		userID = user.ID
	}

	if mode == assertstate.Enforce {
		return enforceValidationSet(st, accountID, name, sequence, userID)
	}

	tr := mylog.Check2(assertstateMonitorValidationSet(st, accountID, name, sequence, userID))

	res := mylog.Check2(validationSetResultFromTracking(st, tr))

	return SyncResponse(*res)
}

// forgetValidationSet forgets the validation set.
// The state needs to be locked by the caller.
func forgetValidationSet(st *state.State, accountID, name string, sequence int) Response {
	// check if it exists first
	var tr assertstate.ValidationSetTracking
	mylog.Check(assertstate.GetValidationSet(st, accountID, name, &tr))
	if errors.Is(err, state.ErrNoState) || (err == nil && sequence != 0 && sequence != tr.PinnedAt) {
		return validationSetNotFound(accountID, name, sequence)
	}
	mylog.Check(assertstate.ForgetValidationSet(st, accountID, name, assertstate.ForgetValidationSetOpts{}))

	return SyncResponse(nil)
}

func validationSetsForTracking(st *state.State, tr *assertstate.ValidationSetTracking) (*snapasserts.ValidationSets, error) {
	as := mylog.Check2(validationSetAssertFromDb(st, tr.AccountID, tr.Name, tr.Sequence()))

	sets := snapasserts.NewValidationSets()
	mylog.Check(sets.Add(as))

	return sets, nil
}

func validationSetAssertFromDb(st *state.State, accountID, name string, sequence int) (*asserts.ValidationSet, error) {
	headers := map[string]string{
		"series":     release.Series,
		"account-id": accountID,
		"name":       name,
		"sequence":   fmt.Sprintf("%d", sequence),
	}
	db := assertstate.DB(st)
	as := mylog.Check2(db.Find(asserts.ValidationSetType, headers))

	vset := as.(*asserts.ValidationSet)
	return vset, nil
}

func validateAgainstStore(st *state.State, accountID, name string, sequence int, user *auth.UserState) Response {
	// get from the store
	as := mylog.Check2(getSingleSeqFormingAssertion(st, accountID, name, sequence, user))
	if _, ok := err.(*asserts.NotFoundError); ok {
		// not in the store - try to find in the database
		as = mylog.Check2(validationSetAssertFromDb(st, accountID, name, sequence))
		if _, ok := err.(*asserts.NotFoundError); ok {
			return validationSetNotFound(accountID, name, sequence)
		}
	}

	sets := snapasserts.NewValidationSets()
	vset := as.(*asserts.ValidationSet)
	mylog.Check(sets.Add(vset))

	snaps, _ := mylog.Check3(snapstate.InstalledSnaps(st))

	validErr := checkInstalledSnaps(sets, snaps, nil)
	res := validationSetResult{
		AccountID: vset.AccountID(),
		Name:      vset.Name(),
		Sequence:  vset.Sequence(),
		// TODO: pass actual err details and implement "verbose" mode
		// for the client?
		Valid: validErr == nil,
	}
	return SyncResponse(res)
}

func getSingleSeqFormingAssertion(st *state.State, accountID, name string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	sto := snapstate.Store(st, nil)
	at := asserts.Type("validation-set")
	if at == nil {
		panic("validation-set assert type not found")
	}

	sequenceKey := []string{release.Series, accountID, name}
	st.Unlock()
	as := mylog.Check2(sto.SeqFormingAssertion(at, sequenceKey, sequence, user))
	st.Lock()

	return as, nil
}

func enforceValidationSet(st *state.State, accountID, name string, sequence, userID int) Response {
	snaps, ignoreValidation := mylog.Check3(snapstate.InstalledSnaps(st))

	tr := mylog.Check2(assertstateFetchAndApplyEnforcedValidationSet(st, accountID, name, sequence, userID, snaps, ignoreValidation))

	// XXX: provide more specific error kinds? This would probably require
	// assertstate.ValidationSetAssertionForEnforce tuning too.

	res := mylog.Check2(validationSetResultFromTracking(st, tr))

	return SyncResponse(*res)
}
