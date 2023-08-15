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
	accessRulesCmd = &Command{
		Path:        "/v2/access-control/rules",
		GET:         getRules,
		POST:        postRules,
		ReadAccess:  openAccess{},
		WriteAccess: openAccess{},
		// TODO: make this authenticatedAccess{Polkit: polkitActionPrompting}
		// Need to add polkitActionPrompting to daemon/api.go and register
		// prompt UI clients with it.
	}

	accessRuleCmd = &Command{
		Path:        "/v2/access-control/rules/{id}",
		GET:         getRule,
		POST:        postRule,
		ReadAccess:  openAccess{},
		WriteAccess: openAccess{},
		// TODO: make these authenticatedAccess{Polkit: polkitActionPrompting}
		// Need to add polkitActionPrompting to daemon/api.go and register
		// prompt UI clients with it.
	}
)

func userAllowedAccessControlClient(user *auth.UserState) bool {
	// Check that the user is authorized to be a access rules settings client
	return true // TODO: actually check
}

func userNotAllowedAccessControlClientResponse(user *auth.UserState) Response {
	// The user is not authorized to be a access rules settings client
	// TODO: fix this
	return SyncResponse("user not allowed")
}

func getRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedAccessControlClient(user) {
		return userNotAllowedAccessControlClientResponse(user)
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

	snap := query.Get("snap")
	app := query.Get("app")

	var userID int
	if user != nil {
		userID = user.ID
	}

	if app != "" && snap == "" {
		return BadRequest("app parameter provided, must also provide snap parameter")
	}
	result, err := c.d.overlord.InterfaceManager().Prompting().GetRules(userID, snap, app)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRules(c *Command, r *http.Request, user *auth.UserState) Response {
	if !userAllowedAccessControlClient(user) {
		return userNotAllowedAccessControlClientResponse(user)
	}

	var postBody apparmorprompting.PostRulesRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into access rule contents: %v", err)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	switch postBody.Action {
	case "create":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesCreate(userID, postBody.CreateRules)
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
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRulesDelete(userID, postBody.DeleteSelectors)
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

	if !userAllowedAccessControlClient(user) {
		return userNotAllowedAccessControlClientResponse(user)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	result, err := c.d.overlord.InterfaceManager().Prompting().GetRule(userID, id)
	if err != nil {
		return InternalError("%v", err)
	}

	return SyncResponse(result)
}

func postRule(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	id := vars["id"]

	if !userAllowedAccessControlClient(user) {
		return userNotAllowedAccessControlClientResponse(user)
	}

	var postBody apparmorprompting.PostRuleRequestBody
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postBody); err != nil {
		return BadRequest("cannot decode request body into access rule modification or deletion: %v", err)
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	switch postBody.Action {
	case "modify":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRuleModify(userID, id, postBody.Rule)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	case "delete":
		result, err := c.d.overlord.InterfaceManager().Prompting().PostRuleDelete(userID, id)
		if err != nil {
			return InternalError("%v", err)
		}
		return SyncResponse(result)
	default:
		return BadRequest(`action must be "create" or "delete"`)
	}
}
