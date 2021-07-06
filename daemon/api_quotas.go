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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/strutil"
)

var (
	quotaGroupsCmd = &Command{
		Path:        "/v2/quotas",
		GET:         getQuotaGroups,
		POST:        postQuotaGroup,
		WriteAccess: rootAccess{},
		ReadAccess:  openAccess{},
	}
	quotaGroupInfoCmd = &Command{
		Path:       "/v2/quotas/{group}",
		GET:        getQuotaGroupInfo,
		ReadAccess: openAccess{},
	}
)

type postQuotaGroupData struct {
	// Action can be "ensure" or "remove"
	Action    string   `json:"action"`
	GroupName string   `json:"group-name"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
	// Constraints is a map of resource type to constraint, currently accepted
	// resource types is just "memory"
	Constraints map[string]interface{} `json:"constraints,omitempty"`
}

var (
	servicestateCreateQuota = servicestate.CreateQuota
	servicestateUpdateQuota = servicestate.UpdateQuota
	servicestateRemoveQuota = servicestate.RemoveQuota
)

var getQuotaMemUsage = func(grp *quota.Group) (quantity.Size, error) {
	return grp.CurrentMemoryUsage()
}

// getQuotaGroups returns all quota groups sorted by name.
func getQuotaGroups(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	quotas, err := servicestate.AllQuotas(st)
	if err != nil {
		return InternalError(err.Error())
	}

	i := 0
	names := make([]string, len(quotas))
	for name := range quotas {
		names[i] = name
		i++
	}
	sort.Strings(names)

	results := make([]client.QuotaGroupResult, len(quotas))
	for i, name := range names {
		group := quotas[name]

		memoryUsage, err := getQuotaMemUsage(group)
		if err != nil {
			return InternalError(err.Error())
		}

		results[i] = client.QuotaGroupResult{
			GroupName: group.Name,
			Parent:    group.ParentGroup,
			Subgroups: group.SubGroups,
			Snaps:     group.Snaps,
			Constraints: &client.QuotaValues{
				Memory: group.MemoryLimit,
			},
			Current: &client.QuotaValues{
				Memory: memoryUsage,
			},
		}
	}
	return SyncResponse(results)
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

	group, err := servicestate.GetQuota(st, groupName)
	if err == servicestate.ErrQuotaNotFound {
		return NotFound("cannot find quota group %q", groupName)
	}
	if err != nil {
		return InternalError(err.Error())
	}

	memoryUsage, err := getQuotaMemUsage(group)
	if err != nil {
		return InternalError(err.Error())
	}

	res := client.QuotaGroupResult{
		GroupName: group.Name,
		Parent:    group.ParentGroup,
		Snaps:     group.Snaps,
		Subgroups: group.SubGroups,
		Constraints: &client.QuotaValues{
			Memory: group.MemoryLimit,
		},
		Current: &client.QuotaValues{
			Memory: memoryUsage,
		},
	}
	return SyncResponse(res)
}

// postQuotaGroup creates quota resource group or updates an existing group.
func postQuotaGroup(c *Command, r *http.Request, _ *auth.UserState) Response {
	var data postQuotaGroupData

	if err := jsonutil.DecodeWithNumber(r.Body, &data); err != nil {
		return BadRequest("cannot decode quota action from request body: %v", err)
	}

	if err := naming.ValidateQuotaGroup(data.GroupName); err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	chgSummary := ""

	var ts *state.TaskSet

	var memSize uint64

	var unknownResourceTypes []string
	for resourceType := range data.Constraints {
		switch resourceType {
		case "memory":
		default:
			unknownResourceTypes = append(unknownResourceTypes, resourceType)
		}
	}

	if len(unknownResourceTypes) != 0 {
		pluralization := ""
		if len(unknownResourceTypes) > 1 {
			pluralization = "s"
		}
		return BadRequest("unknown resource type constraint%s in request: %s", pluralization, strutil.Quoted(unknownResourceTypes))
	}

	if memVal, ok := data.Constraints["memory"]; ok {
		memJsonNumber, ok := memVal.(json.Number)
		if !ok {
			return BadRequest("cannot decode quota action memory constraint as number (got %T)", memVal)
		}

		memInt64, err := memJsonNumber.Int64()
		if err != nil {
			return BadRequest("cannot decode quota action memory constrain as uint: %v", err)
		}

		memSize = uint64(memInt64)
	}

	switch data.Action {
	case "ensure":
		// check if the quota group exists first, if it does then we need to
		// update it instead of create it
		_, err := servicestate.GetQuota(st, data.GroupName)
		if err != nil && err != servicestate.ErrQuotaNotFound {
			return InternalError(err.Error())
		}
		if err == servicestate.ErrQuotaNotFound {
			// then we need to create the quota
			ts, err = servicestateCreateQuota(st, data.GroupName, data.Parent, data.Snaps, quantity.Size(memSize))
			if err != nil {
				// XXX: dedicated error type?
				return BadRequest(err.Error())
			}
			chgSummary = "Create quota group"
		} else if err == nil {
			// the quota group already exists, update it
			updateOpts := servicestate.QuotaGroupUpdate{
				AddSnaps:       data.Snaps,
				NewMemoryLimit: quantity.Size(memSize),
			}
			ts, err = servicestateUpdateQuota(st, data.GroupName, updateOpts)
			if err != nil {
				return BadRequest(err.Error())
			}
			chgSummary = "Update quota group"
		}

	case "remove":
		var err error
		ts, err = servicestateRemoveQuota(st, data.GroupName)
		if err != nil {
			return BadRequest(err.Error())
		}
		chgSummary = "Remove quota group"
	default:
		return BadRequest("unknown quota action %q", data.Action)
	}

	chg := newChange(st, "quota-control", chgSummary, []*state.TaskSet{ts}, data.Snaps)
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
