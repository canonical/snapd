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
	"fmt"
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/interfaces/prompting"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
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
		Actions:     []string{"allow", "deny"},
		ReadAccess:  interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
	}

	requestsRulesCmd = &Command{
		Path:       "/v2/interfaces/requests/rules",
		GET:        getRules,
		POST:       postRules,
		Actions:    []string{"add", "remove"},
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		// POST rules operates only on the rules for the user making the API
		// request, so there is no need for additional polkit authentication.
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
	}

	requestsRuleCmd = &Command{
		Path:       "/v2/interfaces/requests/rules/{id}",
		GET:        getRule,
		POST:       postRule,
		Actions:    []string{"patch", "remove"},
		ReadAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
		// POST rules operates only on the rules for the user making the API
		// request, so there is no need for additional polkit authentication.
		WriteAccess: interfaceOpenAccess{Interfaces: []string{"snap-interfaces-requests-control"}},
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

// isClientActivity returns true if the request comes a prompting handler
// service.
func isClientActivity(c *Command, r *http.Request) bool {
	// TODO: check that it's a handler service client making the API request
	return true
}

type invalidReason string

const (
	unsupportedValueReason invalidReason = "unsupported-value"
	parseErrorReason       invalidReason = "parse-error"
)

type invalidFieldValue struct {
	Reason invalidReason `json:"reason"`
	// Value is a []string for unsupported value errors and string for parse errors
	Value     any      `json:"value,omitempty"`
	Supported []string `json:"supported,omitempty"`
	// TODO: once documentation exists for user-defined fields
	// DocumentationURL string `json:"documentation"`
}

type promptingUnsupportedValueError prompting_errors.UnsupportedValueError

func (v *promptingUnsupportedValueError) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value[v.Field] = invalidFieldValue{
		Reason:    unsupportedValueReason,
		Value:     v.Value,
		Supported: v.Supported,
	}
	return json.Marshal(value)
}

type promptingParseError prompting_errors.ParseError

func (v *promptingParseError) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value[v.Field] = invalidFieldValue{
		Reason: parseErrorReason,
		Value:  v.Invalid,
		// TODO: once documentation exists for user-defined fields
		// DocumentationURL: <url>,
	}
	return json.Marshal(value)
}

type pathNotMatchedValue struct {
	Requested string `json:"requested-path"`
	Replied   string `json:"replied-pattern"`
}

type requestedPathNotMatchedError prompting_errors.RequestedPathNotMatchedError

func (v *requestedPathNotMatchedError) MarshalJSON() ([]byte, error) {
	value := make(map[string]pathNotMatchedValue, 1)
	value["path-pattern"] = pathNotMatchedValue{
		Requested: v.Requested,
		Replied:   v.Replied,
	}
	return json.Marshal(value)
}

type permissionsNotMatchedValue struct {
	Requested []string `json:"requested-permissions"`
	Replied   []string `json:"replied-permissions"`
}

type requestedPermissionsNotMatchedError prompting_errors.RequestedPermissionsNotMatchedError

func (v *requestedPermissionsNotMatchedError) MarshalJSON() ([]byte, error) {
	value := make(map[string]permissionsNotMatchedValue, 1)
	value["permissions"] = permissionsNotMatchedValue{
		Requested: v.Requested,
		Replied:   v.Replied,
	}
	return json.Marshal(value)
}

type promptingRuleConflict prompting_errors.RuleConflict

func (r *promptingRuleConflict) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Permission    string `json:"permission"`
		Variant       string `json:"variant"`
		ConflictingID string `json:"conflicting-id"`
	}{
		Permission:    r.Permission,
		Variant:       r.Variant,
		ConflictingID: r.ConflictingID,
	})
}

type promptingRuleConflictError prompting_errors.RuleConflictError

