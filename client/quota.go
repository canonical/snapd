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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
)

type postQuotaData struct {
	Action      string       `json:"action"`
	GroupName   string       `json:"group-name"`
	Parent      string       `json:"parent,omitempty"`
	Snaps       []string     `json:"snaps,omitempty"`
	Services    []string     `json:"services,omitempty"`
	Constraints *QuotaValues `json:"constraints,omitempty"`
}

type QuotaGroupResult struct {
	GroupName   string       `json:"group-name"`
	Parent      string       `json:"parent,omitempty"`
	Subgroups   []string     `json:"subgroups,omitempty"`
	Snaps       []string     `json:"snaps,omitempty"`
	Services    []string     `json:"services,omitempty"`
	Constraints *QuotaValues `json:"constraints,omitempty"`
	Current     *QuotaValues `json:"current,omitempty"`
}

type QuotaCPUValues struct {
	Count      int `json:"count,omitempty"`
	Percentage int `json:"percentage,omitempty"`
}

type QuotaCPUSetValues struct {
	CPUs []int `json:"cpus,omitempty"`
}

type QuotaJournalRate struct {
	RateCount  int           `json:"rate-count"`
	RatePeriod time.Duration `json:"rate-period"`
}

type QuotaJournalValues struct {
	Size quantity.Size `json:"size,omitempty"`
	*QuotaJournalRate
}

type QuotaValues struct {
	Memory  quantity.Size       `json:"memory,omitempty"`
	CPU     *QuotaCPUValues     `json:"cpu,omitempty"`
	CPUSet  *QuotaCPUSetValues  `json:"cpu-set,omitempty"`
	Threads int                 `json:"threads,omitempty"`
	Journal *QuotaJournalValues `json:"journal,omitempty"`
}

type EnsureQuotaOptions struct {
	// Parent is used to assign a Parent quota group
	Parent string
	// Snaps that should be added to the quota group
	Snaps []string
	// Services that should be added to the quota group
	Services []string
	// Constraints are the resource limits that should be applied to the quota group,
	// these are added or modified, not removed.
	Constraints *QuotaValues
}

// EnsureQuota creates a quota group or updates an existing group with the options
// provided.
func (client *Client) EnsureQuota(groupName string, opts *EnsureQuotaOptions) (changeID string, err error) {
	if groupName == "" {
		return "", fmt.Errorf("cannot create or update quota group without a name")
	}
	if opts == nil {
		return "", fmt.Errorf("cannot create or update quota group without any options")
	}

	// TODO: use naming.ValidateQuotaGroup()

	data := &postQuotaData{
		Action:      "ensure",
		GroupName:   groupName,
		Parent:      opts.Parent,
		Snaps:       opts.Snaps,
		Services:    opts.Services,
		Constraints: opts.Constraints,
	}

	var body bytes.Buffer
	mylog.Check(json.NewEncoder(&body).Encode(data))

	chgID := mylog.Check2(client.doAsync("POST", "/v2/quotas", nil, nil, &body))

	return chgID, nil
}

func (client *Client) GetQuotaGroup(groupName string) (*QuotaGroupResult, error) {
	if groupName == "" {
		return nil, fmt.Errorf("cannot get quota group without a name")
	}

	var res *QuotaGroupResult
	path := fmt.Sprintf("/v2/quotas/%s", groupName)
	mylog.Check2(client.doSync("GET", path, nil, nil, nil, &res))

	return res, nil
}

func (client *Client) RemoveQuotaGroup(groupName string) (changeID string, err error) {
	if groupName == "" {
		return "", fmt.Errorf("cannot remove quota group without a name")
	}
	data := &postQuotaData{
		Action:    "remove",
		GroupName: groupName,
	}

	var body bytes.Buffer
	mylog.Check(json.NewEncoder(&body).Encode(data))

	chgID := mylog.Check2(client.doAsync("POST", "/v2/quotas", nil, nil, &body))

	return chgID, nil
}

func (client *Client) Quotas() ([]*QuotaGroupResult, error) {
	var res []*QuotaGroupResult
	mylog.Check2(client.doSync("GET", "/v2/quotas", nil, nil, nil, &res))

	return res, nil
}
