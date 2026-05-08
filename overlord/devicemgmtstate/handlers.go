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

package devicemgmtstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	confdbstateGetView     = confdbstate.GetView
	confdbstateReadConfdb  = confdbstate.ReadConfdb
	confdbstateWriteConfdb = confdbstate.WriteConfdb
)

// confdbMessageHandler implements MessageHandler for the "confdb" message kind.
type confdbMessageHandler struct{}

// Validate checks that the requesting account is authorized by the
// device's confdb-control assertion.
func (h *confdbMessageHandler) Validate(st *state.State, msg *requestMessage) *messageResult {
	// TODO: implement this handler, no-op for now.
	return nil
}

// Apply schedules the confdb read or write as an async change and returns
// its ID for ResultFromChange to consume once the change completes.
func (h *confdbMessageHandler) Apply(st *state.State, msg *requestMessage) (string, *messageResult) {
	var payload struct {
		Action  string         `json:"action"`
		Account string         `json:"account"`
		View    string         `json:"view"`
		Keys    []string       `json:"keys"`
		Values  map[string]any `json:"values"`
	}
	err := json.Unmarshal([]byte(msg.Body), &payload)
	if err != nil {
		return "", &messageResult{
			Status: asserts.MessageStatusError,
			Body: map[string]any{
				"message": fmt.Sprintf("cannot decode confdb message body: %v", err),
			},
		}
	}

	viewPath := strings.SplitN(payload.View, "/", 2)
	if len(viewPath) != 2 {
		return "", &messageResult{
			Status: asserts.MessageStatusError,
			Body: map[string]any{
				"message": fmt.Sprintf("invalid view path: expected 2 parts separated by /, got %d: %s", len(viewPath), payload.View),
			},
		}
	}

	view, err := confdbstateGetView(st, payload.Account, viewPath[0], viewPath[1])
	if err != nil {
		return "", toResult(err)
	}

	var changeID string
	switch payload.Action {
	case "get":
		changeID, err = confdbstateReadConfdb(context.Background(), st, view, payload.Keys, nil, confdb.AdminAccess)
	case "set":
		changeID, err = confdbstateWriteConfdb(context.Background(), st, view, payload.Values)
	default:
		return "", &messageResult{
			Status: asserts.MessageStatusError,
			Body: map[string]any{
				"message": fmt.Sprintf("cannot apply confdb message: unknown action %q", payload.Action),
			},
		}
	}
	if err != nil {
		return "", toResult(err)
	}

	return changeID, nil
}

// ResultFromChange reads the completed confdb change and returns the full result.
func (h *confdbMessageHandler) ResultFromChange(chg *state.Change) *messageResult {
	if chg.Status() != state.DoneStatus {
		errMsg := "change did not complete successfully"
		err := chg.Err()
		if err != nil {
			errMsg = err.Error()
		}

		return &messageResult{
			Status: asserts.MessageStatusError,
			Body:   map[string]any{"message": errMsg},
		}
	}

	var apiData map[string]any
	err := chg.Get("api-data", &apiData)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			// A successful set writes nothing to api-data.
			return &messageResult{
				Status: asserts.MessageStatusSuccess,
				Body:   map[string]any{},
			}
		}

		return &messageResult{
			Status: asserts.MessageStatusError,
			Body:   map[string]any{"message": err.Error()},
		}
	}

	errData, hasErr := apiData["error"]
	if hasErr {
		errMap, ok := errData.(map[string]any)
		if ok {
			return &messageResult{
				Status: asserts.MessageStatusError,
				Body:   errMap,
			}
		}

		return &messageResult{
			Status: asserts.MessageStatusError,
			Body:   map[string]any{"message": "no error context available"},
		}
	}

	return &messageResult{
		Status: asserts.MessageStatusSuccess,
		Body:   apiData,
	}
}

func toResult(err error) *messageResult {
	body := map[string]any{"message": err.Error()}

	kind := confdbstate.ToErrorKind(err)
	if kind != "" {
		body["kind"] = kind
	}

	return &messageResult{
		Status: asserts.MessageStatusError,
		Body:   body,
	}
}
