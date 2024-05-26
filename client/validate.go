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

	"github.com/ddkwork/golibrary/mylog"
	"golang.org/x/xerrors"
)

// ValidateApplyOptions carries options for ApplyValidationSet.
type ValidateApplyOptions struct {
	Mode     string
	Sequence int
}

// ValidationSetResult holds information about a single validation set.
type ValidationSetResult struct {
	AccountID string `json:"account-id"`
	Name      string `json:"name"`

	Sequence int `json:"sequence,omitempty"`
	PinnedAt int `json:"pinned-at,omitempty"`

	Mode  string `json:"mode"`
	Valid bool   `json:"valid"`
	// TODO: flags/states for notes column
}

type postValidationSetData struct {
	Action   string `json:"action"`
	Mode     string `json:"mode,omitempty"`
	Sequence int    `json:"sequence,omitempty"`
}

// ForgetValidationSet forgets the given validation set identified by account,
// name and optional sequence (if non-zero).
func (client *Client) ForgetValidationSet(accountID, name string, sequence int) error {
	if accountID == "" || name == "" {
		return xerrors.Errorf("cannot forget validation set without account ID and name")
	}

	data := &postValidationSetData{
		Action:   "forget",
		Sequence: sequence,
	}

	var body bytes.Buffer
	mylog.Check(json.NewEncoder(&body).Encode(data))

	path := fmt.Sprintf("/v2/validation-sets/%s/%s", accountID, name)
	mylog.Check2(client.doSync("POST", path, nil, nil, &body, nil))

	return nil
}

// ApplyValidationSet applies the given validation set identified by account and name and returns
// the new validation set tracking info. For monitoring mode the returned res may indicate invalid
// state.
func (client *Client) ApplyValidationSet(accountID, name string, opts *ValidateApplyOptions) (res *ValidationSetResult, err error) {
	if accountID == "" || name == "" {
		return nil, xerrors.Errorf("cannot apply validation set without account ID and name")
	}

	data := &postValidationSetData{
		Action:   "apply",
		Mode:     opts.Mode,
		Sequence: opts.Sequence,
	}

	var body bytes.Buffer
	mylog.Check(json.NewEncoder(&body).Encode(data))

	path := fmt.Sprintf("/v2/validation-sets/%s/%s", accountID, name)
	mylog.Check2(client.doSync("POST", path, nil, nil, &body, &res))

	return res, nil
}

// ListValidationsSets queries all validation sets.
func (client *Client) ListValidationsSets() ([]*ValidationSetResult, error) {
	var res []*ValidationSetResult
	mylog.Check2(client.doSync("GET", "/v2/validation-sets", nil, nil, nil, &res))

	return res, nil
}

// ValidationSet queries the given validation set identified by account/name.
func (client *Client) ValidationSet(accountID, name string, sequence int) (*ValidationSetResult, error) {
	if accountID == "" || name == "" {
		return nil, xerrors.Errorf("cannot query validation set without account ID and name")
	}

	q := url.Values{}
	if sequence != 0 {
		q.Set("sequence", fmt.Sprintf("%d", sequence))
	}

	var res *ValidationSetResult
	path := fmt.Sprintf("/v2/validation-sets/%s/%s", accountID, name)
	mylog.Check2(client.doSync("GET", path, q, nil, nil, &res))

	return res, nil
}
