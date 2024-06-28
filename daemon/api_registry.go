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

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/strutil"
)

var (
	registryCmd = &Command{
		Path:        "/v2/registry/{account}/{registry}/{view}",
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

	if err := validateRegistryFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, registryName, view := vars["account"], vars["registry"], vars["view"]
	fieldStr := r.URL.Query().Get("fields")

	var fields []string
	if fieldStr != "" {
		fields = strutil.CommaSeparatedList(fieldStr)
	}

	results, err := registrystateGetViaView(st, account, registryName, view, fields)
	if err != nil {
		return toAPIError(err)
	}

	return SyncResponse(results)
}

func setView(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.state
	st.Lock()
	defer st.Unlock()

	if err := validateRegistryFeatureFlag(st); err != nil {
		return err
	}

	vars := muxVars(r)
	account, registryName, view := vars["account"], vars["registry"], vars["view"]

	decoder := json.NewDecoder(r.Body)
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return BadRequest("cannot decode registry request body: %v", err)
	}

	err := registrystateSetViaView(st, account, registryName, view, values)
	if err != nil {
		return toAPIError(err)
	}

	// NOTE: could be sync but this is closer to the final version and the conf API
	summary := fmt.Sprintf("Set registry view %s/%s/%s", account, registryName, view)
	chg := newChange(st, "set-registry-view", summary, nil, nil)
	chg.SetStatus(state.DoneStatus)
	ensureStateSoon(st)

	return AsyncResponse(nil, chg.ID())
}

func toAPIError(err error) *apiError {
	switch {
	case errors.Is(err, &registry.NotFoundError{}):
		return NotFound(err.Error())

	case errors.Is(err, &registry.BadRequestError{}):
		return BadRequest(err.Error())

	default:
		return InternalError(err.Error())
	}
}

func validateRegistryFeatureFlag(st *state.State) *apiError {
	tr := config.NewTransaction(st)
	enabled, err := features.Flag(tr, features.Registries)
	if err != nil && !config.IsNoOption(err) {
		return InternalError(fmt.Sprintf("internal error: cannot check registries feature flag: %s", err))
	}

	if !enabled {
		_, confName := features.Registries.ConfigOption()
		return BadRequest(fmt.Sprintf(`"registries" feature flag is disabled: set '%s' to true`, confName))
	}
	return nil
}
