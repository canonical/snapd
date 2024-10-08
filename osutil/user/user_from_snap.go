// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build snapdusergo

/*
 * Copyright (C) 2024 Canonical Ltd
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

package user

import (
	"os"
	"strconv"

	osuser "os/user"
)

type (
	User               = osuser.User
	Group              = osuser.Group
	UnknownUserError   = osuser.UnknownUserError
	UnknownUserIdError = osuser.UnknownUserIdError
	UnknownGroupError  = osuser.UnknownGroupError
)

const GetentBased = true

// Current returns the current user
func Current() (*User, error) {
	u, err := lookupUserFromGetent(userMatchUid(os.Getuid()))
	if u == nil && err == nil {
		return nil, UnknownUserIdError(os.Getuid())
	}
	return u, err
}

// Lookup looks up a user by username
func Lookup(username string) (*User, error) {
	u, err := lookupUserFromGetent(userMatchUsername(username))
	if u == nil && err == nil {
		return nil, UnknownUserError(username)
	}
	return u, err
}

// Lookup looks up a user by uid
func LookupId(uid string) (*User, error) {
	uidn, err := strconv.Atoi(uid)
	if err != nil {
		return nil, UnknownUserError(uid)
	}
	u, err := lookupUserFromGetent(userMatchUid(uidn))
	if u == nil && err == nil {
		return nil, UnknownUserIdError(uidn)
	}
	return u, err
}

// Lookup looks up a group by group name
func LookupGroup(groupname string) (*Group, error) {
	g, err := lookupGroupFromGetent(groupMatchGroupname(groupname))
	if g == nil && err == nil {
		return nil, UnknownGroupError(groupname)
	}
	return g, err
}
