// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

package devicestatetest

import (
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
)

func Device(st *state.State) (*auth.DeviceState, error) {
	return internal.Device(st)
}

func SetDevice(st *state.State, device *auth.DeviceState) error {
	return internal.SetDevice(st, device)
}

// MarkInitialized flags the state as seeded and the device registered to avoid
// running the seeding code etc in tests, it also tries to sets the model to
// generic-classic.
// If the initial model is imporant this cannot be used.
func MarkInitialized(st *state.State) {
	model := sysdb.GenericClassicModel()
	// best-effort
	assertstate.Add(st, model)
	st.Set("seeded", true)
	SetDevice(st, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserialserial",
	})
}
