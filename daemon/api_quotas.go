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
	"net/http"
	"sort"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
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
	Action      string             `json:"action"`
	GroupName   string             `json:"group-name"`
	Parent      string             `json:"parent,omitempty"`
	Snaps       []string           `json:"snaps,omitempty"`
	Constraints client.QuotaValues `json:"constraints,omitempty"`
}

var (
	servicestateCreateQuota = servicestate.CreateQuota
	servicestateUpdateQuota = servicestate.UpdateQuota
	servicestateRemoveQuota = servicestate.RemoveQuota
)

var getQuotaUsage = func(grp *quota.Group) (*client.QuotaValues, error) {
	var currentUsage client.QuotaValues

	if grp.MemoryLimit != 0 {
		mem, err := grp.CurrentMemoryUsage()
		if err != nil {
			return nil, err
		}
		currentUsage.Memory = mem
	}

	if grp.TaskLimit != 0 {
		threads, err := grp.CurrentTaskUsage()
		if err != nil {
			return nil, err
		}
		currentUsage.Threads = threads
	}

	return &currentUsage, nil
}

func createQuotaValues(grp *quota.Group) *client.QuotaValues {
	var constraints client.QuotaValues
	constraints.Memory = grp.MemoryLimit
	constraints.Threads = grp.TaskLimit

	if grp.CPULimit != nil {
		constraints.CPU = &client.QuotaCPUValues{
			Count:      grp.CPULimit.Count,
			Percentage: grp.CPULimit.Percentage,
		}
		constraints.CPUSet = &client.QuotaCPUSetValues{
			CPUs: grp.CPULimit.CPUSet,
		}
	}
	if grp.JournalLimit != nil {
		constraints.Journal = &client.QuotaJournalValues{
			Size:       grp.JournalLimit.Size,
			RateCount:  grp.JournalLimit.RateCount,
			RatePeriod: grp.JournalLimit.RatePeriod,
		}
	}
	return &constraints
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

		currentUsage, err := getQuotaUsage(group)
		if err != nil {
			return InternalError(err.Error())
		}

		results[i] = client.QuotaGroupResult{
			GroupName:   group.Name,
			Parent:      group.ParentGroup,
			Subgroups:   group.SubGroups,
			Snaps:       group.Snaps,
			Constraints: createQuotaValues(group),
			Current:     currentUsage,
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

	currentUsage, err := getQuotaUsage(group)
	if err != nil {
		return InternalError(err.Error())
	}

	res := client.QuotaGroupResult{
		GroupName:   group.Name,
		Parent:      group.ParentGroup,
		Snaps:       group.Snaps,
		Subgroups:   group.SubGroups,
		Constraints: createQuotaValues(group),
		Current:     currentUsage,
	}
	return SyncResponse(res)
}

func quotaValuesToResources(values client.QuotaValues) quota.Resources {
	resourcesBuilder := quota.NewResourcesBuilder()
	if values.Memory != 0 {
		resourcesBuilder.WithMemoryLimit(values.Memory)
	}
	if values.CPU != nil {
		if values.CPU.Count != 0 {
			resourcesBuilder.WithCPUCount(values.CPU.Count)
		}
		if values.CPU.Percentage != 0 {
			resourcesBuilder.WithCPUPercentage(values.CPU.Percentage)
		}
	}
	if values.CPUSet != nil && len(values.CPUSet.CPUs) != 0 {
		resourcesBuilder.WithCPUSet(values.CPUSet.CPUs)
	}
	if values.Threads != 0 {
		resourcesBuilder.WithThreadLimit(values.Threads)
	}
	if values.Journal != nil {
		resourcesBuilder.WithJournalNamespace()
		if values.Journal.Size != 0 {
			resourcesBuilder.WithJournalSize(values.Journal.Size)
		}
		if values.Journal.RateCount != 0 && values.Journal.RatePeriod != 0 {
			resourcesBuilder.WithJournalRate(values.Journal.RateCount, values.Journal.RatePeriod)
		}
	}
	return resourcesBuilder.Build()
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

	switch data.Action {
	case "ensure":
		// pack constraints into a resource limits struct
		resourceLimits := quotaValuesToResources(data.Constraints)

		// check if the quota group exists first, if it does then we need to
		// update it instead of create it
		_, err := servicestate.GetQuota(st, data.GroupName)
		if err != nil && err != servicestate.ErrQuotaNotFound {
			return InternalError(err.Error())
		}
		if err == servicestate.ErrQuotaNotFound {
			// then we need to create the quota
			ts, err = servicestateCreateQuota(st, data.GroupName, data.Parent, data.Snaps, resourceLimits)
			if err != nil {
				return errToResponse(err, nil, BadRequest, "cannot create quota group: %v")
			}
			chgSummary = "Create quota group"
		} else if err == nil {
			// the quota group already exists, update it
			updateOpts := servicestate.QuotaGroupUpdate{
				AddSnaps:          data.Snaps,
				NewResourceLimits: resourceLimits,
			}
			ts, err = servicestateUpdateQuota(st, data.GroupName, updateOpts)
			if err != nil {
				return errToResponse(err, nil, BadRequest, "cannot update quota group: %v")
			}
			chgSummary = "Update quota group"
		}

	case "remove":
		var err error
		ts, err = servicestateRemoveQuota(st, data.GroupName)
		if err != nil {
			return errToResponse(err, nil, BadRequest, "cannot remove quota group: %v")
		}
		chgSummary = "Remove quota group"
	default:
		return BadRequest("unknown quota action %q", data.Action)
	}

	chg := newChange(st, "quota-control", chgSummary, []*state.TaskSet{ts}, data.Snaps)
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
