// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package hotplug

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// HotplugDeviceInfo carries information about added/removed device detected at runtime.
type HotplugDeviceInfo struct {
	// kobj name reported by uevent (corresponds to DEVPATH).
	object string
	// map of all attributes returned for given uevent.
	data map[string]string
}

// NewHotplugDeviceInfo creates HotplugDeviceInfo structure related to udev add or remove event.
func NewHotplugDeviceInfo(obj string, env map[string]string) *HotplugDeviceInfo {
	return &HotplugDeviceInfo{
		object: obj,
		data:   env,
	}
}

// Returns object path, i.e. the sysfs path of the device, e.g. /devices/pci0000:00/0000:00:14.0/usb1/1-2.
// It is expected to be equal to DEVPATH attribute.
func (h *HotplugDeviceInfo) Object() string {
	return h.object
}

// Returns the value of "SUBSYSTEM" attribute of the udev event associated with the device, e.g. "usb".
// Subsystem value is always present.
func (h *HotplugDeviceInfo) Subsystem() string {
	return h.data["SUBSYSTEM"]
}

// Returns full device path under /sysfs, e.g /sys/devices/pci0000:00/0000:00:14.0/usb1/1-2.
// The path is derived from DEVPATH attribute of the udev event.
func (h *HotplugDeviceInfo) Path() string {
	path, ok := h.Attribute("DEVPATH")
	if ok {
		return filepath.Join(dirs.SysfsDir, path)
	}
	return ""
}

// Returns the value of "MINOR" attribute of the udev event associated with the device.
// The Minor value may be empty.
func (h *HotplugDeviceInfo) Minor() string {
	return h.data["MINOR"]
}

// Returns the value of "MAJOR" attribute of the udev event associated with the device.
// The Major value may be empty.
func (h *HotplugDeviceInfo) Major() string {
	return h.data["MAJOR"]
}

// Returns the value of "DEVNAME" attribute of the udev event associated with the device, e.g. "ttyUSB0".
// The DeviceName value may be empty.
func (h *HotplugDeviceInfo) DeviceName() string {
	return h.data["DEVNAME"]
}

// Returns the value of "DEVTYPE" attribute of the udev event associated with the device, e.g. "usb_device".
// The DeviceType value may be empty.
func (h *HotplugDeviceInfo) DeviceType() string {
	return h.data["DEVTYPE"]
}

// Generic method for getting arbitrary attribute from the uevent data.
func (h *HotplugDeviceInfo) Attribute(name string) (string, bool) {
	val, ok := h.data[name]
	return val, ok
}
