// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !snapdusergo

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
	osuser "os/user"
)

type (
	User              = osuser.User
	Group             = osuser.Group
	UnknownUserError  = osuser.UnknownUserError
	UnknownGroupError = osuser.UnknownGroupError
)

const GetentBased = false

// Current returns the current user
//
// This is a wrapper for (os/user).Current
func Current() (*User, error) {
	return osuser.Current()
}

// Lookup looks up a user by username
//
// This is a wrapper for (os/user).Lookup
func Lookup(username string) (*User, error) {
	return osuser.Lookup(username)
}

// Lookup looks up a user by uid
//
// This is a wrapper for (os/user).LookupId
func LookupId(uid string) (*User, error) {
	return osuser.LookupId(uid)
}

// Lookup looks up a group by group name
//
// This is a wrapper for (os/user).LookupGroup
func LookupGroup(groupname string) (*Group, error) {
	return osuser.LookupGroup(groupname)
}
