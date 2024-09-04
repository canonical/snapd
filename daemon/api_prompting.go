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
	"github.com/snapcore/snapd/interfaces/prompting/errortypes"
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

type invalidReason string

const (
	unsupportedValueReason invalidReason = "unsupported-value"
	parseErrorReason       invalidReason = "parse-error"
	validationErrorReason  invalidReason = "validation-error"
)

type invalidFieldValue struct {
	Reason   invalidReason `json:"invalid-reason"`
	Metadata interface{}   `json:"metadata,omitempty"`
}

type UnsupportedValueValue struct {
	err *errortypes.UnsupportedValueError
}

func (v *UnsupportedValueValue) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value[v.err.Field] = invalidFieldValue{
		Reason: unsupportedValueReason,
		Metadata: &struct {
			Received  interface{} `json:"received-invalid"`
			Supported []string    `json:"supported"`
		}{
			Received:  v.err.Unsupported,
			Supported: v.err.Supported,
		},
	}
	return json.Marshal(value)
}

type ParseErrorValue struct {
	err *errortypes.ParseError
}

func (v *ParseErrorValue) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value[v.err.Field] = invalidFieldValue{
		Reason: parseErrorReason,
		Metadata: &struct {
			Received string `json:"received-invalid"`
			// TODO: once documentation exists for user-defined fields
			// DocumentationURL string `json:"documentation"`
		}{
			Received: v.err.Invalid,
			// TODO: once documentation exists for user-defined fields
			// DocumentationURL: <url>,
		},
	}
	return json.Marshal(value)
}

type validationErrorType string

var (
	requestedPathNotMatched        validationErrorType = "requested-path-not-matched"
	requestedPermissionsNotMatched validationErrorType = "requested-permissions-not-matched"
	ruleConflict                   validationErrorType = "conflict-with-existing-rule"
)

type RequestedPathNotMatchedErrorValue struct {
	err *prompting.RequestedPathNotMatchedError
}

func (v *RequestedPathNotMatchedErrorValue) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value["path-pattern"] = invalidFieldValue{
		Reason: validationErrorReason,
		Metadata: &struct {
			ErrorType validationErrorType `json:"error-type"`
			Received  string              `json:"received"`
			Requested string              `json:"requested"`
		}{
			ErrorType: requestedPathNotMatched,
			Received:  v.err.Received,
			Requested: v.err.Requested,
		},
	}
	return json.Marshal(value)
}

type RequestedPermissionsNotMatchedErrorValue struct {
	err *prompting.RequestedPermissionsNotMatchedError
}

func (v *RequestedPermissionsNotMatchedErrorValue) MarshalJSON() ([]byte, error) {
	value := make(map[string]invalidFieldValue, 1)
	value["permissions"] = invalidFieldValue{
		Reason: validationErrorReason,
		Metadata: &struct {
			ErrorType validationErrorType `json:"error-type"`
			Received  []string            `json:"received"`
			Requested []string            `json:"requested"`
		}{
			ErrorType: requestedPermissionsNotMatched,
			Received:  v.err.Received,
			Requested: v.err.Requested,
		},
	}
	return json.Marshal(value)
}

type RuleConflictJSON prompting.RuleConflict

func (r *RuleConflictJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Permission    string           `json:"permission"`
		Variant       string           `json:"variant"`
		ConflictingID prompting.IDType `json:"conflicting-id"`
	}{
		Permission:    r.Permission,
		Variant:       r.Variant,
		ConflictingID: r.ConflictingID,
	})
}

type RuleConflictErrorValue struct {
	err *prompting.RuleConflictError
}

