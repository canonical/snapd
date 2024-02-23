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

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
)

var (
	requestsPromptsCmd = &Command{
		Path:       "/v2/interfaces/requests/prompts",
		GET:        getPrompts,
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
	}

	requestsPromptCmd = &Command{
		Path:        "/v2/interfaces/requests/prompts/{id}",
		GET:         getPrompt,
		POST:        postPrompt,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
	}

	requestsRulesCmd = &Command{
		Path:        "/v2/interfaces/requests/rules",
		GET:         getRules,
		POST:        postRules,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-prompting-control"}},
		WriteAccess: interfaceAuthenticatedAccess{Interfaces: []string{"snap-prompting-control"}, Polkit: polkitActionManage},
	}

	requestsRuleCmd = &Command{
		Path:        "/v2/interfaces/requests/rules/{id}",
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

type postRulesRequestBody struct {
	Action         string                                 `json:"action"`
	AddRule        *apparmorprompting.AddRuleContents     `json:"rule,omitempty"`
	RemoveSelector *apparmorprompting.RemoveRulesSelector `json:"selector,omitempty"`
}

type postRuleRequestBody struct {
	Action    string                               `json:"action"`
	PatchRule *apparmorprompting.PatchRuleContents `json:"rule,omitempty"`
}

func getPrompts(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetPrompts(ucred.Uid)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func getPrompt(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetPrompt(ucred.Uid, id)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postPrompt(c *Command, r *http.Request, user *auth.UserState) Response {
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

	result, err := c.d.overlord.InterfaceManager().Prompting().PostPrompt(ucred.Uid, id, &reply)
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
	iface := query.Get("interface")

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	if iface != "" && snap == "" {
		return BadRequest(`"interface" field provided, must also provide "snap" field`)
	}
	result, err := c.d.overlord.InterfaceManager().Prompting().GetRules(ucred.Uid, snap, iface)
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

	var postBody postRulesRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body for rules endpoint: %v", err)
	}

	switch postBody.Action {
	case "add":
		if postBody.AddRule == nil {
			return BadRequest(`must include "rule" field in request body when action is "add"`)
		}
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesAdd(ucred.Uid, postBody.AddRule)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	case "remove":
		if postBody.RemoveSelector == nil {
			return BadRequest(`must include "selector" field in request body when action is "remove"`)
		}
		if postBody.RemoveSelector.Snap == "" {
			return BadRequest(`must include "snap" field in "selector"`)
		}
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesRemove(ucred.Uid, postBody.RemoveSelector)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	default:
		return BadRequest(`"action" field must be "create" or "remove"`)
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

	var postBody postRuleRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into request rule modification or deletion: %v", err)
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return Forbidden("cannot get remote user: %v", err)
	}

	switch postBody.Action {
	case "patch":
		if postBody.PatchRule == nil {
			return BadRequest(`must include "rule" field in request body when action is "patch"`)
		}
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulePatch(ucred.Uid, id, postBody.PatchRule)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	case "remove":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRuleRemove(ucred.Uid, id)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	default:
		return BadRequest(`action must be "add" or "remove"`)
	}
}
