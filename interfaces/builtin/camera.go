// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package builtin

import (
	"strings"

	"github.com/snapcore/snapd/release"
)

const cameraSummary = `allows access to all cameras`

const cameraBaseDeclarationSlots = `
  camera:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const cameraConnectedPlugAppArmor = `
# Until we have proper device assignment, allow access to all cameras
###PROMPT### /dev/video[0-9]* rwk,

# VideoCore cameras (shared device with VideoCore/EGL)
###PROMPT### /dev/vchiq rw,

# Allow detection of cameras. Leaks plugged in USB device info
/sys/bus/usb/devices/ r,
/sys/devices/pci**/usb*/**/busnum r,
/sys/devices/pci**/usb*/**/devnum r,
/sys/devices/pci**/usb*/**/idVendor r,
/sys/devices/pci**/usb*/**/idProduct r,
/sys/devices/pci**/usb*/**/interface r,
/sys/devices/pci**/usb*/**/modalias r,
/sys/devices/pci**/usb*/**/speed r,
/run/udev/data/c81:[0-9]* r, # video4linux (/dev/video*, etc)
/run/udev/data/+usb:* r,
/sys/class/video4linux/ r,
/sys/devices/pci**/usb*/**/video4linux/** r,
/sys/devices/platform/**/usb*/**/video4linux/** r,
`

const cameraConnectedPlugTouchAppArmor = `
# Support for Android-based Camera stack on Ubuntu Touch
/android{,/**} r,
/{,android/}system/build.prop r,
/{,android/}vendor/build.prop r,
/{,android/}odm/build.prop    r,

# libcamera_compat_layer doesn't require proprietary vendor blobs
/{,android/}system/lib{,64}/** r,
/{,android/}system/lib{,64}/**.so m,

# Commonly expected Android paths
/{,dev/}socket/property_service rw,
/{,dev/}socket/logdw rw,
/{,dev/}__properties__/** r,

# Allow access to the CameraService on app-only binder
/dev/{,binderfs/}binder rw,
`

// DetectCameraFromPath returns true if the given path corresponds to an
// AppArmor rule with the prompt prefix from the camera interface.
//
// XXX: this is only necessary until metadata tags are fully supported by the
// AppArmor parser and kernel. Then, this function should be removed.
func DetectCameraFromPath(path string) bool {
	if strings.HasPrefix(path, "/dev/video") || path == "/dev/vchiq" {
		return true
	}
	return false
}

var cameraConnectedPlugUDev = []string{
	`KERNEL=="video[0-9]*"`,
	`KERNEL=="vchiq"`,
}

func init() {
	connectedPlugAppArmor := cameraConnectedPlugAppArmor
	if release.OnTouch {
		connectedPlugAppArmor += cameraConnectedPlugTouchAppArmor
	}

	registerIface(&commonInterface{
		name:                  "camera",
		summary:               cameraSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  cameraBaseDeclarationSlots,
		connectedPlugAppArmor: connectedPlugAppArmor,
		connectedPlugUDev:     cameraConnectedPlugUDev,
	})
}
