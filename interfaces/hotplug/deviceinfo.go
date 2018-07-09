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
	"github.com/snapcore/snapd/dirs"
	"io/ioutil"
	"path/filepath"
)

// HotplugDeviceInfo carries information about added/removed device detected at runtime.
type HotplugDeviceInfo struct {
	// map of all attributes returned for given uevent.
	data                                               map[string]string
	idVendor, idProduct, product, manufacturer, serial string
}

// NewHotplugDeviceInfo creates HotplugDeviceInfo structure related to udev add or remove event.
func NewHotplugDeviceInfo(env map[string]string) (*HotplugDeviceInfo, error) {
	if _, ok := env["DEVPATH"]; !ok {
		return nil, fmt.Errorf("missing device path attribute")
	}
	return &HotplugDeviceInfo{
		data: env,
	}, nil
}

// Returns the value of "SUBSYSTEM" attribute of the udev event associated with the device, e.g. "usb".
// Subsystem value is always present.
func (h *HotplugDeviceInfo) Subsystem() string {
	return h.data["SUBSYSTEM"]
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

func (h *HotplugDeviceInfo) IdVendor() string {
	return h.readOnceMaybe("idVendor", &h.idVendor)
}

func (h *HotplugDeviceInfo) IdProduct() string {
	return h.readOnceMaybe("idProduct", &h.idProduct)
}

func (h *HotplugDeviceInfo) Product() string {
	return h.readOnceMaybe("product", &h.product)
}

func (h *HotplugDeviceInfo) Manufacturer() string {
	return h.readOnceMaybe("manufacturer", &h.manufacturer)
}

func (h *HotplugDeviceInfo) Serial() string {
	return h.readOnceMaybe("serial", &h.serial)
}

func (h *HotplugDeviceInfo) readOnceMaybe(fileName string, out *string) string {
	if *out == "" {
		data, err := ioutil.ReadFile(filepath.Join(h.DevicePath(), fileName))
		if err != nil {
			return ""
		}
		*out = string(data)
	}
	return *out
}
