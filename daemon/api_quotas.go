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
	"github.com/snapcore/snapd/snap/quota"
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
	MaxMemory uint64   `json:"max-memory,omitempty"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
}

type quotaGroupResultJSON struct {
	GroupName     string   `json:"group-name"`
	MaxMemory     uint64   `json:"max-memory"`
	CurrentMemory uint64   `json:"current-memory"`
	Parent        string   `json:"parent,omitempty"`
	Snaps         []string `json:"snaps,omitempty"`
	SubGroups     []string `json:"subgroups,omitempty"`
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

	results := make([]quotaGroupResultJSON, len(quotas))
	for i, name := range names {
		qt := quotas[name]

		memoryUsage, err := getQuotaMemUsage(qt)
		if err != nil {
			return InternalError(err.Error())
		}

		results[i] = quotaGroupResultJSON{
			GroupName:     qt.Name,
			Parent:        qt.ParentGroup,
			SubGroups:     qt.SubGroups,
			Snaps:         qt.Snaps,
			MaxMemory:     uint64(qt.MemoryLimit),
			CurrentMemory: uint64(memoryUsage),
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

	res := quotaGroupResultJSON{
		GroupName:     group.Name,
		Parent:        group.ParentGroup,
		Snaps:         group.Snaps,
		SubGroups:     group.SubGroups,
		MaxMemory:     uint64(group.MemoryLimit),
		CurrentMemory: uint64(memoryUsage),
	}
	return SyncResponse(res)
}

// postQuotaGroup creates quota resource group or updates an existing group.
func postQuotaGroup(c *Command, r *http.Request, _ *auth.UserState) Response {
	var data postQuotaGroupData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return BadRequest("cannot decode quota action from request body: %v", err)
	}

	if err := naming.ValidateQuotaGroup(data.GroupName); err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

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
			if err := servicestateCreateQuota(st, data.GroupName, data.Parent, data.Snaps, quantity.Size(data.MaxMemory)); err != nil {
				// XXX: dedicated error type?
				return BadRequest(err.Error())
			}
		} else if err == nil {
			// the quota group already exists, update it
			updateOpts := servicestate.QuotaGroupUpdate{
				AddSnaps:       data.Snaps,
				NewMemoryLimit: quantity.Size(data.MaxMemory),
			}
			if err := servicestateUpdateQuota(st, data.GroupName, updateOpts); err != nil {
				return BadRequest(err.Error())
			}
		}

	case "remove":
		if err := servicestateRemoveQuota(st, data.GroupName); err != nil {
			return BadRequest(err.Error())
		}
	default:
		return BadRequest("unknown quota action %q", data.Action)
	}
	return SyncResponse(nil)
}
