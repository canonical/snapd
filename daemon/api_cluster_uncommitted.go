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

package daemon

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/state"
)

var clusterUncommittedCmd = &Command{
	Path:        "/v2/cluster/uncommitted",
	GET:         getClusterUncommitted,
	POST:        postClusterUncommitted,
	ReadAccess:  rootAccess{},
	WriteAccess: rootAccess{},
}

func getClusterUncommitted(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	headers, err := clusterstate.GetUncommittedClusterHeaders(st)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return NotFound("no uncommitted cluster state")
		}
		return InternalError("cannot get uncommitted cluster headers: %v", err)
	}

	return SyncResponse(headers)
}

type clusterCommitRequest struct {
	Assertion string `json:"assertion"`
}

func postClusterUncommitted(c *Command, r *http.Request, user *auth.UserState) Response {
	var req clusterCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	if req.Assertion == "" {
		return BadRequest("assertion is required")
	}

	assertion, err := asserts.Decode([]byte(req.Assertion))
	if err != nil {
		return BadRequest("cannot decode assertion: %v", err)
	}

	cluster, ok := assertion.(*asserts.Cluster)
	if !ok {
		return BadRequest("assertion must be a cluster assertion")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if err := clusterstate.CommitClusterAssertion(st, cluster); err != nil {
		return InternalError("cannot commit cluster assertion: %v", err)
	}

	return SyncResponse(nil)
}
