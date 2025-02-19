// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
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
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateFeatureFlag(st, features.Confdbs); err != nil {
		return err
	}

	vars := muxVars(r)
	account, schemaName, viewName := vars["account"], vars["confdb-schema"], vars["view"]
	fieldStr := r.URL.Query().Get("fields")

	var fields []string
	if fieldStr != "" {
		fields = strutil.CommaSeparatedList(fieldStr)
	}

	view, err := confdbstateGetView(st, account, schemaName, viewName)
	if err != nil {
		return toAPIError(err)
	}

	chgID, err := confdbstateLoadConfdbAsync(st, view, fields)
	if err != nil {
		return toAPIError(err)
	}

	ensureStateSoon(st)
	return AsyncResponse(nil, chgID)
}

func setView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateFeatureFlag(st, features.Confdbs); err != nil {
		return err
	}

	vars := muxVars(r)
	account, schemaName, viewName := vars["account"], vars["confdb-schema"], vars["view"]

	decoder := json.NewDecoder(r.Body)
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return BadRequest("cannot decode confdb request body: %v", err)
	}

	view, err := confdbstateGetView(st, account, schemaName, viewName)
	if err != nil {
		return toAPIError(err)
	}

	tx, commitTxFunc, err := confdbstateGetTransactionToSet(nil, st, view)
	if err != nil {
		return toAPIError(err)
	}

	err = confdbstateSetViaView(tx, view, values)
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
	case errors.Is(err, &confdb.NotFoundError{}):
		return NotFound(err.Error())

	case errors.Is(err, &confdb.BadRequestError{}):
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
			fmt.Sprintf("internal error: cannot check %s feature flag: %s", feature.String(), err),
		)
	}

	if !enabled {
		_, confName := feature.ConfigOption()
		return BadRequest(
			fmt.Sprintf(`"%s" feature flag is disabled: set '%s' to true`, feature.String(), confName),
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

	if err := validateFeatureFlag(st, features.Confdbs); err != nil {
		return err
	}
	if err := validateFeatureFlag(st, features.ConfdbControl); err != nil {
		return err
	}

	devMgr := c.d.overlord.DeviceManager()
	ctrl, revision, err := getOrCreateConfdbControl(st, devMgr)
	if err != nil {
		return InternalError(err.Error())
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

	cc, err := devicestateSignConfdbControl(devMgr, ctrl.Groups(), revision)
	if err != nil {
		return InternalError(err.Error())
	}

	if err := assertstate.Add(st, cc); err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(nil)
}

// getOrCreateConfdbControl returns the confdb.Control base to build the next revision of the assertion.
func getOrCreateConfdbControl(st *state.State, devMgr *devicestate.DeviceManager) (*confdb.Control, int, error) {
	serial, err := devMgr.Serial()
	if err != nil {
		return nil, 0, errors.New("device has no serial assertion")
	}

	db := assertstate.DB(st)
	a, err := db.Find(asserts.ConfdbControlType, map[string]string{
		"brand-id": serial.BrandID(),
		"model":    serial.Model(),
		"serial":   serial.Serial(),
	})
	if errors.Is(err, &asserts.NotFoundError{}) {
		return &confdb.Control{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}

	cc := a.(*asserts.ConfdbControl)
	ctrl := cc.Control()
	return &ctrl, cc.Revision() + 1, nil
}
