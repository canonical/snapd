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
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

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
		Path: "/v2/validation-sets",
		GET:  listValidationSets,
	}

	validationSetsCmd = &Command{
		Path: "/v2/validation-sets/{account}/{name}",
		GET:  getValidationSet,
		POST: applyValidationSet,
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
	res := &errorResult{
		Message: "validation set not found",
		Kind:    client.ErrorKindValidationSetNotFound,
		Value:   v,
	}
	return &resp{
		Type:   ResponseTypeError,
		Result: res,
		Status: 404,
	}
}

func validationSetAccountAndName(validationSet string) (accountID, name string, err error) {
	parts := strings.Split(validationSet, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid validation-set argument")
	}
	accountID = parts[0]
	name = parts[1]
	if !asserts.IsValidAccountID(accountID) {
		return "", "", fmt.Errorf("invalid account ID %q", accountID)
	}
	if !asserts.IsValidValidationSetName(name) {
		return "", "", fmt.Errorf("invalid validation set name %q", name)
	}
	return accountID, name, nil
}

func listValidationSets(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	validationSets, err := assertstate.ValidationSets(st)
	if err != nil {
		return InternalError("accessing validation sets failed: %v", err)
	}

	names := make([]string, 0, len(validationSets))
	for k := range validationSets {
		names = append(names, k)
	}
	sort.Strings(names)

	snaps, err := installedSnaps(st)
	if err != nil {
		return InternalError(err.Error())
	}

	results := make([]validationSetResult, len(names))
	for i, vs := range names {
		tr := validationSets[vs]
		sequence := tr.Current
		if tr.PinnedAt > 0 {
			sequence = tr.PinnedAt
		}
		sets, err := validationSetForAssert(st, tr.AccountID, tr.Name, sequence)
		if err != nil {
			return InternalError("cannot get assertion for validation set tracking %s/%s/%d: %v", tr.AccountID, tr.Name, sequence, err)
		}
		validErr := checkInstalledSnaps(sets, snaps)
		modeStr, err := modeString(tr.Mode)
		if err != nil {
			return InternalError(err.Error())
		}
		results[i] = validationSetResult{
			AccountID: tr.AccountID,
			Name:      tr.Name,
			PinnedAt:  tr.PinnedAt,
			Mode:      modeStr,
			Sequence:  tr.Current,
			Valid:     validErr == nil,
		}
	}

	return SyncResponse(results, nil)
}

var checkInstalledSnaps = func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error {
	return vsets.CheckInstalledSnaps(snaps)
}

func installedSnaps(st *state.State) ([]*snapasserts.InstalledSnap, error) {
	var snaps []*snapasserts.InstalledSnap
	all, err := snapstate.All(st)
	if err != nil {
		return nil, err
	}
	for _, snapState := range all {
		cur, err := snapState.CurrentInfo()
		if err != nil {
			return nil, err
		}
		snaps = append(snaps,
			snapasserts.NewInstalledSnap(snapState.InstanceName(),
				snapState.CurrentSideInfo().SnapID,
				cur.Revision))
	}
	return snaps, nil
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
		var err error
		sequence, err = strconv.Atoi(sequenceStr)
		if err != nil {
			return BadRequest("invalid sequence argument")
		}
		if sequence < 0 {
			return BadRequest("invalid sequence argument: %d", sequence)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var tr assertstate.ValidationSetTracking
	err := assertstate.GetValidationSet(st, accountID, name, &tr)
	if err == state.ErrNoState || (err == nil && sequence != 0 && sequence != tr.PinnedAt) {
		// not available locally, try to find in the store.
		return validateAgainstStore(st, accountID, name, sequence, user)
	}
	if err != nil {
		return InternalError("accessing validation sets failed: %v", err)
	}

	modeStr, err := modeString(tr.Mode)
	if err != nil {
		return InternalError(err.Error())
	}

	// evaluate against installed snaps

	if tr.PinnedAt > 0 {
		sequence = tr.PinnedAt
	} else {
		sequence = tr.Current
	}
	sets, err := validationSetForAssert(st, tr.AccountID, tr.Name, sequence)
	if err != nil {
		return InternalError(err.Error())
	}
	snaps, err := installedSnaps(st)
	if err != nil {
		return InternalError(err.Error())
	}

	validErr := checkInstalledSnaps(sets, snaps)
	res := validationSetResult{
		AccountID: tr.AccountID,
		Name:      tr.Name,
		PinnedAt:  tr.PinnedAt,
		Mode:      modeStr,
		Sequence:  tr.Current,
		Valid:     validErr == nil,
	}
	return SyncResponse(res, nil)
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
	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode request body into validation set action: %v", err)
	}
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

var validationSetAssertionForMonitor = assertstate.ValidationSetAssertionForMonitor

// updateValidationSet handles snap validate --monitor and --enforce accountId/name[=sequence].
func updateValidationSet(st *state.State, accountID, name string, reqMode string, sequence int, user *auth.UserState) Response {
	var mode assertstate.ValidationSetMode
	// TODO: only monitor mode for now, add enforce.
	switch reqMode {
	case "monitor":
		mode = assertstate.Monitor
	default:
		return BadRequest("invalid mode %q", reqMode)
	}

	tr := assertstate.ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      mode,
		// note, Sequence may be 0, meaning not pinned.
		PinnedAt: sequence,
	}

	userID := 0
	if user != nil {
		userID = user.ID
	}
	pinned := sequence > 0
	as, local, err := validationSetAssertionForMonitor(st, accountID, name, sequence, pinned, userID)
	if err != nil {
		return BadRequest("cannot get validation set assertion for %v: %v", assertstate.ValidationSetKey(accountID, name), err)
	}
	tr.Current = as.Sequence()
	tr.LocalOnly = local

	assertstate.UpdateValidationSet(st, &tr)
	return SyncResponse(nil, nil)
}

