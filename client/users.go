// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
)

// CreateUserResult holds the result of a user creation.
type CreateUserResult struct {
	Username string   `json:"username"`
	SSHKeys  []string `json:"ssh-keys"`
}

// CreateUserOptions holds options for creating a local system user.
//
// If Known is false, the provided email is used to query the store for
// username and SSH key details.
//
// If Known is true, the user will be created by looking through existing
// system-user assertions and looking for a matching email. If Email is
// empty then all such assertions are considered and multiple users may
// be created.
type CreateUserOptions struct {
	Email        string `json:"email,omitempty"`
	Sudoer       bool   `json:"sudoer,omitempty"`
	Known        bool   `json:"known,omitempty"`
	ForceManaged bool   `json:"force-managed,omitempty"`
}

type userAction struct {
	Action string `json:"action"`
	*CreateUserOptions
}

func (client *Client) doUserAction(act *userAction) ([]*CreateUserResult, error) {
	data, err := json.Marshal(act)
	if err != nil {
		return nil, err
	}

	var result []*CreateUserResult
	_, err = client.doSync("POST", "/v2/users", nil, nil, bytes.NewReader(data), &result)
	return result, err
}

// CreateUser creates a local system user. See CreateUserOptions for details.
func (client *Client) CreateUser(options *CreateUserOptions) (*CreateUserResult, error) {
	if options.Email == "" {
		return nil, fmt.Errorf("cannot create a user without providing an email")
	}

	result, err := client.doUserAction(&userAction{Action: "create", CreateUserOptions: options})
	if err != nil {
		return nil, fmt.Errorf("while creating user: %v", err)
	}
	return result[0], nil
}

// CreateUsers creates multiple local system users. See CreateUserOptions for details.
//
// Results may be provided even if there are errors.
func (client *Client) CreateUsers(options []*CreateUserOptions) ([]*CreateUserResult, error) {
	for _, opts := range options {
		if opts.Email == "" && !opts.Known {
			return nil, fmt.Errorf("cannot create user from store details without an email to query for")
		}
	}

	var results []*CreateUserResult
	var errs []error
	for _, opts := range options {
		result, err := client.doUserAction(&userAction{Action: "create", CreateUserOptions: opts})
		if err != nil {
			errs = append(errs, err)
		} else {
			results = append(results, result...)
		}
	}

	if len(errs) == 1 {
		return results, fmt.Errorf("while creating user: %v", errs[0])
	}
	if len(errs) > 1 {
		var buf bytes.Buffer
		for _, err := range errs {
			fmt.Fprintf(&buf, "\n- %s", err)
		}
		return results, fmt.Errorf("while creating users:%s", buf.Bytes())
	}
	return results, nil
}

// Users returns the local users.
func (client *Client) Users() ([]*User, error) {
	var result []*User

	if _, err := client.doSync("GET", "/v2/users", nil, nil, nil, &result); err != nil {
		return nil, fmt.Errorf("while getting users: %v", err)
	}
	return result, nil
}
