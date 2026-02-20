// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2025 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var (
	confdbCmd = &Command{
		Path:        "/v2/confdb/{account}/{confdb-schema}/{view}",
		GET:         getView,
		PUT:         setView,
		ReadAccess:  authenticatedAccess{Polkit: polkitActionManage},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
	confdbControlCmd = &Command{
		Path:        "/v2/confdb",
		POST:        handleConfdbControlAction,
		Actions:     []string{"delegate", "undelegate"},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateFeatureFlag(st, features.Confdb); err != nil {
		return err
	}

	vars := muxVars(r)
	account, schemaName, viewName := vars["account"], vars["confdb-schema"], vars["view"]

	keysStr := r.URL.Query().Get("keys")
	var keys []string
	if keysStr != "" {
		keys = strutil.CommaSeparatedList(keysStr)
	}

	constraintsRaw := r.URL.Query().Get("constraints")
	var constraints map[string]any
	if constraintsRaw != "" {
		if err := json.Unmarshal([]byte(constraintsRaw), &constraints); err != nil || constraints == nil {
			return BadRequest(`"constraints" must be a JSON object`)
		}

		err := validateConstraints(constraints)
		if err != nil {
			return BadRequest(err.Error())
		}
	}

	view, err := confdbstateGetView(st, account, schemaName, viewName)
	if err != nil {
		return toAPIError(err)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return toAPIError(err)
	}
	chgID, err := confdbstateLoadConfdbAsync(st, view, keys, constraints, int(ucred.Uid))
	if err != nil {
		return toAPIError(err)
	}

	ensureStateSoon(st)
	return AsyncResponse(nil, chgID)
}

func validateConstraints(cstrs map[string]any) error {
	for k, v := range cstrs {
		var typeStr string
		switch v.(type) {
		case nil:
			typeStr = "null"
		case []any:
			typeStr = "array"
		case map[string]any:
			typeStr = "map"
		default:
			continue
		}

		return fmt.Errorf("constraint value must be non-null scalar but parameter %q has %s constraint", k, typeStr)
	}

	return nil
}

func setView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateFeatureFlag(st, features.Confdb); err != nil {
		return err
	}

	vars := muxVars(r)
	account, schemaName, viewName := vars["account"], vars["confdb-schema"], vars["view"]

	type setAction struct {
		Values map[string]any `json:"values"`
	}

	var action setAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&action); err != nil {
		return BadRequest("cannot decode confdb request body: %v", err)
	}

	if len(action.Values) == 0 {
		return BadRequest("cannot set confdb: request body contains no values")
	}
	// TODO: apply some size restrictions to the value (so someone can't pass
	// a massive string, for instance)

	view, err := confdbstateGetView(st, account, schemaName, viewName)
	if err != nil {
		return toAPIError(err)
	}

	tx, commitTxFunc, err := confdbstateGetTransactionToSet(nil, st, view)
	if err != nil {
		return toAPIError(err)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return toAPIError(err)
	}

	err = confdbstateSetViaView(tx, view, action.Values, int(ucred.Uid))
	if err != nil {
		return toAPIError(err)
	}

	changeID, _, err := commitTxFunc()
	if err != nil {
		return toAPIError(err)
	}

	return AsyncResponse(nil, changeID)
}

func toAPIError(err error) *apiError {
	switch {
	case errors.Is(err, &asserts.NotFoundError{}):
		return &apiError{
			Status:  400,
			Message: err.Error(),
			Kind:    client.ErrorKindAssertionNotFound,
			Value:   err,
		}
	case errors.Is(err, &confdbstate.NoViewError{}):
		fallthrough
	case errors.Is(err, &confdb.NoMatchError{}):
		return &apiError{
			Status:  400,
			Message: err.Error(),
			Kind:    client.ErrorKindOptionNotAvailable,
			Value:   err,
		}
	case errors.Is(err, &confdb.NoDataError{}):
		return &apiError{
			Status:  400,
			Message: err.Error(),
			Kind:    client.ErrorKindConfigNoSuchOption,
			Value:   err,
		}
	case errors.Is(err, &confdb.BadRequestError{}),
		errors.Is(err, &confdb.UnconstrainedParamsError{}),
		errors.Is(err, &confdb.UnmatchedConstraintsError{}):
		return BadRequest(err.Error())
	default:
		return InternalError(err.Error())
	}
}

func validateFeatureFlag(st *state.State, feature features.SnapdFeature) *apiError {
	tr := config.NewTransaction(st)
	enabled, err := features.Flag(tr, feature)
	if err != nil && !config.IsNoOption(err) {
		return InternalError(
			fmt.Sprintf("internal error: cannot check %q feature flag: %s", feature, err),
		)
	}

	if !enabled {
		_, confName := feature.ConfigOption()
		return BadRequest(
			fmt.Sprintf(`feature flag %q is disabled: set '%s' to true`, feature, confName),
		)
	}
	return nil
}

type confdbControlAction struct {
	Action          string   `json:"action"`
	OperatorID      string   `json:"operator-id"`
	Authentications []string `json:"authentications"`
	Views           []string `json:"views"`
}

func handleConfdbControlAction(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateFeatureFlag(st, features.Confdb); err != nil {
		return err
	}
	if err := validateFeatureFlag(st, features.ConfdbControl); err != nil {
		return err
	}

	devMgr := c.d.overlord.DeviceManager()
	cc, err := devMgr.ConfdbControl()
	if err != nil &&
		(!errors.Is(err, state.ErrNoState) ||
			errors.Is(err, devicestate.ErrNoDeviceIdentityYet)) {
		return InternalError(err.Error())
	}

	var ctrl confdb.Control
	var revision int
	if cc != nil {
		ctrl = cc.Control()
		revision = cc.Revision() + 1
	}

	var a confdbControlAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&a); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	switch a.Action {
	case "delegate":
		err = ctrl.Delegate(a.OperatorID, a.Views, a.Authentications)
	case "undelegate":
		err = ctrl.Undelegate(a.OperatorID, a.Views, a.Authentications)
	default:
		return BadRequest("unknown action %q", a.Action)
	}
	if err != nil {
		return BadRequest(err.Error())
	}

	cc, err = devicestateSignConfdbControl(devMgr, ctrl.Groups(), revision)
	if err != nil {
		return InternalError(err.Error())
	}

	if err := assertstate.Add(st, cc); err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(nil)
}
