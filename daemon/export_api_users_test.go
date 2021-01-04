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

package daemon

import (
	"os/user"

	"github.com/snapcore/snapd/osutil"
)

func MockHasUserAdmin(mockHasUserAdmin bool) (restore func()) {
	oldHasUserAdmin := hasUserAdmin
	hasUserAdmin = mockHasUserAdmin
	return func() {
		hasUserAdmin = oldHasUserAdmin
	}
}

func MockUserLookup(lookup func(username string) (*user.User, error)) (restore func()) {
	oldLookup := userLookup
	userLookup = lookup
	return func() {
		userLookup = oldLookup
	}
}

func MockOsutilAddUser(addUser func(name string, opts *osutil.AddUserOptions) error) (restore func()) {
	oldAddUser := osutilAddUser
	osutilAddUser = addUser
	return func() {
		osutilAddUser = oldAddUser
	}
}

func MockOsutilDelUser(delUser func(name string, opts *osutil.DelUserOptions) error) (restore func()) {
	oldDelUser := osutilDelUser
	osutilDelUser = delUser
	return func() {
		osutilDelUser = oldDelUser
	}
}

type (
	UserResponseData = userResponseData
)

var (
	GetUserDetailsFromAssertion = getUserDetailsFromAssertion
)
