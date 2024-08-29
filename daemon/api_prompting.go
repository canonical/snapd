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
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/interfaces/prompting/requestprompts"
	"github.com/snapcore/snapd/interfaces/prompting/requestrules"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate/apparmorprompting"
	"github.com/snapcore/snapd/strutil"
)

var (
	requestsPromptsCmd = &Command{
		Path:       "/v2/interfaces/requests/prompts",
		GET:        getPrompts,
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
	}

	requestsPromptCmd = &Command{
		Path:        "/v2/interfaces/requests/prompts/{id}",
		GET:         getPrompt,
		POST:        postPrompt,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
	}

	requestsRulesCmd = &Command{
		Path:        "/v2/interfaces/requests/rules",
		GET:         getRules,
		POST:        postRules,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		WriteAccess: interfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: polkitActionManage},
	}

	requestsRuleCmd = &Command{
		Path:        "/v2/interfaces/requests/rules/{id}",
		GET:         getRule,
		POST:        postRule,
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		WriteAccess: interfaceAuthenticatedAccess{Interfaces: []string{"snap-interfaces-requests-control"}, Polkit: polkitActionManage},
	}
)

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
	const prefix = `invalid "user-id" parameter`
	queryUserIDs := strutil.MultiCommaSeparatedList(query["user-id"])
	if len(queryUserIDs) != 1 {
		return 0, BadRequest(`%s: must only include one "user-id"`, prefix)
	}
	userIDInt, err := strconv.ParseUint(queryUserIDs[0], 10, 32)
	if err != nil {
		return 0, BadRequest("%s: user ID is not a valid uint32: %d", prefix, userIDInt)
	}
	return uint32(userIDInt), nil
}

type interfaceManager interface {
	AppArmorPromptingRunning() bool
	InterfacesRequestsManager() apparmorprompting.Manager
}

var getInterfaceManager = func(c *Command) interfaceManager {
	return c.d.overlord.InterfaceManager()
}

type postPromptBody struct {
	Outcome     prompting.OutcomeType  `json:"action"`
	Lifespan    prompting.LifespanType `json:"lifespan"`
	Duration    string                 `json:"duration,omitempty"`
	Constraints *prompting.Constraints `json:"constraints"`
}

type addRuleContents struct {
	Snap        string                 `json:"snap"`
	Interface   string                 `json:"interface"`
	Constraints *prompting.Constraints `json:"constraints"`
	Outcome     prompting.OutcomeType  `json:"outcome"`
	Lifespan    prompting.LifespanType `json:"lifespan"`
	Duration    string                 `json:"duration,omitempty"`
}

type removeRulesSelector struct {
	Snap      string `json:"snap"`
	Interface string `json:"interface,omitempty"`
}

type patchRuleContents struct {
	Constraints *prompting.Constraints `json:"constraints,omitempty"`
	Outcome     prompting.OutcomeType  `json:"outcome,omitempty"`
	Lifespan    prompting.LifespanType `json:"lifespan,omitempty"`
	Duration    string                 `json:"duration,omitempty"`
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
	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
	}

	prompts, err := getInterfaceManager(c).InterfacesRequestsManager().Prompts(userID)
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

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	promptID, err := prompting.IDFromString(id)
	if err != nil {
		return BadRequest("%v", err)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
	}

	prompt, err := getInterfaceManager(c).InterfacesRequestsManager().PromptWithID(userID, promptID)
	if errors.Is(err, requestprompts.ErrNotFound) {
		return NotFound("%v", err)
	} else if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(prompt)
}

func postPrompt(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	promptID, err := prompting.IDFromString(id)
	if err != nil {
		return BadRequest("%v", err)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
	}

	var reply postPromptBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return BadRequest("cannot decode request body into prompt reply: %v", err)
	}

	satisfiedPromptIDs, err := getInterfaceManager(c).InterfacesRequestsManager().HandleReply(userID, promptID, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration)
	if errors.Is(err, requestprompts.ErrClosed) || errors.Is(err, requestrules.ErrInternalInconsistency) {
		return InternalError("%v", err)
	} else if errors.Is(err, requestprompts.ErrNotFound) {
		return NotFound("%v", err)
	} else if errors.Is(err, requestrules.ErrPathPatternConflict) {
		return Conflict("%v", err)
	} else if err != nil {
		return BadRequest("%v", err)
	}

	if len(satisfiedPromptIDs) == 0 {
		satisfiedPromptIDs = []prompting.IDType{}
	}

	return SyncResponse(satisfiedPromptIDs)
}

func getRules(c *Command, r *http.Request, user *auth.UserState) Response {
	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
	}

	query := r.URL.Query()
	snap := query.Get("snap")
	iface := query.Get("interface")

	rules, err := getInterfaceManager(c).InterfacesRequestsManager().Rules(userID, snap, iface)
	if err != nil {
		return InternalError("%v", err)
	}

	if len(rules) == 0 {
		rules = []*requestrules.Rule{}
	}

	return SyncResponse(rules)
}

func postRules(c *Command, r *http.Request, user *auth.UserState) Response {
	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
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
		newRule, err := getInterfaceManager(c).InterfacesRequestsManager().AddRule(userID, postBody.AddRule.Snap, postBody.AddRule.Interface, postBody.AddRule.Constraints, postBody.AddRule.Outcome, postBody.AddRule.Lifespan, postBody.AddRule.Duration)
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
		if postBody.RemoveSelector.Snap == "" && postBody.RemoveSelector.Interface == "" {
			return BadRequest(`must include "snap" and/or "interface" field in "selector"`)
		}
		removedRules, err := getInterfaceManager(c).InterfacesRequestsManager().RemoveRules(userID, postBody.RemoveSelector.Snap, postBody.RemoveSelector.Interface)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(removedRules)
	default:
		return BadRequest(`"action" field must be "create" or "remove"`)
	}
}

func getRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	ruleID, err := prompting.IDFromString(id)
	if err != nil {
		return BadRequest("%v", err)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
	}

	rule, err := getInterfaceManager(c).InterfacesRequestsManager().RuleWithID(userID, ruleID)
	if err != nil {
		// Even if the error is ErrUserNotAllowed, reply with NotFound response
		// to match the behavior of prompts, and so if we switch to storing
		// rules by ID (and don't want to check whether a rule with that ID
		// exists for some other user), this error will remain unchanged.
		return NotFound("%v", err)
	}

	return SyncResponse(rule)
}

func postRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	ruleID, err := prompting.IDFromString(id)
	if err != nil {
		return BadRequest("%v", err)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return InternalError("Apparmor Prompting is not running")
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
		patchedRule, err := getInterfaceManager(c).InterfacesRequestsManager().PatchRule(userID, ruleID, postBody.PatchRule.Constraints, postBody.PatchRule.Outcome, postBody.PatchRule.Lifespan, postBody.PatchRule.Duration)
		if errors.Is(err, requestrules.ErrRuleIDNotFound) || errors.Is(err, requestrules.ErrUserNotAllowed) {
			return NotFound("%v", err)
		} else if errors.Is(err, requestrules.ErrInternalInconsistency) || errors.Is(err, requestrules.ErrClosed) {
			return InternalError("%v", err)
		} else if err != nil {
			return BadRequest("%v", err)
		}
		return SyncResponse(patchedRule)
	case "remove":
		removedRule, err := getInterfaceManager(c).InterfacesRequestsManager().RemoveRule(userID, ruleID)
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
