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
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
	}

	promptRequestCmd = &Command{
		Path:        "/v2/prompting/requests/{id}",
		GET:         getRequest,
		POST:        postRequest,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
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

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	if follow {
		// TODO: provide a way to stop these when the daemon stops.
		// XXX: is there a way to tell when the connection has been closed by
		// the UI client? Can't let requestsCh be closed by the daemon, that
		// would cause a panic when the prompting manager tries to write or
		// close it.
		jsonSeqResp, requestsCh := newFollowRequestsSeqResponse()
		// TODO: implement the following:
		// respWriter := c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRequestsChan(int(ucred.Uid), requestsCh)
		// When daemon stops, call respWriter.Stop()
		_ = c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRequestsChan(int(ucred.Uid), requestsCh)
		return jsonSeqResp
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequests(int(ucred.Uid))
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func getRequest(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptClient(user) {
		return userNotAllowedPromptClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequest(int(ucred.Uid), id)
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

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	var reply apparmorprompting.PromptReply
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return BadRequest("cannot decode request body into prompt reply: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().PostRequest(int(ucred.Uid), id, &reply)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}
