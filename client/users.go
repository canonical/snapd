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

	"github.com/ddkwork/golibrary/mylog"
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
	// Automatic is for internal snapd use, behavior might evolve
	Automatic bool `json:"automatic,omitempty"`
}

// RemoveUserOptions holds options for removing a local system user.
type RemoveUserOptions struct {
	// Username indicates which user to remove.
	Username string `json:"username,omitempty"`
}

type userAction struct {
	Action string `json:"action"`
	*CreateUserOptions
	*RemoveUserOptions
}

func (client *Client) doUserAction(act *userAction, result interface{}) error {
	data := mylog.Check2(json.Marshal(act))

	_ = mylog.Check2(client.doSync("POST", "/v2/users", nil, nil, bytes.NewReader(data), result))
	return err
}

// CreateUser creates a local system user. See CreateUserOptions for details.
func (client *Client) CreateUser(options *CreateUserOptions) (*CreateUserResult, error) {
	if options == nil || options.Email == "" {
		return nil, fmt.Errorf("cannot create a user without providing an email")
	}

	var result []*CreateUserResult
	mylog.Check(client.doUserAction(&userAction{Action: "create", CreateUserOptions: options}, &result))

	return result[0], nil
}

// CreateUsers creates multiple local system users. See CreateUserOptions for details.
//
// Results may be provided even if there are errors.
func (client *Client) CreateUsers(options []*CreateUserOptions) ([]*CreateUserResult, error) {
	for _, opts := range options {
		if opts == nil || (opts.Email == "" && !(opts.Known || opts.Automatic)) {
			return nil, fmt.Errorf("cannot create user from store details without an email to query for")
		}
	}

	var results []*CreateUserResult
	var errs []error
	for _, opts := range options {
		var result []*CreateUserResult
		mylog.Check(client.doUserAction(&userAction{Action: "create", CreateUserOptions: opts}, &result))

	}

	if len(errs) == 1 {
		return results, errs[0]
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

// RemoveUser removes a local system user.
func (client *Client) RemoveUser(options *RemoveUserOptions) (removed []*User, err error) {
	if options == nil || options.Username == "" {
		return nil, fmt.Errorf("cannot remove a user without providing a username")
	}
	var result struct {
		Removed []*User `json:"removed"`
	}
	mylog.Check(client.doUserAction(&userAction{Action: "remove", RemoveUserOptions: options}, &result))

	return result.Removed, nil
}

// Users returns the local users.
func (client *Client) Users() ([]*User, error) {
	var result []*User
	mylog.Check2(client.doSync("GET", "/v2/users", nil, nil, nil, &result))

	return result, nil
}
