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
	Mode     string
	Sequence int
}

// ValidationSetResult holds information about a single validation set.
type ValidationSetResult struct {
	ValidationSet string `json:"validation-set,omitempty"`
	Mode          string `json:"mode"`
	Sequence      int    `json:"sequence,omitempty"`
	Valid         bool   `json:"valid"`
	// TODO: flags/states for notes column
}

type postData struct {
	Action   string `json:"action"`
	Mode     string `json:"mode,omitempty"`
	Sequence int    `json:"sequence,omitempty"`
}

// ForgetValidationSet forgets the given validation set identified by account,
// name and optional sequence (if non-zero).
func (client *Client) ForgetValidationSet(account, name string, sequence int) error {
	if account == "" || name == "" {
		return xerrors.Errorf("cannot forget validation set without account and name")
	}

	data := &postData{
		Action:   "forget",
		Sequence: sequence,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		return err
	}
	path := fmt.Sprintf("/v2/validation-sets/%s/%s", account, name)
	if _, err := client.doSync("POST", path, nil, nil, &body, nil); err != nil {
		return xerrors.Errorf("cannot forget validation set: %v", err)
	}
	return nil
}

// ApplyValidationSet applies the given validation set identified by account and name.
func (client *Client) ApplyValidationSet(account, name string, opts *ValidateApplyOptions) error {
	if account == "" || name == "" {
		return xerrors.Errorf("cannot apply validation set without account and name")
	}

	data := &postData{
		Action:   "apply",
		Mode:     opts.Mode,
		Sequence: opts.Sequence,
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(data); err != nil {
		return err
	}
	path := fmt.Sprintf("/v2/validation-sets/%s/%s", account, name)
	if _, err := client.doSync("POST", path, nil, nil, &body, nil); err != nil {
		return xerrors.Errorf("cannot apply validation set: %v", err)
	}
	return nil
}

// ListValidationsSets queries all validation sets.
func (client *Client) ListValidationsSets() ([]*ValidationSetResult, error) {
	var res []*ValidationSetResult
	_, err := client.doSync("GET", "/v2/validation-sets", nil, nil, nil, &res)
	if err != nil {
		return nil, xerrors.Errorf("cannot list validation sets: %v", err)
	}
	return res, nil
}

// ValidationSet queries the given validation set identified by account/name.
func (client *Client) ValidationSet(account, name string, sequence int) (*ValidationSetResult, error) {
	if account == "" || name == "" {
		return nil, xerrors.Errorf("cannot query validation set without account and name")
	}

	q := url.Values{}
	if sequence != 0 {
		q.Set("sequence", fmt.Sprintf("%d", sequence))
	}

	var res *ValidationSetResult
	path := fmt.Sprintf("/v2/validation-sets/%s/%s", account, name)
	_, err := client.doSync("GET", path, q, nil, nil, &res)
	if err != nil {
		return nil, xerrors.Errorf("cannot query validation set: %v", err)
	}
	return res, nil
}
