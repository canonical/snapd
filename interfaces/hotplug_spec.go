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
	"fmt"
)

type HotplugSpec struct {
	deviceKey string
}

func NewHotplugSpec(deviceKey string) (*HotplugSpec, error) {
	if deviceKey == "" {
		return nil, fmt.Errorf("invalid device key %q", deviceKey)
	}
	return &HotplugSpec{
		deviceKey: deviceKey,
	}, nil
}

// SetDeviceKey can be used by interfaces to set custom device key.
func (h *HotplugSpec) SetDeviceKey(deviceKey string) error {
	if deviceKey == "" {
		return fmt.Errorf("invalid device key %q", deviceKey)
	}
	h.deviceKey = deviceKey
	return nil
}

func (h *HotplugSpec) AddSlot(name, label string, attrs map[string]interface{}) {
	// TODO
}

// HotplugDeviceHandler can be implemented by Interfaces that need to create slots in response to hotplug events
type HotplugDeviceHandler interface {
	HotplugDeviceAdd(di *HotplugDeviceInfo, spec *HotplugSpec) error
}
