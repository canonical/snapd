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

	"golang.org/x/xerrors"
)

type postQuotaData struct {
	Action    string   `json:"action"`
	GroupName string   `json:"group-name"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
	MaxMemory uint64   `json:"max-memory,omitempty"`
}

type QuotaGroupResult struct {
	GroupName     string   `json:"group-name"`
	Parent        string   `json:"parent,omitempty"`
	Subgroups     []string `json:"subgroups,omitempty"`
	Snaps         []string `json:"snaps,omitempty"`
	MaxMemory     uint64   `json:"max-memory"`
	CurrentMemory uint64   `json:"current-memory"`
}

// EnsureQuota creates a quota group or updates an existing group.
// The list of snaps can be empty.
func (client *Client) EnsureQuota(groupName string, parent string, snaps []string, maxMemory uint64) error {
	if groupName == "" {
		return xerrors.Errorf("cannot create or update quota group without a name")
	}
	// TODO: use naming.ValidateQuotaGroup()

	data := &postQuotaData{
		Action:    "ensure",
		GroupName: groupName,
		Parent:    parent,
		Snaps:     snaps,
		MaxMemory: maxMemory,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/quotas", nil, nil, &body, nil); err != nil {
		fmt := "cannot create or update quota group: %w"
		return xerrors.Errorf(fmt, err)
	}
	return nil
}

func (client *Client) GetQuotaGroup(groupName string) (*QuotaGroupResult, error) {
	if groupName == "" {
		return nil, xerrors.Errorf("cannot get quota group without a name")
	}

	var res *QuotaGroupResult
	path := fmt.Sprintf("/v2/quotas/%s", groupName)
	if _, err := client.doSync("GET", path, nil, nil, nil, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func (client *Client) RemoveQuotaGroup(groupName string) error {
	if groupName == "" {
		return xerrors.Errorf("cannot remove quota group without a name")
	}
	data := &postQuotaData{
		Action:    "remove",
		GroupName: groupName,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/quotas", nil, nil, &body, nil); err != nil {
		return err
	}
	return nil
}

func (client *Client) Quotas() ([]*QuotaGroupResult, error) {
	var res []*QuotaGroupResult
	if _, err := client.doSync("GET", "/v2/quotas", nil, nil, nil, &res); err != nil {
		return nil, err
	}
	return res, nil
}
