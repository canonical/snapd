// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const cameraSummary = `allows access to all cameras`

type cameraInterface struct{}

const cameraBaseDeclarationSlots = `
  camera:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const cameraConnectedPlugAppArmor = `
# Until we have proper device assignment, allow access to all cameras
/dev/video[0-9]* rw,

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
/sys/class/video4linux/ r,
/sys/devices/pci**/usb*/**/video4linux/** r,
`

const cameraConnectedPlugAppArmorHotplug = `
###PATH### rw,

# Allow listing of all devices, however only the designated camera can discovered
/sys/bus/usb/devices/ r,
###DEVPATH### r,
/run/udev/data/c81:###MINOR### r, # video4linux (/dev/video*, etc)
/sys/class/video4linux/ r,
`

var cameraConnectedPlugUDev = []string{`KERNEL=="###KERNEL###"`}

// Name of the serial-port interface.
func (iface *cameraInterface) Name() string {
	return "camera"
}

func (iface *cameraInterface) String() string {
	return iface.Name()
}

func (iface *cameraInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              cameraSummary,
		BaseDeclarationSlots: cameraBaseDeclarationSlots,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
	}
}

func (iface *cameraInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	return sanitizeSlotReservedForOS(iface, slot)
}

func (iface *cameraInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var path, devpath, minor string
	// if path/devpath/minor attributes are set by hotplug, then use very precise rules
	if slot.Attr("path", &path) == nil && slot.Attr("devpath", &devpath) == nil && slot.Attr("minor", &minor) == nil {
		snippet := strings.Replace(cameraConnectedPlugAppArmorHotplug, "###PATH###", path, -1)
		snippet = strings.Replace(snippet, "###DEVPATH###", devpath, -1)
		snippet = strings.Replace(snippet, "###MINOR###", minor, -1)
		spec.AddSnippet(snippet)
	} else {
		spec.AddSnippet(cameraConnectedPlugAppArmor)
	}
	return nil
}

func (iface *cameraInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	kernel := "video[0-9]*"
	var path string
	// if path attribute is set by hotplug, then tag specific device
	if slot.Attr("path", &path) == nil {
		if dir, file := filepath.Split(path); dir == "/dev/" {
			kernel = file
		}
	}
	for _, rule := range cameraConnectedPlugUDev {
		snippet := strings.Replace(rule, "###KERNEL###", kernel, -1)
		spec.TagDevice(snippet)
	}
	return nil
}

func (iface *cameraInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func (iface *cameraInterface) HotplugDeviceDetected(di *hotplug.HotplugDeviceInfo, spec *hotplug.Specification) error {
	if di.Subsystem() == "video4linux" && strings.HasPrefix(di.DeviceName(), "/dev/video") {
		slot := hotplug.RequestedSlotSpec{
			Attrs: map[string]interface{}{
				"path":    di.DeviceName(), // e.g. /dev/video0
				"devpath": di.DevicePath(), // e.g. /sys/devices/pci0000:00/0000:00:14.0/usb1/1-11/1-11:1.0/video4linux/video0
				"minor":   di.Minor(),
			},
		}
		return spec.SetSlot(&slot)
	}
	return nil
}

func init() {
	registerIface(&cameraInterface{})
}
