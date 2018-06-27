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
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

type HotplugDeviceInfo struct {
	object       string
	Data         map[string]string
	idVendor     string
	idProduct    string
	product      string
	manufacturer string
	serial       string
}

func NewHotplugDeviceInfo(obj string, env map[string]string) *HotplugDeviceInfo {
	return &HotplugDeviceInfo{
		object: obj,
		Data:   env,
	}
}

func (h *HotplugDeviceInfo) Object() string {
	return h.object
}

func (h *HotplugDeviceInfo) Path() string {
	return filepath.Join(dirs.SysDir, h.Data["DEVPATH"])
}

func (h *HotplugDeviceInfo) Subsystem() string {
	return h.Data["SUBSYSTEM"]
}

func (h *HotplugDeviceInfo) Minor() string {
	return h.Data["MINOR"]
}

func (h *HotplugDeviceInfo) Major() string {
	return h.Data["MAJOR"]
}

func (h *HotplugDeviceInfo) DeviceName() string {
	return h.Data["DEVNAME"]
}

func (h *HotplugDeviceInfo) DeviceType() string {
	return h.Data["DEVTYPE"]
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
		data, err := ioutil.ReadFile(filepath.Join(h.Path(), fileName))
		if err != nil {
			return ""
		}
		*out = string(data)
	}
	return *out
}

// HotplugDeviceHandler can be implemented by interfaces that need to create slots in response to hotplug events
type HotplugDeviceHandler interface {
	HotplugDeviceDetected(di *HotplugDeviceInfo, spec *Specification) error
}

// HotplugDeviceInfo can be implemented by interfaces that need to provide a non-standard device key for hotplug devices
type HotplugDeviceKeyHandler interface {
	HotplugDeviceKey(di *HotplugDeviceInfo) (string, error)
}
