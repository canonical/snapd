// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func MockDevicestateRemodel(mock func(*state.State, *asserts.Model, []*snap.SideInfo, []string, devicestate.RemodelOptions) (*state.Change, error)) (restore func()) {
	oldDevicestateRemodel := devicestateRemodel
	devicestateRemodel = mock
	return func() {
		devicestateRemodel = oldDevicestateRemodel
	}
}

func MockDevicestateDeviceManagerUnregister(mock func(*devicestate.DeviceManager, *devicestate.UnregisterOptions) error) (restore func()) {
	oldDevicestateDeviceManagerUnregister := devicestateDeviceManagerUnregister
	devicestateDeviceManagerUnregister = mock
	return func() {
		devicestateDeviceManagerUnregister = oldDevicestateDeviceManagerUnregister
	}
}

type (
	PostModelData = postModelData
)
