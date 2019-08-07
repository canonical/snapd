// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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
	"os/user"
	"strconv"
)

// TODO: the builtin os/user functions only look at /etc/passwd and /etc/group
// which is fine for our purposes today. In the future we may want to support
// lookups in extrausers, which is configured via nsswitch.conf. Since snapd
// does not support being built with cgo itself, when we want to support
// extrausers here, we can convert these to do the equivalent of:
//
//   getent passwd <user> | cut -d : -f 3
//   getent group <group> | cut -d : -f 3

// FindUid returns the identifier of the given UNIX user name.
func FindUid(username string) (uint64, error) {
	user, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(user.Uid, 10, 64)
}

// FindGid returns the identifier of the given UNIX group name.
func FindGid(groupname string) (uint64, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(group.Gid, 10, 64)
}
