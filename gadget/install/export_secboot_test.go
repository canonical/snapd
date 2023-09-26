// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package install

import (
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/testutil"
)

var (
	DiskWithSystemSeed     = diskWithSystemSeed
	NewEncryptedDeviceLUKS = newEncryptedDeviceLUKS
)

func MockSecbootFormatEncryptedDevice(f func(key keys.EncryptionKey, encType secboot.EncryptionType, label, node string) error) (restore func()) {
	r := testutil.Backup(&secbootFormatEncryptedDevice)
	secbootFormatEncryptedDevice = f
	return r

}

func MockBootRunFDESetupHook(f func(req *fde.SetupRequest) ([]byte, error)) (restore func()) {
	r := testutil.Backup(&boot.RunFDESetupHook)
	boot.RunFDESetupHook = f
	return r
}
