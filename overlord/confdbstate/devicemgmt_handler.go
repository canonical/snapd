// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package confdbstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/devicemgmtstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	confdbstateGetView     = GetView
	confdbstateReadConfdb  = ReadConfdb
	confdbstateWriteConfdb = WriteConfdb
)

// deviceBackend fetches the device's confdb-control assertion.
type deviceBackend interface {
	ConfdbControl() (*asserts.ConfdbControl, error)
}

// confdbMessageHandler implements devicemgmtstate.MessageHandler for the "confdb" message kind.
type confdbMessageHandler struct {
	device deviceBackend
}

// Validate checks that the operator sending the message has been granted
// access to the requested confdb view in the device's confdb-control assertion.
func (h *confdbMessageHandler) Validate(st *state.State, msg *devicemgmtstate.RequestMessage) error {
	var payload struct {
		Account string `json:"account"`
		View    string `json:"view"`
	}
	err := json.Unmarshal([]byte(msg.Body), &payload)
	if err != nil {
		return fmt.Errorf("cannot decode message body: %v", err)
	}

	cc, err := h.device.ConfdbControl()
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &devicemgmtstate.UnauthorizedError{Operator: msg.AccountID}
		}

		return fmt.Errorf("cannot validate message: %v", err)
	}

	// TODO: implement store authentication method. Currently, the store doesn't
	// support signing request messages on behalf of operators.
	// For now, only "operator-key" is supported.

	ctrl := cc.Control()
	ok, err := ctrl.IsDelegated(
		msg.AccountID,
		payload.Account+"/"+payload.View,
		[]string{"operator-key"},
	)
	if err != nil {
		return fmt.Errorf("cannot validate message: %v", err)
	}
	if !ok {
		return &devicemgmtstate.UnauthorizedError{Operator: msg.AccountID}
	}

	return nil
}

// Apply schedules the confdb action described in the message and returns the change ID.
func (h *confdbMessageHandler) Apply(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
	// TODO: determine if constraints (filtering) and access-timeout need to be
	// supported in the message body spec (SD194), and implement them here.
	var payload struct {
		Action  string         `json:"action"`
		Account string         `json:"account"`
		View    string         `json:"view"`
		Keys    []string       `json:"keys"`
		Values  map[string]any `json:"values"`
	}
	err := json.Unmarshal([]byte(msg.Body), &payload)
	if err != nil {
		return "", fmt.Errorf("cannot decode message body: %v", err)
	}

	viewParts := strings.Split(payload.View, "/")
	if len(viewParts) != 2 {
		return "", fmt.Errorf("cannot apply message: invalid view %q, expected <schema>/<view-name>", payload.View)
	}

	view, err := confdbstateGetView(st, payload.Account, viewParts[0], viewParts[1])
	if err != nil {
		return "", err
	}

	switch payload.Action {
	case "get":
		return confdbstateReadConfdb(context.Background(), st, view, payload.Keys, nil, confdb.AdminAccess)
	case "set":
		if len(payload.Values) == 0 {
			return "", fmt.Errorf("cannot apply message: body contains no values to write")
		}

		return confdbstateWriteConfdb(context.Background(), st, view, payload.Values)
	default:
		return "", fmt.Errorf("cannot apply message: unknown action %q", payload.Action)
	}
}

// ResultFromChange returns the result of a completed confdb action.
func (h *confdbMessageHandler) ResultFromChange(chg *state.Change) (map[string]any, error) {
	if chg.Status() == state.ErrorStatus {
		return nil, chg.Err()
	}
	if chg.Status() != state.DoneStatus {
		logger.Noticef("internal: ResultFromChange called on change in unexpected status %s", chg.Status())
		return nil, fmt.Errorf("internal: unexpected change status %s", chg.Status())
	}

	var apiData map[string]any
	err := chg.Get("api-data", &apiData)
	if errors.Is(err, state.ErrNoState) {
		if chg.Kind() == setConfdbChangeKind {
			return map[string]any{}, nil
		}

		logger.Noticef("internal: change %q done with no api-data", chg.Kind())
		return nil, fmt.Errorf("internal: change %q done with no api-data", chg.Kind())
	}
	if err != nil {
		return nil, err
	}

	errData, hasErr := apiData["error"]
	if !hasErr {
		return apiData, nil
	}

	errMap, ok := errData.(map[string]any)
	if !ok {
		logger.Noticef("internal: api-data error field is not a map")
		return nil, fmt.Errorf("internal: api-data error field is not a map")
	}

	msg, _ := errMap["message"].(string)
	return nil, fmt.Errorf("%s", msg)
}
