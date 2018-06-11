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

package interfaces

import (
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

type HotplugDeviceInfo struct {
	object string
	Data   map[string]string
}

func NewHotplugDeviceInfo(obj string, env map[string]string) *HotplugDeviceInfo {
	return &HotplugDeviceInfo{
		object: obj,
		Data:   env,
	}
}

// TODO: implement methods that traverse device path under /sys and fetch vendor/product id and serial

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

func (h *HotplugDeviceInfo) Name() string {
	return h.Data["DEVNAME"]
}

func (h *HotplugDeviceInfo) Type() string {
	return h.Data["DEVTYPE"]
}