func (v *RuleConflictErrorValue) MarshalJSON() ([]byte, error) {
	conflictsJSON := make([]RuleConflictJSON, len(v.err.Conflicts))
	for i, conflict := range v.err.Conflicts {
		conflictsJSON[i] = RuleConflictJSON(conflict)
	}
	value := make(map[string]invalidFieldValue, 1)
	value["path-pattern"] = invalidFieldValue{
		Reason: validationErrorReason,
		Metadata: &struct {
			ErrorType validationErrorType `json:"error-type"`
			Conflicts []RuleConflictJSON  `json:"conflicts"`
		}{
			ErrorType: ruleConflict,
			Conflicts: conflictsJSON,
		},
	}
	return json.Marshal(value)
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
	case errors.Is(err, prompting.ErrPromptNotFound):
		apiErr.Status = 404
		apiErr.Kind = client.ErrorKindInterfacesRequestsPromptNotFound
	case errors.Is(err, prompting.ErrRuleNotFound) || errors.Is(err, prompting.ErrRuleNotAllowed):
		// Even if the error is ErrRuleNotAllowed, reply with 404 status
		// to match the behavior of prompts, and so if we switch to storing
		// rules by ID (and don't want to check whether a rule with that ID
		// exists for some other user), this error will remain unchanged.
		apiErr.Status = 404
		apiErr.Kind = client.ErrorKindInterfacesRequestsRuleNotFound
	case errors.Is(err, errortypes.ErrUnsupportedValue):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var unsupportedValueErr *errortypes.UnsupportedValueError
		if errors.As(err, &unsupportedValueErr) {
			apiErr.Value = UnsupportedValueValue{
				err: unsupportedValueErr,
			}
		}
	case errors.Is(err, errortypes.ErrParseError):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var parseErr *errortypes.ParseError
		if errors.As(err, &parseErr) {
			apiErr.Value = ParseErrorValue{
				err: parseErr,
			}
		}
	case errors.Is(err, prompting.ErrReplyNotMatchRequestedPath):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var patternErr *prompting.RequestedPathNotMatchedError
		if errors.As(err, &patternErr) {
			apiErr.Value = &RequestedPathNotMatchedErrorValue{
				err: patternErr,
			}
		}
	case errors.Is(err, prompting.ErrReplyNotMatchRequestedPermissions):
		apiErr.Status = 400
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var permissionsErr *prompting.RequestedPermissionsNotMatchedError
		if errors.As(err, &permissionsErr) {
			apiErr.Value = &RequestedPermissionsNotMatchedErrorValue{
				err: permissionsErr,
			}
		}
	case errors.Is(err, prompting.ErrRuleConflict):
		apiErr.Status = 409
		apiErr.Kind = client.ErrorKindInterfacesRequestsInvalidFields
		var conflictErr *prompting.RuleConflictError
		if errors.As(err, &conflictErr) {
			apiErr.Value = &RuleConflictErrorValue{
				err: conflictErr,
			}
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
		return promptingNotRunningError()
	}

	prompts, err := getInterfaceManager(c).InterfacesRequestsManager().Prompts(userID)
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
		return promptingError(prompting.ErrPromptNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	prompt, err := getInterfaceManager(c).InterfacesRequestsManager().PromptWithID(userID, promptID)
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
		return promptingError(prompting.ErrPromptNotFound)
	}

	if !getInterfaceManager(c).AppArmorPromptingRunning() {
		return promptingNotRunningError()
	}

	var reply postPromptBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reply); err != nil {
		return promptingError(fmt.Errorf("cannot decode request body into prompt reply: %w", err))
	}

	satisfiedPromptIDs, err := getInterfaceManager(c).InterfacesRequestsManager().HandleReply(userID, promptID, reply.Constraints, reply.Outcome, reply.Lifespan, reply.Duration)
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
		newRule, err := getInterfaceManager(c).InterfacesRequestsManager().AddRule(userID, postBody.AddRule.Snap, postBody.AddRule.Interface, postBody.AddRule.Constraints, postBody.AddRule.Outcome, postBody.AddRule.Lifespan, postBody.AddRule.Duration)
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
		return promptingError(prompting.ErrRuleNotFound)
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
		return promptingError(prompting.ErrRuleNotFound)
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
		patchedRule, err := getInterfaceManager(c).InterfacesRequestsManager().PatchRule(userID, ruleID, postBody.PatchRule.Constraints, postBody.PatchRule.Outcome, postBody.PatchRule.Lifespan, postBody.PatchRule.Duration)
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
