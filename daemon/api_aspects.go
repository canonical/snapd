// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

var (
	aspectsCmd = &Command{
		Path:        "/v2/aspects/{account}/{bundle}/{aspect}",
		GET:         getAspect,
		PUT:         setAspect,
		ReadAccess:  authenticatedAccess{Polkit: polkitActionManage},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

func getAspect(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateAspectFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, bundleName, aspect := vars["account"], vars["bundle"], vars["aspect"]
	fields := strutil.CommaSeparatedList(r.URL.Query().Get("fields"))

	if len(fields) == 0 {
		return BadRequest("missing aspect fields")
	}

	results, err := aspectstateGetAspect(st, account, bundleName, aspect, fields)
	if err != nil {
		return toAPIError(err)
	}

	return SyncResponse(results)
}

func setAspect(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateAspectFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, bundleName, aspect := vars["account"], vars["bundle"], vars["aspect"]

	decoder := json.NewDecoder(r.Body)
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return BadRequest("cannot decode aspect request body: %v", err)
	}

	err := aspectstateSetAspect(st, account, bundleName, aspect, values)
	if err != nil {
		return toAPIError(err)
	}

	// NOTE: could be sync but this is closer to the final version and the conf API
	summary := fmt.Sprintf("Set aspect %s/%s/%s", account, bundleName, aspect)
	chg := newChange(st, "set-aspect", summary, nil, nil)
	chg.SetStatus(state.DoneStatus)
	ensureStateSoon(st)

	return AsyncResponse(nil, chg.ID())
}

func toAPIError(err error) *apiError {
	switch {
	case errors.Is(err, &aspects.NotFoundError{}):
		return NotFound(err.Error())

	case errors.Is(err, &aspects.BadRequestError{}):
		return BadRequest(err.Error())

	default:
		return InternalError(err.Error())
	}
}

func validateAspectFeatureFlag(st *state.State) *apiError {
	tr := config.NewTransaction(st)
	enabled, err := features.Flag(tr, features.AspectsConfiguration)
	if err != nil && !config.IsNoOption(err) {
		return InternalError(fmt.Sprintf("internal error: cannot check aspect configuration flag: %s", err))
	}

	if !enabled {
		_, confName := features.AspectsConfiguration.ConfigOption()
		return BadRequest(fmt.Sprintf("aspect-based configuration disabled: you must set '%s' to true", confName))
	}
	return nil
}
