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
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	devicemgmthandlers "github.com/snapcore/snapd/overlord/devicemgmtstate/handlers"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	confdbstateGetView     = GetView
	confdbstateReadConfdb  = ReadConfdb
	confdbstateWriteConfdb = WriteConfdb
)

// checkConfdbFeatureFlags checks that both confdb and confdb-control are enabled.
func checkConfdbFeatureFlags(st *state.State) error {
	tr := config.NewTransaction(st)
	for _, feature := range []features.SnapdFeature{features.Confdb, features.ConfdbControl} {
		enabled, err := features.Flag(tr, feature)
		if err != nil && !config.IsNoOption(err) {
			return fmt.Errorf("cannot check %q feature flag: %v", feature, err)
		}

		if !enabled {
			return fmt.Errorf("feature flag %q is disabled", feature)
		}
	}

	return nil
}

// confdbAction describes a confdb "get" or "set" action.
type confdbAction struct {
	Action      string         `json:"action"`
	Account     string         `json:"account"`
	View        string         `json:"view"`
	Keys        []string       `json:"keys"`
	Constraints map[string]any `json:"constraints"`
	Values      map[string]any `json:"values"`
}

// decodeConfdbAction decodes the JSON body of a request message into a confdbAction.
func decodeConfdbAction(raw string) (confdbAction, error) {
	var body confdbAction
	err := json.Unmarshal([]byte(raw), &body)
	if err != nil {
		return confdbAction{}, fmt.Errorf("cannot decode message body: %v", err)
	}

	return body, nil
}

// validate checks that a confdbAction is well-formed.
func (a confdbAction) validate() error {
	if a.Account == "" {
		return fmt.Errorf("account is required")
	}

	_, _, err := parseView(a.View)
	if err != nil {
		return err
	}

	switch a.Action {
	case "get":
		err := confdb.ValidateConstraints(a.Constraints)
		if err != nil {
			return err
		}
	case "set":
		if len(a.Values) == 0 {
			return fmt.Errorf("body contains no values to write")
		}
	default:
		return fmt.Errorf("unknown action %q", a.Action)
	}

	return nil
}

// deviceBackend fetches the device's confdb-control assertion.
type deviceBackend interface {
	ConfdbControl() (*asserts.ConfdbControl, error)
}

// confdbMessageHandler implements devicemgmthandlers.MessageHandler for the "confdb" message kind.
type confdbMessageHandler struct {
	device deviceBackend
}

// Validate checks that the confdb request message is well-formed and that
// the sending operator has been granted access to the requested view.
func (h *confdbMessageHandler) Validate(ctx context.Context, st *state.State, msg *devicemgmthandlers.RequestMessage) error {
	err := checkConfdbFeatureFlags(st)
	if err != nil {
		return fmt.Errorf("cannot validate message: %v", err)
	}

	action, err := decodeConfdbAction(msg.Body)
	if err != nil {
		return err
	}

	err = action.validate()
	if err != nil {
		return fmt.Errorf("cannot validate message: %v", err)
	}

	cc, err := h.device.ConfdbControl()
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &devicemgmthandlers.UnauthorizedError{Operator: msg.AccountID}
		}

		return fmt.Errorf("cannot validate message: %v", err)
	}

	// TODO: implement store authentication method. Currently, the store doesn't
	// support signing request messages on behalf of operators.
	if msg.AuthorityID != msg.AccountID {
		return &devicemgmthandlers.UnauthorizedError{Operator: msg.AccountID}
	}

	ctrl := cc.Control()
	authMethod := []string{"operator-key"}
	delegated, err := ctrl.IsDelegated(msg.AccountID, action.Account+"/"+action.View, authMethod)
	if err != nil {
		return fmt.Errorf("cannot validate message: %v", err)
	}
	if !delegated {
		return &devicemgmthandlers.UnauthorizedError{Operator: msg.AccountID}
	}

	return nil
}

// Apply schedules the confdb action described in the message and returns the change ID.
func (h *confdbMessageHandler) Apply(ctx context.Context, st *state.State, msg *devicemgmthandlers.RequestMessage) (string, error) {
	action, err := decodeConfdbAction(msg.Body)
	if err != nil {
		return "", err
	}

	schemaName, viewName, err := parseView(action.View)
	if err != nil {
		return "", fmt.Errorf("cannot apply message: %v", err)
	}

	view, err := confdbstateGetView(st, action.Account, schemaName, viewName)
	if err != nil {
		return "", err
	}

	var chgID string
	switch action.Action {
	case "get":
		chgID, err = confdbstateReadConfdb(ctx, st, view, action.Keys, action.Constraints, confdb.AdminAccess)
	case "set":
		chgID, err = confdbstateWriteConfdb(ctx, st, view, action.Values)
	default:
		return "", fmt.Errorf("cannot apply message: unknown action %q", action.Action)
	}
	if err != nil {
		return "", err
	}

	chg := st.Change(chgID)
	if chg == nil {
		return "", fmt.Errorf("internal error: cannot find change %q created for confdb message", chgID)
	}
	devicemgmthandlers.MarkChangeForMessage(chg, msg)

	return chgID, nil
}

// ResultFromChange returns the result of a completed confdb action.
func (h *confdbMessageHandler) ResultFromChange(ctx context.Context, chg *state.Change) (map[string]any, error) {
	var apiData map[string]any
	err := chg.Get("api-data", &apiData)
	if errors.Is(err, state.ErrNoState) {
		// A "set" change with no api-data succeeded with nothing to report.
		// Per SD194, a successful "set" response body is an empty object {}.
		if chg.Kind() == setConfdbChangeKind {
			return map[string]any{}, nil
		}

		return nil, fmt.Errorf("internal error: %q change (%s) done with no api-data", chg.Kind(), chg.ID())
	}
	if err != nil {
		return nil, err
	}

	errData, ok := apiData["error"]
	if !ok {
		return apiData, nil
	}

	errMap, ok := errData.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("internal error: api-data error field is not a map")
	}

	msg, ok := errMap["message"].(string)
	if !ok {
		return nil, fmt.Errorf("internal error: api-data error field has no message")
	}

	return nil, fmt.Errorf("%s", msg)
}

func parseView(raw string) (schemaName, viewName string, err error) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid view %q, expected <schema>/<view-name>", raw)
	}

	return parts[0], parts[1], nil
}
