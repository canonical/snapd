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
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/common"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestprompts"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting/requestrules"
	"github.com/snapcore/snapd/strutil"
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
	return Forbidden("user not allowed")
}

// getUserID returns the UID specified by the user-id parameter of the query,
// otherwise the UID of the connection.
//
// Only admin users are allowed to use the user-id parameter.
//
// If an error occurs, returns an error response, otherwise returns the user ID
// and a nil response.
func getUserID(r *http.Request) (uint32, Response) {
	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return 0, Forbidden("cannot get remote user: %v", err)
	}
	reqUID := ucred.Uid
	query := r.URL.Query()
	if len(query["user-id"]) == 0 {
		return reqUID, nil
	}
	if reqUID != 0 {
		return 0, Forbidden(`only admins may use the "user-id" parameter`)
	}
	prefix := `invalid "user-id" parameter`
	queryUserIDs := strutil.MultiCommaSeparatedList(query["user-id"])
	if len(queryUserIDs) != 1 {
		return 0, BadRequest(`%v: must only include one "user-id"`, prefix)
	}
	userIDInt, err := strconv.ParseInt(queryUserIDs[0], 10, 64)
	if err != nil {
		return 0, BadRequest("%v: %v", prefix, err)
	}
	if userIDInt < 0 || userIDInt > math.MaxUint32 {
		return 0, BadRequest("%v: user ID is not a valid uint32: %d", prefix, userIDInt)
	}
	return uint32(userIDInt), nil
}

type postPromptBody struct {
	Outcome     common.OutcomeType  `json:"action"`
	Lifespan    common.LifespanType `json:"lifespan"`
	Duration    string              `json:"duration,omitempty"`
	Constraints *common.Constraints `json:"constraints"`
}

type addRuleContents struct {
	Snap        string              `json:"snap"`
	Interface   string              `json:"interface"`
	Constraints *common.Constraints `json:"constraints"`
	Outcome     common.OutcomeType  `json:"outcome"`
	Lifespan    common.LifespanType `json:"lifespan"`
	Duration    string              `json:"duration,omitempty"`
}

type removeRulesSelector struct {
	Snap      string `json:"snap"`
	Interface string `json:"interface,omitempty"`
}

type patchRuleContents struct {
	Constraints *common.Constraints `json:"constraints,omitempty"`
	Outcome     common.OutcomeType  `json:"outcome,omitempty"`
	Lifespan    common.LifespanType `json:"lifespan,omitempty"`
	Duration    string              `json:"duration,omitempty"`
}

type postRulesRequestBody struct {
	Action         string               `json:"action"`
	AddRule        *addRuleContents     `json:"rule,omitempty"`
	RemoveSelector *removeRulesSelector `json:"selector,omitempty"`
}

type postRuleRequestBody struct {
	Action    string             `json:"action"`
	PatchRule *patchRuleContents `json:"rule,omitempty"`
}

func getPrompts(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	prompts, err := c.d.overlord.InterfaceManager().Prompting().GetPrompts(userID)
	if err != nil {
		return InternalError("%v", err)
	}
	if len(prompts) == 0 {
		prompts = []*requestprompts.Prompt{}
	}

	return SyncResponse(prompts)
}

func getPrompt(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	prompt, err := c.d.overlord.InterfaceManager().Prompting().GetPromptWithID(userID, id)
	if errors.Is(err, apparmorprompting.ErrPromptingNotEnabled) {
		return InternalError("%v", err)
	} else if err != nil {
		return NotFound("%v", err)
	}

	return SyncResponse(prompt)
}

func postPrompt(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	var reply postPromptBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return BadRequest("cannot decode request body into prompt reply: %v", err)
	}

	satisfiedPromptIDs, err := c.d.overlord.InterfaceManager().Prompting().HandleReply(userID, id, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration)
	// TODO: once rebased on master, add to the following:
	//if errors.Is(err, requestprompts.ErrClosed) {
	//	return InternalError("%v", err)
	//}
	if errors.Is(err, requestprompts.ErrUserNotFound) || errors.Is(err, requestprompts.ErrPromptIDNotFound) {
		return NotFound("%v", err)
	} else if errors.Is(err, requestrules.ErrPathPatternConflict) {
		return Conflict("%v", err)
	} else if err != nil {
		return BadRequest("%v", err)
	}

	return SyncResponse(satisfiedPromptIDs)
}

func getRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	query := r.URL.Query()
	snap := query.Get("snap")
	iface := query.Get("interface")

	rules, err := c.d.overlord.InterfaceManager().Prompting().GetRules(userID, snap, iface)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(rules)
}

func postRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
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
		newRule, err := c.d.overlord.InterfaceManager().Prompting().AddRule(userID, postBody.AddRule.Snap, postBody.AddRule.Interface, postBody.AddRule.Constraints, postBody.AddRule.Outcome, postBody.AddRule.Lifespan, postBody.AddRule.Duration)
		if errors.Is(err, requestrules.ErrPathPatternConflict) {
			return Conflict("%v", err)
		} else if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(newRule)
	case "remove":
		if postBody.RemoveSelector == nil {
			return BadRequest(`must include "selector" field in request body when action is "remove"`)
		}
		if postBody.RemoveSelector.Snap == "" {
			return BadRequest(`must include "snap" field in "selector"`)
		}
		removedRules := c.d.overlord.InterfaceManager().Prompting().RemoveRules(userID, postBody.RemoveSelector.Snap, postBody.RemoveSelector.Interface)
		return SyncResponse(removedRules)
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

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	rule, err := c.d.overlord.InterfaceManager().Prompting().GetRule(userID, id)
	if err != nil {
		return NotFound("%v", err)
	}

	return SyncResponse(rule)
}

func postRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedPromptingClient(user) {
		return userNotAllowedPromptingClientResponse(user)
	}

	if !apparmorprompting.PromptingEnabled() {
		return InternalError("Apparmor Prompting is not enabled")
	}

	var postBody postRuleRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into request rule modification or deletion: %v", err)
	}

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	switch postBody.Action {
	case "patch":
		if postBody.PatchRule == nil {
			return BadRequest(`must include "rule" field in request body when action is "patch"`)
		}
		patchedRule, err := c.d.overlord.InterfaceManager().Prompting().PatchRule(userID, id, postBody.PatchRule.Constraints, postBody.PatchRule.Outcome, postBody.PatchRule.Lifespan, postBody.PatchRule.Duration)
		if errors.Is(err, requestrules.ErrRuleIDNotFound) || errors.Is(err, requestrules.ErrUserNotAllowed) {
			return NotFound("%v", err)
		} else if errors.Is(err, requestrules.ErrInternalInconsistency) {
			return InternalError("%v", err)
		} else if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(patchedRule)
	case "remove":
		removedRule, err := c.d.overlord.InterfaceManager().Prompting().RemoveRule(userID, id)
		if errors.Is(err, requestrules.ErrRuleIDNotFound) || errors.Is(err, requestrules.ErrUserNotAllowed) {
			return NotFound("%v", err)
		} else if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(removedRule)
	default:
		return BadRequest(`action must be "add" or "remove"`)
	}
}
