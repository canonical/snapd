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
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/clusterstate"
)

var clusterCommitCmd = &Command{
	Path:        "/v2/cluster/commit",
	POST:        postClusterCommit,
	WriteAccess: rootAccess{},
}

type clusterCommitRequest struct {
	ClusterID string `json:"cluster-id"`
}

func postClusterCommit(c *Command, r *http.Request, user *auth.UserState) Response {
	var req clusterCommitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	if req.ClusterID == "" {
		return BadRequest("cluster-id is required")
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	if err := clusterstate.CommitClusterAssertion(st, req.ClusterID); err != nil {
		return InternalError("cannot commit cluster assertion: %v", err)
	}

	return SyncResponse(nil)
}
