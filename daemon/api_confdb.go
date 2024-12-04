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

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var (
	confdbCmd = &Command{
		Path:        "/v2/confdbs/{account}/{confdb}/{view}",
		GET:         getView,
		PUT:         setView,
		ReadAccess:  authenticatedAccess{Polkit: polkitActionManage},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateConfdbFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, confdbName, view := vars["account"], vars["confdb"], vars["view"]
	fieldStr := r.URL.Query().Get("fields")

	var fields []string
	if fieldStr != "" {
		fields = strutil.CommaSeparatedList(fieldStr)
	}

	results, err := confdbstateGet(st, account, confdbName, view, fields)
	if err != nil {
		return toAPIError(err)
	}

	return SyncResponse(results)
}

func setView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateConfdbFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, confdbName, viewName := vars["account"], vars["confdb"], vars["view"]

	decoder := json.NewDecoder(r.Body)
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return BadRequest("cannot decode confdb request body: %v", err)
	}

	view, err := confdbstateGetView(st, account, confdbName, viewName)
	if err != nil {
		return toAPIError(err)
	}

	tx, commitTxFunc, err := confdbstateGetTransaction(nil, st, view)
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

func validateConfdbFeatureFlag(st *state.State) *apiError {
	tr := config.NewTransaction(st)
	enabled, err := features.Flag(tr, features.Confdbs)
	if err != nil && !config.IsNoOption(err) {
		return InternalError(fmt.Sprintf("internal error: cannot check confdbs feature flag: %s", err))
	}

	if !enabled {
		_, confName := features.Confdbs.ConfigOption()
		return BadRequest(fmt.Sprintf(`"confdbs" feature flag is disabled: set '%s' to true`, confName))
	}
	return nil
}
