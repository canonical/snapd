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
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func MockDeviceManagerReboot(f func(*devicestate.DeviceManager, string, string) error) (restore func()) {
	old := deviceManagerReboot
	deviceManagerReboot = f
	return func() {
		deviceManagerReboot = old
	}
}

type (
	SystemsResponse = systemsResponse
)

func MockDeviceManagerSystemAndGadgetAndKernelInfo(f func(*devicestate.DeviceManager, string) (*devicestate.System, *gadget.Info, *snap.Info, error)) (restore func()) {
	restore = testutil.Backup(&deviceManagerSystemAndGadgetAndKernelInfo)
	deviceManagerSystemAndGadgetAndKernelInfo = f
	return restore
}

func MockDeviceManagerEncryptionSupportInfo(f func(dm *devicestate.DeviceManager, model *asserts.Model, kernelInfo *snap.Info, gadgetInfo *gadget.Info) (devicestate.EncryptionSupportInfo, error)) (restore func()) {
	restore = testutil.Backup(&deviceManagerEncryptionSupportInfo)
	deviceManagerEncryptionSupportInfo = f
	return restore
}
