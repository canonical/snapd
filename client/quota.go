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
	GroupName string   `json:"group-name"`
	Parent    string   `json:"parent,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
	MaxMemory int64    `json:"max-memory"`
}

type QuotaGroupResult struct {
	GroupName string   `json:"group-name"`
	Parent    string   `json:"parent,omitempty"`
	Subgroups []string `json:"subgroups,omitempty"`
	Snaps     []string `json:"snaps,omitempty"`
	MaxMemory int64    `json:"max-memory"`
}

// CreateOrUpdateQuota creates a quota group or updates an existing group.
// The list of snaps can be empty.
func (client *Client) CreateOrUpdateQuota(groupName string, parent string, snaps []string, maxMemory int64) error {
	if groupName == "" {
		return xerrors.Errorf("cannot create or update quota group without a name")
	}
	// TODO: use naming.ValidateQuotaGroup()

	data := &postQuotaData{
		GroupName: groupName,
		Parent:    parent,
		Snaps:     snaps,
		MaxMemory: maxMemory,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/quota", nil, nil, &body, nil); err != nil {
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
	path := fmt.Sprintf("/v2/quota/%s", groupName)
	if _, err := client.doSync("GET", path, nil, nil, nil, &res); err != nil {
		fmt := "cannot get quota group: %w"
		return nil, xerrors.Errorf(fmt, err)
	}
	return res, nil
}
