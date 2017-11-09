// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package osutil

import (
	"strconv"

	"github.com/snapcore/snapd/osutil/user"
)

// Group represents a grouping of users.
//
// On POSIX systems Gid contains a decimal number representing the group ID.
type Group struct {
	Gid  string // group ID
	Name string // group name
}

// FindUid returns the identifier of the given UNIX user name.
func FindUid(username string) (uint64, error) {
	user, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(user.Uid, 10, 64)
}

// FindGid returns the identifier of the given UNIX group name.
func FindGid(groupName string) (uint64, error) {
	// In golang 1.8 we can use the built-in function like this:
	//group, err := user.LookupGroup(group)
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, err
	}

	// In golang 1.8 we can parse the group.Gid string instead.
	//return strconv.ParseUint(group.Gid, 10, 64)
	return strconv.ParseUint(group.Gid, 10, 64)
}
