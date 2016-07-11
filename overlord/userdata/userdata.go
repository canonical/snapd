// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package userdata

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

// UserData acts as a key-value store for a specific user
type UserData struct {
	user  *auth.UserState
	state *state.State
}

// New returns a store for the user which is persisted using the provided state
func New(u *auth.UserState, s *state.State) *UserData {
	return &UserData{
		user:  u,
		state: s,
	}
}

func (ud *UserData) keyForData(key string) string {
	return fmt.Sprintf("user-%d-%s", ud.user.ID, key)
}

// Get retrieves a value for the user
func (ud *UserData) Get(key string, value interface{}) error {
	return ud.state.Get(ud.keyForData(key), value)
}

// Set stores a value for the user
func (ud *UserData) Set(key string, value interface{}) {
	ud.state.Set(ud.keyForData(key), value)
}
