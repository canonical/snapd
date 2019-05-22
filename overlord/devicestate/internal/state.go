// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

// Package internal (of devicestate) provides functions to access and
// set the device state for use only by devicestate, for convenience they
// are also exposed via devicestatetest for use in tests.
package internal

import (
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

// Device returns the device details from the state.
func Device(st *state.State) (*auth.DeviceState, error) {
	var authStateData auth.AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
		return &auth.DeviceState{}, nil
	} else if err != nil {
		return nil, err
	}

	if authStateData.Device == nil {
		return &auth.DeviceState{}, nil
	}

	return authStateData.Device, nil
}

// SetDevice updates the device details in the state.
func SetDevice(st *state.State, device *auth.DeviceState) error {
	var authStateData auth.AuthState

	err := st.Get("auth", &authStateData)
	if err == state.ErrNoState {
		authStateData = auth.AuthState{}
	} else if err != nil {
		return err
	}

	authStateData.Device = device
	st.Set("auth", authStateData)

	return nil
}
