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
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

type validationSetResult struct {
	ValidationSet string `json:"validation-set,omitempty"`
	Mode          string `json:"mode"`
	Seq           int    `json:"seq,omitempty"`
	Valid         bool   `json:"valid"`
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

func validationSetKeyString(accountID, name string, pinAt int) string {
	key := assertstate.ValidationSetKey(accountID, name)
	if pinAt != 0 {
		key = fmt.Sprintf("%s=%d", key, pinAt)
	}
	return key
}

func validationSetNotFound(accountID, name string, pinAt int) Response {
	res := &errorResult{
		Message: "validation set not found",
		Kind:    client.ErrorKindValidationSetNotFound,
		Value:   validationSetKeyString(accountID, name, pinAt),
	}
	return &resp{
		Type:   ResponseTypeError,
		Result: res,
		Status: 404,
	}
}

// XXX: share this with client-side validation?
var validName = regexp.MustCompile("^[a-z][0-9a-z]+$")

func validationSetAccountAndName(validationSet string) (accountID, name string, err error) {
	parts := strings.Split(validationSet, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid validation-set argument")
	}
	accountID = parts[0]
	name = parts[1]
	if !validName.MatchString(accountID) {
		return "", "", fmt.Errorf("invalid account name %q", accountID)
	}
	if !validName.MatchString(name) {
		return "", "", fmt.Errorf("invalid name %q", name)
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

	results := make([]validationSetResult, len(names))
	for i, vs := range names {
		tr := validationSets[vs]
		// TODO: evaluate against installed snaps
		var valid bool
		modeStr, err := modeString(tr.Mode)
		if err != nil {
			return InternalError(err.Error())
		}
		results[i] = validationSetResult{
			ValidationSet: validationSetKeyString(tr.AccountID, tr.Name, tr.PinnedAt),
			Mode:          modeStr,
			Seq:           tr.Current,
			Valid:         valid,
		}
	}

	return SyncResponse(results, nil)
}

func getValidationSet(c *Command, r *http.Request, _ *auth.UserState) Response {
	vars := muxVars(r)
	accountID := vars["account"]
	name := vars["name"]

	if !validName.MatchString(accountID) {
		return BadRequest("invalid account name %q", accountID)
	}
	if !validName.MatchString(name) {
		return BadRequest("invalid name %q", name)
	}

	query := r.URL.Query()

	// pin-at is optional
	pinAtStr := query.Get("pin-at")
	var pinAt int
	if pinAtStr != "" {
		var err error
		pinAt, err = strconv.Atoi(pinAtStr)
		if err != nil {
			return BadRequest("invalid pin-at argument")
		}
		if pinAt < 0 {
			return BadRequest("invalid pin-at argument: %d", pinAt)
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var tr assertstate.ValidationSetTracking
	err := assertstate.GetValidationSet(st, accountID, name, &tr)
	if err == state.ErrNoState || (err == nil && pinAt != 0 && pinAt != tr.PinnedAt) {
		return validationSetNotFound(accountID, name, pinAt)
	}
	if err != nil {
		return InternalError("accessing validation sets failed: %v", err)
	}

	modeStr, err := modeString(tr.Mode)
	if err != nil {
		return InternalError(err.Error())
	}
	// TODO: evaluate against installed snaps
	var valid bool
	res := validationSetResult{
		ValidationSet: validationSetKeyString(tr.AccountID, tr.Name, tr.PinnedAt),
		Mode:          modeStr,
		Seq:           tr.Current,
		Valid:         valid,
	}
	return SyncResponse(res, nil)
}

type validationSetApplyRequest struct {
	Action string `json:"action"`
	Mode   string `json:"mode"`
	PinAt  int    `json:"pin-at,omitempty"`
}

func applyValidationSet(c *Command, r *http.Request, _ *auth.UserState) Response {
	vars := muxVars(r)
	accountID := vars["account"]
	name := vars["name"]

	if !validName.MatchString(accountID) {
		return BadRequest("invalid account name %q", accountID)
	}
	if !validName.MatchString(name) {
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
	if req.PinAt < 0 {
		return BadRequest("invalid pin-at argument: %d", req.PinAt)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if req.Action == "forget" {
		return forgetValidationSet(st, accountID, name, req.PinAt)
	}
	if req.Action != "apply" {
		return BadRequest("unsupported action %q", req.Action)
	}

	var mode assertstate.ValidationSetMode
	switch req.Mode {
	case "monitor":
		mode = assertstate.Monitor
	case "enforce":
		mode = assertstate.Enforce
	default:
		return BadRequest("invalid mode %q", req.Mode)
	}

	// TODO: check that it exists in the store?

	tr := assertstate.ValidationSetTracking{
		AccountID: accountID,
		Name:      name,
		Mode:      mode,
		// note, PinAt may be 0, meaning not pinned.
		PinnedAt: req.PinAt,
	}
	assertstate.UpdateValidationSet(st, &tr)
	return SyncResponse(nil, nil)
}

func forgetValidationSet(st *state.State, accountID, name string, pinAt int) Response {
	// check if it exists first
	var tr assertstate.ValidationSetTracking
	err := assertstate.GetValidationSet(st, accountID, name, &tr)
	if err == state.ErrNoState || (err == nil && pinAt != 0 && pinAt != tr.PinnedAt) {
		return validationSetNotFound(accountID, name, pinAt)
	}
	if err != nil {
		return InternalError("accessing validation sets failed: %v", err)
	}
	assertstate.DeleteValidationSet(st, accountID, name)
	return SyncResponse(nil, nil)
}
