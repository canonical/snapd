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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/state"
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

func MockDeviceManagerSystemAndGadgetAndEncryptionInfo(f func(*devicestate.DeviceManager, string) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error)) (restore func()) {
	restore = testutil.Backup(&deviceManagerSystemAndGadgetAndEncryptionInfo)
	deviceManagerSystemAndGadgetAndEncryptionInfo = f
	return restore
}

func MockDevicestateInstallFinish(f func(*state.State, string, map[string]*gadget.Volume, *devicestate.OptionalInstall) (*state.Change, error)) (restore func()) {
	restore = testutil.Backup(&devicestateInstallFinish)
	devicestateInstallFinish = f
	return restore
}

func MockDevicestateInstallSetupStorageEncryption(f func(*state.State, string, map[string]*gadget.Volume) (*state.Change, error)) (restore func()) {
	restore = testutil.Backup(&devicestateInstallSetupStorageEncryption)
	devicestateInstallSetupStorageEncryption = f
	return restore
}

func MockDevicestateCreateRecoverySystem(f func(*state.State, string, devicestate.CreateRecoverySystemOptions) (*state.Change, error)) (restore func()) {
	restore = testutil.Backup(&devicestateCreateRecoverySystem)
	devicestateCreateRecoverySystem = f
	return restore
}

func MockDevicestateRemoveRecoverySystem(f func(*state.State, string) (*state.Change, error)) (restore func()) {
	restore = testutil.Backup(&devicestateRemoveRecoverySystem)
	devicestateRemoveRecoverySystem = f
	return restore
}
