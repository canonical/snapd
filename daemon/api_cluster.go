// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/overlord/state"
)

// clusterCmd exposes the cluster API endpoint.
// For now only a POST action "assemble" is accepted.
var clusterCmd = &Command{
	Path:        "/v2/cluster",
	POST:        postClusterAction,
	Actions:     []string{"assemble"},
	WriteAccess: rootAccess{},
}

type clusterActionRequest struct {
	Action       string `json:"action"`
	Secret       string `json:"secret,omitempty"`
	Address      string `json:"address,omitempty"`
	ExpectedSize int    `json:"expected-size,omitempty"`
	Domain       string `json:"domain,omitempty"`
}

func postClusterAction(c *Command, r *http.Request, user *auth.UserState) Response {
	var req clusterActionRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode request body into cluster action: %v", err)
	}
	if decoder.More() {
		return BadRequest("extra content found in request body")
	}

	switch req.Action {
	case "assemble":
		// Basic validation of provided fields
		if req.Secret == "" {
			return BadRequest("secret must be provided")
		}
		if req.Address == "" {
			return BadRequest("address must be provided")
		}
		if req.ExpectedSize < 0 {
			return BadRequest("expected-size cannot be negative")
		}

		// Create the cluster assembly configuration
		config := clusterstate.AssembleConfig{
			Secret:       req.Secret,
			Address:      req.Address,
			ExpectedSize: req.ExpectedSize,
			Domain:       req.Domain,
		}

		// Create the task set using clusterstate.Assemble
		st := c.d.overlord.State()
		st.Lock()
		defer st.Unlock()

		ts, err := clusterstate.Assemble(st, config)
		if err != nil {
			return BadRequest("cannot create cluster assembly: %v", err)
		}

		chg := newChange(st, "assemble-cluster", "Create cluster assembly", []*state.TaskSet{ts}, nil)
		ensureStateSoon(st)
		return AsyncResponse(nil, chg.ID())
	default:
		return BadRequest("unsupported action %q", req.Action)
	}
}