func (v *promptingRuleConflictError) MarshalJSON() ([]byte, error) {
	conflictsJSON := make([]promptingRuleConflict, len(v.Conflicts))
	for i, conflict := range v.Conflicts {
		conflictsJSON[i] = promptingRuleConflict(conflict)
	}
	return json.Marshal(&struct {
		Conflicts []promptingRuleConflict `json:"conflicts"`
	}{
		Conflicts: conflictsJSON,
	})
}

func promptingNotRunningError() *apiError {
	return &apiError{
		Status:  500, // Internal error
		Message: "AppArmor Prompting is not running",
		Kind:    client.ErrorKindAppArmorPromptingNotRunning,
	}
}

func promptingError(err error) *apiError {
	apiErr := &apiError{
		Message: err.Error(),
	}
	switch {
	case errors.Is(err, prompting_errors.ErrPromptNotFound):
		apiErr.Status = 404
		apiErr.Kind = client.ErrorKindInterfacesRequestsPromptNotFound
	case errors.Is(err, prompting_errors.ErrRuleNotFound) || errors.Is(err, prompting_errors.ErrRuleNotAllowed):
		// Even if the error is ErrRuleNotAllowed, reply with 404 status
		// to match the behavior of prompts, and so if we switch to storing
		// rules by ID (and don't want to check whether a rule with that ID
		// exists for some other user), this error will remain unchanged.
		apiErr.Status = 404
		apiErr.Kind = client.ErrorKindInterfacesRequestsRuleNotFound
	case errors.Is(err, prompting_errors.ErrUnsupportedValue):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var unsupportedValueErr *prompting_errors.UnsupportedValueError
		if errors.As(err, &unsupportedValueErr) {
			apiErr.Value = (*promptingUnsupportedValueError)(unsupportedValueErr)
		}
	case errors.Is(err, prompting_errors.ErrParseError):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var parseErr *prompting_errors.ParseError
		if errors.As(err, &parseErr) {
			apiErr.Value = (*promptingParseError)(parseErr)
		}
	case errors.Is(err, prompting_errors.ErrPatchedRuleHasNoPerms):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsPatchedRuleHasNoPermissions
	case errors.Is(err, prompting_errors.ErrNewSessionRuleNoSession):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsNewSessionRuleNoSession
	case errors.Is(err, prompting_errors.ErrReplyNotMatchRequestedPath):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsReplyNotMatchRequest
		var patternErr *prompting_errors.RequestedPathNotMatchedError
		if errors.As(err, &patternErr) {
			apiErr.Value = (*requestedPathNotMatchedError)(patternErr)
		}
	case errors.Is(err, prompting_errors.ErrReplyNotMatchRequestedPermissions):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsReplyNotMatchRequest
		var permissionsErr *prompting_errors.RequestedPermissionsNotMatchedError
		if errors.As(err, &permissionsErr) {
			apiErr.Value = (*requestedPermissionsNotMatchedError)(permissionsErr)
		}
	case errors.Is(err, prompting_errors.ErrRuleConflict):
		apiErr.Status = 409
		apiErr.Kind = client.ErrorKindInterfacesRequestsRuleConflict
		var conflictErr *prompting_errors.RuleConflictError
		if errors.As(err, &conflictErr) {
			apiErr.Value = (*promptingRuleConflictError)(conflictErr)
		}
	default:
		// Treat errors without specific mapping as internal errors.
		// These include:
		// - ErrPromptsClosed
		// - ErrRulesClosed
		// - ErrTooManyPrompts
		// - ErrRuleIDConflict
		// - ErrRuleDBInconsistent
		// - listener errors
		apiErr.Status = 500
	}
	return apiErr
}

type interfaceManager interface {
	AppArmorPromptingRunning() bool
	InterfacesRequestsManager() apparmorprompting.Manager
}

var getInterfaceManager = func(c *Command) interfaceManager {
	return c.d.overlord.InterfaceManager()
}

