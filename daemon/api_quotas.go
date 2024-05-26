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

	"github.com/ddkwork/golibrary/mylog"
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
	Services    []string           `json:"services,omitempty"`
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
		mem := mylog.Check2(grp.CurrentMemoryUsage())

		currentUsage.Memory = mem
	}

	if grp.ThreadLimit != 0 {
		threads := mylog.Check2(grp.CurrentTaskUsage())

		currentUsage.Threads = threads
	}

	return &currentUsage, nil
}

func createQuotaValues(grp *quota.Group) *client.QuotaValues {
	var constraints client.QuotaValues
	constraints.Memory = grp.MemoryLimit
	constraints.Threads = grp.ThreadLimit

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
			Size: grp.JournalLimit.Size,
		}
		if grp.JournalLimit.RateEnabled {
			constraints.Journal.QuotaJournalRate = &client.QuotaJournalRate{
				RateCount:  grp.JournalLimit.RateCount,
				RatePeriod: grp.JournalLimit.RatePeriod,
			}
		}
	}
	return &constraints
}

// getQuotaGroups returns all quota groups sorted by name.
func getQuotaGroups(c *Command, r *http.Request, _ *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	quotas := mylog.Check2(servicestate.AllQuotas(st))

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

		currentUsage := mylog.Check2(getQuotaUsage(group))

		results[i] = client.QuotaGroupResult{
			GroupName:   group.Name,
			Parent:      group.ParentGroup,
			Subgroups:   group.SubGroups,
			Snaps:       group.Snaps,
			Services:    group.Services,
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
	mylog.Check(naming.ValidateQuotaGroup(groupName))

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	group := mylog.Check2(servicestate.GetQuota(st, groupName))
	if err == servicestate.ErrQuotaNotFound {
		return NotFound("cannot find quota group %q", groupName)
	}

	currentUsage := mylog.Check2(getQuotaUsage(group))

	res := client.QuotaGroupResult{
		GroupName:   group.Name,
		Parent:      group.ParentGroup,
		Snaps:       group.Snaps,
		Services:    group.Services,
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
		if values.Journal.QuotaJournalRate != nil {
			resourcesBuilder.WithJournalRate(values.Journal.RateCount, values.Journal.RatePeriod)
		}
	}
	return resourcesBuilder.Build()
}

// postQuotaGroup creates quota resource group or updates an existing group.
func postQuotaGroup(c *Command, r *http.Request, _ *auth.UserState) Response {
	var data postQuotaGroupData
	mylog.Check(jsonutil.DecodeWithNumber(r.Body, &data))
	mylog.Check(naming.ValidateQuotaGroup(data.GroupName))

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
		_ := mylog.Check2(servicestate.GetQuota(st, data.GroupName))
		if err != nil && err != servicestate.ErrQuotaNotFound {
			return InternalError(err.Error())
		}
		if err == servicestate.ErrQuotaNotFound {
			// then we need to create the quota
			ts = mylog.Check2(servicestateCreateQuota(st, data.GroupName, servicestate.CreateQuotaOptions{
				ParentName:     data.Parent,
				Snaps:          data.Snaps,
				Services:       data.Services,
				ResourceLimits: resourceLimits,
			}))

			chgSummary = "Create quota group"
		} else if err == nil {
			// the quota group already exists, update it
			updateOpts := servicestate.UpdateQuotaOptions{
				AddSnaps:          data.Snaps,
				AddServices:       data.Services,
				NewResourceLimits: resourceLimits,
			}
			ts = mylog.Check2(servicestateUpdateQuota(st, data.GroupName, updateOpts))

			chgSummary = "Update quota group"
		}

	case "remove":

		ts = mylog.Check2(servicestateRemoveQuota(st, data.GroupName))

		chgSummary = "Remove quota group"
	default:
		return BadRequest("unknown quota action %q", data.Action)
	}

	chg := newChange(st, "quota-control", chgSummary, []*state.TaskSet{ts}, data.Snaps)
	ensureStateSoon(st)
	return AsyncResponse(nil, chg.ID())
}
