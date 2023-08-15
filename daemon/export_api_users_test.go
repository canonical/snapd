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
	"time"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func MockHasUserAdmin(mockHasUserAdmin bool) (restore func()) {
	restore = testutil.Backup(&hasUserAdmin)
	hasUserAdmin = mockHasUserAdmin
	return restore
}

func MockDeviceStateCreateUser(createUser func(st *state.State, sudoer bool, email string, expiration time.Time) (*devicestate.CreatedUser, error)) (restore func()) {
	restore = testutil.Backup(&deviceStateCreateUser)
	deviceStateCreateUser = createUser
	return restore
}

func MockDeviceStateCreateKnownUsers(createKnownUser func(st *state.State, sudoer bool, email string) ([]*devicestate.CreatedUser, error)) (restore func()) {
	restore = testutil.Backup(&deviceStateCreateUser)
	deviceStateCreateKnownUsers = createKnownUser
	return restore
}

func MockDeviceStateRemoveUser(removeUser func(st *state.State, username string, opts *devicestate.RemoveUserOptions) (*auth.UserState, error)) (restore func()) {
	restore = testutil.Backup(&deviceStateRemoveUser)
	deviceStateRemoveUser = removeUser
	return restore
}

type (
	UserResponseData = userResponseData
)