type postPromptBody struct {
	Outcome     prompting.OutcomeType       `json:"action"`
	Lifespan    prompting.LifespanType      `json:"lifespan"`
	Duration    string                      `json:"duration,omitempty"`
	Constraints *prompting.ReplyConstraints `json:"constraints"`
}

type addRuleContents struct {
	Snap        string                 `json:"snap"`
	Interface   string                 `json:"interface"`
	Constraints *prompting.Constraints `json:"constraints"`
}

type removeRulesSelector struct {
	Snap      string `json:"snap"`
	Interface string `json:"interface,omitempty"`
}

type patchRuleContents struct {
	Constraints *prompting.RuleConstraintsPatch `json:"constraints,omitempty"`
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
		return promptingNotRunningError()
	}

	clientActivity := isClientActivity(c, r)

	prompts, err := getInterfaceManager(c).InterfacesRequestsManager().Prompts(userID, clientActivity)
	if err != nil {
		return promptingError(err)
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
		return promptingError(prompting_errors.ErrPromptNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	clientActivity := isClientActivity(c, r)

	prompt, err := getInterfaceManager(c).InterfacesRequestsManager().PromptWithID(userID, promptID, clientActivity)
	if err != nil {
		return promptingError(err)
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
		return promptingError(prompting_errors.ErrPromptNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	var reply postPromptBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return promptingError(fmt.Errorf("cannot decode request body into prompt reply: %w", err))
	}

	clientActivity := isClientActivity(c, r)

	satisfiedPromptIDs, err := getInterfaceManager(c).InterfacesRequestsManager().HandleReply(userID, promptID, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration, clientActivity)
	if err != nil {
		return promptingError(err)
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
		return promptingNotRunningError()
	}

	query := r.URL.Query()
	snap := query.Get("snap")
	iface := query.Get("interface")

	rules, err := getInterfaceManager(c).InterfacesRequestsManager().Rules(userID, snap, iface)
	if err != nil {
		// Should be impossible, Rules() always returns nil error
		return promptingError(err)
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
		return promptingNotRunningError()
	}

	var postBody postRulesRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return promptingError(fmt.Errorf("cannot decode request body for rules endpoint: %w", err))
	}

	switch postBody.Action {
	case "add":
		if postBody.AddRule == nil {
			return BadRequest(`must include "rule" field in request body when action is "add"`)
		}
		newRule, err := getInterfaceManager(c).InterfacesRequestsManager().AddRule(userID, postBody.AddRule.Snap, postBody.AddRule.Interface, postBody.AddRule.Constraints)
		if err != nil {
			return promptingError(err)
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
			return promptingError(err)
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
		return promptingError(prompting_errors.ErrRuleNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	rule, err := getInterfaceManager(c).InterfacesRequestsManager().RuleWithID(userID, ruleID)
	if err != nil {
		return NotFound("%v", err)
	}

	return SyncResponse(rule)
}

func postRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	userID, errorResp := getUserID(r)
	if errorResp != nil {
		return errorResp
	}

	ruleID, err := prompting.IDFromString(id)
	if err != nil {
		return promptingError(prompting_errors.ErrRuleNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	var postBody postRuleRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return promptingError(fmt.Errorf("cannot decode request body into request rule modification or deletion: %w", err))
	}

	switch postBody.Action {
	case "patch":
		if postBody.PatchRule == nil {
			return BadRequest(`must include "rule" field in request body when action is "patch"`)
		}
		patchedRule, err := getInterfaceManager(c).InterfacesRequestsManager().PatchRule(userID, ruleID, postBody.PatchRule.Constraints)
		if err != nil {
			return promptingError(err)
		}
		return SyncResponse(patchedRule)
	case "remove":
		removedRule, err := getInterfaceManager(c).InterfacesRequestsManager().RemoveRule(userID, ruleID)
		if err != nil {
			return promptingError(err)
		}
		return SyncResponse(removedRule)
	default:
		return BadRequest(`action must be "add" or "remove"`)
	}
}
