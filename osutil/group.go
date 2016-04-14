// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

// Group implements the grp.h struct group
type Group struct {
	Name   string
	Passwd string
	Gid    uint
	Mem    []string
}

// Getgrnam returns a lit of groups for the given groupname
func Getgrnam(name string) (result Group, err error) {
	return getgrnam(name)
}

// IsUIDInAny checks whether the given user belongs to any of the
// given groups
func IsUIDInAny(uid uint32, groups ...string) bool {
	usr, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return false
	}

	gid, err := strconv.ParseUint(usr.Gid, 10, 32)
	if err != nil {
		return false
	}

	// XXX cache the Getgrnam calls for a second or so?
	for _, groupname := range groups {
		group, err := Getgrnam(groupname)
		if err != nil {
			continue
		}

		if group.Gid == uint(gid) {
			return true
		}

		for _, member := range group.Mem {
			if member == usr.Username {
				return true
			}
		}
	}

	return false
}
