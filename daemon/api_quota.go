// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"sort"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/snap/naming"
)

var (
	quotaGroupsCmd = &Command{
		Path: "/v2/quotas",
		GET:  getQuotaGroups,
		POST: postQuotaGroup,
	}
	quotaGroupInfoCmd = &Command{
		Path: "/v2/quotas/{group}",
		GET:  getQuotaGroupInfo,
	}
)

type postQuotaGroupData struct {
	// Action can be "ensure" or "remove"
	Action    string   `json:"action"`
	GroupName string   `json:"group-name"`
	MaxMemory uint64   `json:"max-memory,omitempty"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
}

type quotaGroupResultJSON struct {
	GroupName string   `json:"group-name"`
	MaxMemory uint64   `json:"max-memory"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
	SubGroups []string `json:"subgroups,omitempty"`
}

var (
	servicestateCreateQuota = servicestate.CreateQuota
	servicestateRemoveQuota = servicestate.RemoveQuota
)

// getQuotaGroups returns all quota groups sorted by name.
func getQuotaGroups(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	quotas, err := servicestate.AllQuotas(st)
	if err != nil {
		return InternalError("cannot get quotas: %v", err)
	}

	i := 0
	names := make([]string, len(quotas))
	for name := range quotas {
		names[i] = name
		i++
	}
	sort.Strings(names)

	results := make([]quotaGroupResultJSON, len(quotas))
	for i, name := range names {
		qt := quotas[name]
		results[i] = quotaGroupResultJSON{
			GroupName: qt.Name,
			Parent:    qt.ParentGroup,
			SubGroups: qt.SubGroups,
			Snaps:     qt.Snaps,
			MaxMemory: uint64(qt.MemoryLimit),
		}
	}
	return SyncResponse(results, nil)
}

// getQuotaGroupInfo returns details of a single quota Group.
func getQuotaGroupInfo(c *Command, r *http.Request, _ *auth.UserState) Response {
	vars := muxVars(r)
	groupName := vars["group"]
	if err := naming.ValidateQuotaGroup(groupName); err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	quotas, err := servicestate.AllQuotas(st)
	if err != nil {
		return InternalError("cannot get quotas: %v", err)
	}
	group := quotas[groupName]
	if group == nil {
		return NotFound("cannot find quota group %q", groupName)
	}

	res := quotaGroupResultJSON{
		GroupName: group.Name,
		Parent:    group.ParentGroup,
		Snaps:     group.Snaps,
		SubGroups: group.SubGroups,
		MaxMemory: uint64(group.MemoryLimit),
	}
	return SyncResponse(res, nil)
}

// postQuotaGroup creates quota resource group or updates an existing group.
func postQuotaGroup(c *Command, r *http.Request, _ *auth.UserState) Response {
	var data postQuotaGroupData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return BadRequest("cannot decode create-user data from request body: %v", err)
	}

	if err := naming.ValidateQuotaGroup(data.GroupName); err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	switch data.Action {
	case "ensure":
		// TODO: quota updates
		if err := servicestateCreateQuota(data.GroupName, data.Parent, data.Snaps, quantity.Size(data.MaxMemory)); err != nil {
			// XXX: dedicated error type?
			return BadRequest(err.Error())
		}
	case "remove":
		if err := servicestateRemoveQuota(data.GroupName); err != nil {
			return BadRequest(err.Error())
		}
	default:
		return BadRequest("unknown action %q", data.Action)
	}
	return SyncResponse(nil, nil)
}
