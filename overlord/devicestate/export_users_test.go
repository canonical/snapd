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

package devicestate

import (
	"os/user"

	"github.com/snapcore/snapd/osutil"
)

func MockOsutilAddUser(addUser func(name string, opts *osutil.AddUserOptions) error) (restore func()) {
	oldAddUser := osutilAddUser
	// internal.SetOsutilAddUser(addUser)
	osutilAddUser = addUser
	return func() {
		osutilAddUser = oldAddUser
		// internal.SetOsutilAddUser((func(name string, opts *osutil.AddUserOptions) error)(oldAddUser))
	}
}

func MockOsutilDelUser(delUser func(name string, opts *osutil.DelUserOptions) error) (restore func()) {
	oldDelUser := osutilDelUser
	// internal.SetOsutilDelUser(delUser)
	osutilDelUser = delUser
	return func() {
		// internal.SetOsutilDelUser((func(name string, opts *osutil.DelUserOptions) error)(oldDelUser))
		osutilDelUser = oldDelUser
	}
}

func MockUserLookup(lookup func(username string) (*user.User, error)) (restore func()) {
	oldLookup := userLookup
	// internal.SetUserLookup(lookup)
	userLookup = lookup
	return func() {
		// internal.SetUserLookup((func(username string) (*user.User, error))(oldLookup))
		userLookup = oldLookup
	}
}

var (
	GetUserDetailsFromAssertion = getUserDetailsFromAssertion
)
