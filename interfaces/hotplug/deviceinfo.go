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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

type hotplugDeviceInfoData struct {
	// map of all attributes returned for given uevent.
	Data map[string]string `json:"data"`
}

// HotplugDeviceInfo carries information about added/removed device detected at runtime.
type HotplugDeviceInfo struct {
	hotplugDeviceInfoData
}

// NewHotplugDeviceInfo creates HotplugDeviceInfo structure related to udev add or remove event.
func NewHotplugDeviceInfo(env map[string]string) (*HotplugDeviceInfo, error) {
	if _, ok := env["DEVPATH"]; !ok {
		return nil, fmt.Errorf("missing device path attribute")
	}
	return &HotplugDeviceInfo{
		hotplugDeviceInfoData: hotplugDeviceInfoData{Data: env},
	}, nil
}

// Returns the value of "SUBSYSTEM" attribute of the udev event associated with the device, e.g. "usb".
// Subsystem value is always present.
func (h *HotplugDeviceInfo) Subsystem() string {
	return h.Data["SUBSYSTEM"]
}

// Returns full device path under /sysfs, e.g /sys/devices/pci0000:00/0000:00:14.0/usb1/1-2.
// The path is derived from DEVPATH attribute of the udev event.
func (h *HotplugDeviceInfo) DevicePath() string {
	// DEVPATH is guaranteed to exist (checked in the ctor).
	path, _ := h.Attribute("DEVPATH")
	return filepath.Join(dirs.SysfsDir, path)
}

// Returns the value of "MINOR" attribute of the udev event associated with the device.
// The Minor value may be empty.
func (h *HotplugDeviceInfo) Minor() string {
	return h.Data["MINOR"]
}

// Returns the value of "MAJOR" attribute of the udev event associated with the device.
// The Major value may be empty.
func (h *HotplugDeviceInfo) Major() string {
	return h.Data["MAJOR"]
}

// Returns the value of "DEVNAME" attribute of the udev event associated with the device, e.g. "/dev/ttyUSB0".
// The DeviceName value may be empty.
func (h *HotplugDeviceInfo) DeviceName() string {
	return h.Data["DEVNAME"]
}

// Returns the value of "DEVTYPE" attribute of the udev event associated with the device, e.g. "usb_device".
// The DeviceType value may be empty.
func (h *HotplugDeviceInfo) DeviceType() string {
	return h.Data["DEVTYPE"]
}

// Generic method for getting arbitrary attribute from the uevent data.
func (h *HotplugDeviceInfo) Attribute(name string) (string, bool) {
	val, ok := h.Data[name]
	return val, ok
}

func (h *HotplugDeviceInfo) firstAttrValueOf(tryAttrs ...string) string {
	for _, attr := range tryAttrs {
		if val, _ := h.Attribute(attr); val != "" {
			return val
		}
	}
	return ""
}

func (h *HotplugDeviceInfo) String() string {
	return h.str(70)
}

// ShortString returns a string representation of the device with more aggressive truncating of model/vendor name.
func (h *HotplugDeviceInfo) ShortString() string {
	return h.str(16)
}

func (h *HotplugDeviceInfo) str(maxModelOrVendorLen int) string {
	var nameOrPath string

	// devname is the name of the device under /dev, eg. /dev/ttyS0;
	// prefer devname over devpath as this is the one used to talk to the device.
	if nameOrPath = h.DeviceName(); nameOrPath == "" {
		// devpath is the path of the device under /sys, eg. /sys/devices/pnp0/00:04/tty/ttyS0.
		nameOrPath = h.DevicePath()
	}

	modelOrVendor := h.firstAttrValueOf("ID_MODEL_FROM_DATABASE", "ID_MODEL", "ID_MODEL_ID", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR", "ID_VENDOR_ID")
	if len(modelOrVendor) > maxModelOrVendorLen {
		modelOrVendor = modelOrVendor[0:maxModelOrVendorLen] + "…"
	}

	var serial string
	if modelOrVendor != "" {
		serial = h.firstAttrValueOf("ID_SERIAL_SHORT", "ID_SERIAL")
	} else {
		serial = h.firstAttrValueOf("ID_SERIAL", "ID_SERIAL_SHORT")
	}
	hasSerial := (serial != "" && serial != "noserial")

	s := nameOrPath
	if modelOrVendor != "" || hasSerial {
		s += " ("
		if modelOrVendor != "" {
			s += modelOrVendor
			if hasSerial {
				s += "; "
			}
		}
		if hasSerial {
			s += "serial: " + serial
		}
		s += ")"
	}

	return s
}
