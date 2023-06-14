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
	"github.com/snapcore/snapd/overlord/auth"
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
	vars := muxVars(r)
	account, bundleName, aspect := vars["account"], vars["bundle"], vars["aspect"]
	fields := strutil.CommaSeparatedList(r.URL.Query().Get("fields"))

	if len(fields) == 0 {
		return BadRequest("missing aspect fields")
	}
	results := make(map[string]interface{})

	st := c.d.state
	st.Lock()
	defer st.Unlock()

	for _, field := range fields {
		var value interface{}
		err := aspectstateGet(c.d.state, account, bundleName, aspect, field, &value)
		if err != nil {
			if errors.Is(err, &aspects.FieldNotFoundError{}) {
				// keep looking; return partial result, if only some fields are found
				continue
			} else {
				return toAPIError(err)
			}
		}

		results[field] = value
	}

	// no results were found, return 404
	if len(results) == 0 {
		return NotFound("no fields were found")
	}

	return SyncResponse(results)
}

func setAspect(c *Command, r *http.Request, _ *auth.UserState) Response {
	vars := muxVars(r)
	account, bundleName, aspect := vars["account"], vars["bundle"], vars["aspect"]

	decoder := json.NewDecoder(r.Body)
	var values map[string]interface{}
	if err := decoder.Decode(&values); err != nil {
		return BadRequest("cannot decode aspect request body: %v", err)
	}

	st := c.d.state
	st.Lock()
	defer st.Unlock()

	for field, value := range values {
		err := aspectstateSet(c.d.state, account, bundleName, aspect, field, value)
		if err != nil {
			return toAPIError(err)
		}
	}

	// NOTE: could be sync but this is closer to the final version and the conf API
	summary := fmt.Sprintf("Set aspect %s/%s/%s", account, bundleName, aspect)
	chg := newChange(st, "set-aspect", summary, nil, nil)
	ensureStateSoon(st)

	return AsyncResponse(nil, chg.ID())
}

func toAPIError(err error) *apiError {
	switch {
	case aspects.IsNotFound(err):
		return NotFound(err.Error())

	case errors.Is(err, &aspects.InvalidAccessError{}):
		return &apiError{
			Status:  403,
			Message: err.Error(),
		}

	default:
		return InternalError(err.Error())
	}
}
