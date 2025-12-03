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

package devicemgmtstate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ConfdbRequestPayload represents the JSON body of a confdb request-message assertion.
type ConfdbRequestPayload struct {
	Action  string         `json:"action"`
	Account string         `json:"account"`
	View    string         `json:"view"`
	Keys    []string       `json:"keys"`   // for get action
	Values  map[string]any `json:"values"` // for set action
}

// ConfdbMessageHandler handles confdb request-message messages.
type ConfdbMessageHandler struct{}

// Validate checks confdb-specific constraints.
func (h *ConfdbMessageHandler) Validate(st *state.State, msg *PendingMessage) error {
	// TODO: check against the confdb-control assertion
	return nil
}

// Apply processes a confdb request-message and returns a change ID.
func (h *ConfdbMessageHandler) Apply(st *state.State, msg *PendingMessage) (string, error) {
	payload := ConfdbRequestPayload{}
	err := json.Unmarshal([]byte(msg.Body), &payload)
	if err != nil {
		return "", fmt.Errorf("cannot unmarshal payload %s: %w", msg.Body, err)
	}

	viewPath := strings.SplitN(payload.View, "/", 2)
	view, err := confdbstate.GetView(st, payload.Account, viewPath[0], viewPath[1])
	if err != nil {
		return "", err
	}

	switch payload.Action {
	case "get":
		return confdbstate.LoadConfdbAsync(st, view, payload.Keys)
	case "set":
		tx, commitTxFunc, err := confdbstate.GetTransactionToSet(nil, st, view)
		if err != nil {
			return "", err
		}

		err = confdbstate.SetViaView(tx, view, payload.Values)
		if err != nil {
			return "", err
		}

		changeID, _, err := commitTxFunc()
		return changeID, err
	default:
		return "", fmt.Errorf("cannot apply confdb message: unknown action %q", payload.Action)
	}
}

// BuildResponse converts a completed confdb change into a response body and status.
func (h *ConfdbMessageHandler) BuildResponse(chg *state.Change) (map[string]any, asserts.MessageStatus) {
	if chg.Status() != state.DoneStatus {
		return map[string]any{"message": chg.Err()}, asserts.MessageStatusError
	}

	var apiData map[string]any
	err := chg.Get("api-data", &apiData)
	if err != nil {
		// Nothing to return, e.g., after confdb sets
		return map[string]any{}, asserts.MessageStatusSuccess
	}

	response := map[string]any{}
	errData, hasErr := apiData["error"]
	if hasErr {
		errMap, ok := errData.(map[string]any)
		if ok {
			kind, ok := errMap["kind"]
			if ok {
				response["kind"] = kind
			}

			message, ok := errMap["message"]
			if ok {
				response["message"] = message
			}
		} else {
			response["message"] = fmt.Sprintf("invalid error data format: expected map[string]any, got %T", errData)
		}

		return response, asserts.MessageStatusError
	}

	values, ok := apiData["values"]
	if ok {
		response["values"] = values
	}

	return response, asserts.MessageStatusSuccess
}
