// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"net/url"

	"golang.org/x/xerrors"
)

// ValidateApplyOptions carries options for ApplyValidationSet.
type ValidateApplyOptions struct {
	Mode  string
	PinAt int
}

// ValidationSetResult holds information about a single validation set.
type ValidationSetResult struct {
	ValidationSet string   `json:"validation-set,omitempty"`
	Mode          string   `json:"mode"`
	Seq           int      `json:"seq,omitempty"`
	Valid         bool     `json:"valid"`
	Notes         []string `json:"notes,omitempty"`
}

// ApplyValidationSet applies or forgets the given validation set identified by account and name.
func (client *Client) ApplyValidationSet(account, name string, opts *ValidateApplyOptions) error {
	q := url.Values{}
	q.Set("validation-set", fmt.Sprintf("%s/%s", account, name))
	var postData struct {
		Mode  string `json:"mode"`
		PinAt int    `json:"pin-at,omitempty"`
	}
	postData.Mode = opts.Mode
	if opts.PinAt != 0 {
		postData.PinAt = opts.PinAt
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(postData); err != nil {
		return err
	}
	if _, err := client.doSync("POST", "/v2/validation-sets", q, nil, &body, nil); err != nil {
		return xerrors.Errorf("cannot apply validation set: %v", err)
	}
	return nil
}

// QueryValidationSet queries the given validation set identified by account/name, or all validation
// sets if account/name are not provided.
func (client *Client) QueryValidationSet(account, name string, pinnedAt int) ([]*ValidationSetResult, error) {
	q := url.Values{}

	if account != "" && name != "" {
		q.Set("validation-set", fmt.Sprintf("%s/%s", account, name))
		if pinnedAt != 0 {
			q.Set("pin-at", fmt.Sprintf("%d", pinnedAt))
		}
	}

	var res []*ValidationSetResult
	_, err := client.doSync("GET", "/v2/validation-sets", q, nil, nil, &res)
	if err != nil {
		return nil, xerrors.Errorf("cannot get validation sets: %v", err)
	}
	return res, nil
}
