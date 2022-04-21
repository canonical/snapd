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
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

func MockHasUserAdmin(mockHasUserAdmin bool) (restore func()) {
	oldHasUserAdmin := hasUserAdmin
	hasUserAdmin = mockHasUserAdmin
	return func() {
		hasUserAdmin = oldHasUserAdmin
	}
}

func MockDeviceStateCreateUser(createUser func(st *state.State, mgr *devicestate.DeviceManager, sudoer bool, createKnown bool, email string) (createdUsers []devicestate.UserResponse, internal_err bool, err error)) (restore func()) {
	oldCreateUser := deviceStateCreateUser
	deviceStateCreateUser = createUser
	return func() {
		deviceStateCreateUser = oldCreateUser

	}
}

func MockDeviceStateRemoveUser(removeUser func(st *state.State, username string) (*auth.UserState, bool, error)) (restore func()) {
	oldRemoveUser := deviceStateRemoveUser
	deviceStateRemoveUser = removeUser
	return func() {
		deviceStateRemoveUser = oldRemoveUser
	}
}

type (
	UserResponseData = userResponseData
)
