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
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
)

var (
	promptingRequestsCmd = &Command{
		Path:       "/v2/interfaces/prompting/requests",
		GET:        getRequests,
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
	}

	promptingRequestCmd = &Command{
		Path:        "/v2/interfaces/prompting/requests/{id}",
		GET:         getRequest,
		POST:        postRequest,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
	}

	promptingRulesCmd = &Command{
		Path:        "/v2/interfaces/prompting/rules",
		GET:         getRules,
		POST:        postRules,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceAuthenticatedAccess{Interfaces: []string{"snap-prompting-control"}, Polkit: polkitActionManage},
	}

	promptingRuleCmd = &Command{
		Path:        "/v2/interfaces/prompting/rules/{id}",
		GET:         getRule,
		POST:        postRule,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceAuthenticatedAccess{Interfaces: []string{"snap-prompting-control"}, Polkit: polkitActionManage},
	}
)

func userAllowedPromptingClient(user *auth.UserState) bool {
	// Check that the user is authorized to be a prompting client
	return true // TODO: actually check
}

func userNotAllowedPromptingClientResponse(user *auth.UserState) Response {
	// The user is not authorized to be a prompt UI client
	// TODO: fix this
	return SyncResponse("user not allowed")
}

func getRequests(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
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
		// respWriter := c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRequestsChan(ucred.Uid, requestsCh)
		// When daemon stops, call respWriter.Stop()
		_ = c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRequestsChan(ucred.Uid, requestsCh)
		return jsonSeqResp
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequests(ucred.Uid)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func getRequest(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRequest(ucred.Uid, id)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRequest(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
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

	result, err := c.d.overlord.InterfaceManager().Prompting().PostRequest(ucred.Uid, id, &reply)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func getRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	query := r.URL.Query()

	snap := query.Get("snap")
	app := query.Get("app")

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
		jsonSeqResp, rulesCh := newFollowRulesSeqResponse()
		// TODO: implement the following:
		// respWriter := c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRulesChan(ucred.Uid, snap, app, rulesCh)
		// When daemon stops, call respWriter.Stop()
		_ = c.d.overlord.InterfaceManager().Prompting().RegisterAndPopulateFollowRulesChan(ucred.Uid, snap, app, rulesCh)
		return jsonSeqResp
	}

	if app != "" && snap == "" {
		return BadRequest("app parameter provided, must also provide snap parameter")
	}
	result, err := c.d.overlord.InterfaceManager().Prompting().GetRules(ucred.Uid, snap, app)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	var postBody apparmorprompting.PostRulesRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into prompting rule contents: %v", err)
	}

	switch postBody.Action {
	case "create":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesCreate(ucred.Uid, postBody.CreateRules)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	case "delete":
		for _, selector := range postBody.DeleteSelectors {
			snap := selector.Snap
			if snap == "" {
				return BadRequest(`must include "snap" parameter in "selectors"`)
			}
		}
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesDelete(ucred.Uid, postBody.DeleteSelectors)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	default:
		return BadRequest(`action must "create" or "delete"`)
	}
}

func getRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRule(ucred.Uid, id)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	var postBody apparmorprompting.PostRuleRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into prompting rule modification or deletion: %v", err)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	switch postBody.Action {
	case "modify":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRuleModify(ucred.Uid, id, postBody.Rule)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	case "delete":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRuleDelete(ucred.Uid, id)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	default:
		return BadRequest(`action must be "create" or "delete"`)
	}
}
