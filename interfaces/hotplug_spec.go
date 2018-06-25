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

type SlotSpec struct {
	Name  string
	Label string
	Attrs map[string]interface{}
}

type HotplugSpec struct {
	deviceKey string
	slots     []SlotSpec
}

func NewHotplugSpec(deviceKey string) (*HotplugSpec, error) {
	if deviceKey == "" {
		return nil, fmt.Errorf("invalid device key %q", deviceKey)
	}
	return &HotplugSpec{
		deviceKey: deviceKey,
	}, nil
}

func (h *HotplugSpec) AddSlot(name, label string, attrs map[string]interface{}) {
	// FIXME: normalize attributes
	h.slots = append(h.slots, SlotSpec{Name: name, Label: label, Attrs: attrs})
}

func (h *HotplugSpec) Slots() []SlotSpec {
	slots := make([]SlotSpec, len(h.slots))
	for _, s := range h.slots {
		slots = append(slots, SlotSpec{
			Name:  s.Name,
			Label: s.Label,
			Attrs: s.Attrs, // FIXME: deep copy
		})
	}
	return slots
}

// HotplugDeviceHandler can be implemented by interfaces that need to create slots in response to hotplug events
type HotplugDeviceHandler interface {
	HotplugDeviceDetected(di *HotplugDeviceInfo, spec *HotplugSpec) error
}

// HotplugDeviceInfo can be implemented by interfaces that need to provide a non-standard device key for hotplug devices
type HotplugDeviceKeyHandler interface {
	HotplugDeviceKey(di *HotplugDeviceInfo) (string, error)
}
