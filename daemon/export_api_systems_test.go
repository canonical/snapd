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
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
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

func MockDeviceManagerSystemAndGadgetAndEncryptionInfo(f func(
	*devicestate.DeviceManager,
	string,
	bool,
) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error)) (restore func()) {
	return testutil.Mock(&deviceManagerSystemAndGadgetAndEncryptionInfo, f)
}

func MockDeviceManagerApplyActionOnSystemAndGadgetAndEncryptionInfo(f func(
	*devicestate.DeviceManager,
	string,
	*secboot.PreinstallAction,
) (*devicestate.System, *gadget.Info, *install.EncryptionSupportInfo, error)) (restore func()) {
	return testutil.Mock(&deviceManagerApplyActionOnSystemAndGadgetAndEncryptionInfo, f)
}

func MockDevicestateInstallFinish(f func(*state.State, string, map[string]*gadget.Volume, *devicestate.OptionalContainers) (*state.Change, error)) (restore func()) {
	return testutil.Mock(&devicestateInstallFinish, f)
}

func MockDevicestateInstallSetupStorageEncryption(f func(*state.State, string, map[string]*gadget.Volume, *device.VolumesAuthOptions) (*state.Change, error)) (restore func()) {
	return testutil.Mock(&devicestateInstallSetupStorageEncryption, f)
}

func MockDevicestateCreateRecoverySystem(f func(*state.State, string, devicestate.CreateRecoverySystemOptions) (*state.Change, error)) (restore func()) {
	return testutil.Mock(&devicestateCreateRecoverySystem, f)
}

func MockDevicestateRemoveRecoverySystem(f func(*state.State, string) (*state.Change, error)) (restore func()) {
	return testutil.Mock(&devicestateRemoveRecoverySystem, f)
}

func MockDevicestateGeneratePreInstallRecoveryKey(f func(st *state.State, label string) (rkey keys.RecoveryKey, err error)) (restore func()) {
	return testutil.Mock(&devicestateGeneratePreInstallRecoveryKey, f)
}

func MockDeviceValidatePassphrase(f func(mode device.AuthMode, passphrase string) (device.AuthQuality, error)) (restore func()) {
	return testutil.Mock(&deviceValidatePassphrase, f)
}