// forgetValidationSet forgets the validation set.
// The state needs to be locked by the caller.
func forgetValidationSet(st *state.State, accountID, name string, sequence int) Response {
	// check if it exists first
	var tr assertstate.ValidationSetTracking
	err := assertstate.GetValidationSet(st, accountID, name, &tr)
	if err == state.ErrNoState || (err == nil && sequence != 0 && sequence != tr.PinnedAt) {
		return validationSetNotFound(accountID, name, sequence)
	}
	if err != nil {
		return InternalError("accessing validation sets failed: %v", err)
	}
	assertstate.DeleteValidationSet(st, accountID, name)
	return SyncResponse(nil, nil)
}

func validationSetAssertFromDb(st *state.State, accountID, name string, sequence int) (*asserts.ValidationSet, error) {
	headers := map[string]string{
		"series":     release.Series,
		"account-id": accountID,
		"name":       name,
		"sequence":   fmt.Sprintf("%d", sequence),
	}
	st.Lock()
	db := assertstate.DB(st)
	st.Unlock()
	as, err := db.Find(asserts.ValidationSetType, headers)
	if err != nil {
		return nil, err
	}
	vset := as.(*asserts.ValidationSet)
	return vset, nil
}

func validationSetForAssert(st *state.State, accountID, name string, sequence int) (*snapasserts.ValidationSets, error) {
	st.Unlock()
	as, err := validationSetAssertFromDb(st, accountID, name, sequence)
	st.Lock()
	if err != nil {
		return nil, err
	}
	sets := snapasserts.NewValidationSets()
	if err := sets.Add(as); err != nil {
		return nil, err
	}
	return sets, nil
}

func validateAgainstStore(st *state.State, accountID, name string, sequence int, user *auth.UserState) Response {
	// not available locally, try to find in the store.
	as, err := getSingleSeqFormingAssertion(st, accountID, name, sequence, user)
	if _, ok := err.(*asserts.NotFoundError); ok {
		return validationSetNotFound(accountID, name, sequence)
	}
	if err != nil {
		return InternalError(err.Error())
	}
	sets := snapasserts.NewValidationSets()
	vset := as.(*asserts.ValidationSet)
	if err := sets.Add(vset); err != nil {
		return InternalError(err.Error())
	}
	snaps, err := installedSnaps(st)
	if err != nil {
		return InternalError(err.Error())
	}

	validErr := checkInstalledSnaps(sets, snaps)
	res := validationSetResult{
		AccountID: vset.AccountID(),
		Name:      vset.Name(),
		Sequence:  vset.Sequence(),
		// TODO: pass actual err details and implement "verbose" mode
		// for the client?
		Valid: validErr == nil,
	}
	return SyncResponse(res, nil)
}

func getSingleSeqFormingAssertion(st *state.State, accountID, name string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	sto := snapstate.Store(st, nil)
	at := asserts.Type("validation-set")
	if at == nil {
		panic("validation-set assert type not found")
	}

	sequenceKey := []string{release.Series, accountID, name}
	as, err := sto.SeqFormingAssertion(at, sequenceKey, sequence, user)
	if err != nil {
		return nil, err
	}

	return as, nil
}
