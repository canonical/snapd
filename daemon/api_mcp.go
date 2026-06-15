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

package daemon

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/auth"
)

// maxMCPRequestBodyBytes limits the size of the request body for MCP requests
// to prevent abuse. The actual limit can be adjusted as needed.
const maxMCPRequestBodyBytes = 1024 * 1024

var mcpCmd = &Command{
	Path: "/v2/mcp",
	POST: postMCP,
	// This part is tricky as we want to allow both unauthenticated access
	// (e.g. snap list, snap info equivalents) and authenticated access (e.g.
	// snap install, snap remove equivalents).  For now everything is
	// authenticated for simplicity. This makes the first request in an MCP
	// session pull up the polkit request.
	//
	// As an alternative we could have more fine grained access control by
	// attaching meta-data to individual resources and tools and only require
	// authentication for those that need it.
	WriteAccess: authenticatedAccess{Polkit: polkitActionUseMCP},
}

func postMCP(c *Command, r *http.Request, user *auth.UserState) Response {
	_ = c
	_ = user

	st := c.d.state
	st.Lock()
	flagErr := validateFeatureFlag(st, features.MCP)
	st.Unlock()
	if flagErr != nil {
		return flagErr
	}

	// Read the request body up to the limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPRequestBodyBytes+1))
	if err != nil {
		return BadRequest("cannot read request body: %v", err)
	}
	if len(body) > maxMCPRequestBodyBytes {
		return BadRequest("request body too large (limit %d bytes)", maxMCPRequestBodyBytes)
	}

	mcpMgr := c.d.overlord.ModelContextProtocolManager()
	if mcpMgr == nil {
		return InternalError("cannot process MCP request: mcp manager is not initialized")
	}

	// Process the request and get response.
	response, err := mcpMgr.ProcessRequest(r.Context(), body)
	if err != nil {
		return InternalError("cannot process MCP request: %v", err)
	}

	// Notifications (no id in request) produce no response.
	if response == nil {
		return SyncResponse(nil)
	}

	// Return the response as a raw JSON message wrapped in the standard response
	var mcpResp any
	if err := json.Unmarshal(response, &mcpResp); err != nil {
		return InternalError("cannot parse MCP response: %v", err)
	}

	return SyncResponse(mcpResp)
}
