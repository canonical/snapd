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
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
)

var (
	promptRequestsCmd = &Command{
		Path:       "/v2/prompting/requests",
		GET:        getRequests,
		ReadAccess: promptingOpenAccess{},
		// TODO: make this authenticatedAccess{Polkit: polkitActionPrompting}
		// Need to add polkitActionPrompting to daemon/api.go and register
		// prompt UI clients with it.
	}

	promptRequestCmd = &Command{
		Path:        "/v2/prompting/requests/{id}",
		GET:         getRequest,
		POST:        postRequest,
		ReadAccess:  promptingOpenAccess{},
		WriteAccess: promptingOpenAccess{},
		// TODO: make these authenticatedAccess{Polkit: polkitActionPrompting}
		// Need to add polkitActionPrompting to daemon/api.go and register
		// prompt UI clients with it.
	}
)

func userAllowedPromptClient(user *auth.UserState) bool {
	// Check that the user is authorized to be a prompt UI client
	return true // TODO: actually check
}

func userNotAllowedPromptClientResponse(user *auth.UserState) Response {
	// The user is not authorized to be a prompt UI client
	// TODO: fix this
	return SyncResponse("user not allowed")
}

func getRequests(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptClient(user) {
		return userNotAllowedPromptClientResponse(user)
	}

	query := r.URL.Query()
	follow := false
	if s := query.Get("follow"); s != "" {
		f, err := strconv.ParseBool(s)
		if err != nil {
			return BadRequest("invalid value for follow: %q: %v", s, err)
		}
		follow = f
	}
	if follow {
		// TODO: do something as a result of follow=true to receive requests
		// created for the corresponding user in the future and forward them over
		// this connection.
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequests(userID)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result) // TODO: should this be async for follow=true?
}

func getRequest(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptClient(user) {
		return userNotAllowedPromptClientResponse(user)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequest(userID, id)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRequest(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptClient(user) {
		return userNotAllowedPromptClientResponse(user)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	var reply apparmorprompting.PromptReply
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return BadRequest("cannot decode request body into prompt reply: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().PostRequest(userID, id, &reply)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}
