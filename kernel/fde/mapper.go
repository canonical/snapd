// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021  Canonical Ltd
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

package fde

import (
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/osutil/disks"
)

// IsEncryptedDevice returns true when the provided device mapper name indicates
// that it is encrypted using FDE hooks.
func IsHardwareEncryptedDeviceMapperName(dmName string) bool {
	// TODO: is there anything more we can use to limit the prefix of the
	// dmName?
	return dmName != "-device-locked" && strings.HasSuffix(dmName, "-device-locked")
}

// DeviceUnlockKernelHookDeviceMapperBackResolver is a back resolver to be used
// with disks.RegisterDeviceMapperBackResolver for devices that implement full
// disk encryption via hardware devices with kernel snap hooks.
func DeviceUnlockKernelHookDeviceMapperBackResolver(dmUUID, dmName []byte) (dev string, ok bool) {
	if !IsHardwareEncryptedDeviceMapperName(string(dmName)) {
		return "", false
	}
	// this is a device encrypted using FDE hooks

	// the uuid of the mapper device is the same as the partuuid
	return filepath.Join("/dev/disk/by-partuuid", string(dmUUID)), true
}

func init() {
	disks.RegisterDeviceMapperBackResolver("device-unlock-kernel-fde", DeviceUnlockKernelHookDeviceMapperBackResolver)
}
