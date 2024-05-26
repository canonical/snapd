// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/snap"
)

// Definer can be implemented by interfaces that need to create slots in response to hotplug events.
type Definer interface {
	// HotplugDeviceDetected is called for all devices and should return nil slot for those that are irrelevant for the interface.
	// Error should only be returned in rare cases when device is relevant, but there is a problem with creating a proposed slot for it.
	HotplugDeviceDetected(di *HotplugDeviceInfo) (*ProposedSlot, error)
}

// HotplugKeyHandler can be implemented by interfaces that need to provide a non-standard key for hotplug devices.
type HotplugKeyHandler interface {
	HotplugKey(di *HotplugDeviceInfo) (snap.HotplugKey, error)
}

// HandledByGadgetPredicate can be implemented by hotplug interfaces to decide whether a device is already handled by given gadget slot.
type HandledByGadgetPredicate interface {
	HandledByGadget(di *HotplugDeviceInfo, slot *snap.SlotInfo) bool
}

// ProposedSlot is a definition of the slot to create in response to a hotplug event.
type ProposedSlot struct {
	// Name is how the interface wants to name the slot. When left empty,
	// one will be generated on demand. The hotplug machinery appends a
	// suffix to ensure uniqueness of the name.
	Name  string                 `json:"name"`
	Label string                 `json:"label"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}

// Clean returns a copy of the input slot with normalized attributes and validated slot name (unless its empty).
func (slot *ProposedSlot) Clean() (*ProposedSlot, error) {
	// only validate name if not empty, otherwise name is created by hotplug
	// subsystem later on when the proposed slot is processed.
	if slot.Name != "" {
		mylog.Check(snap.ValidateSlotName(slot.Name))
	}
	attrs := slot.Attrs
	if attrs == nil {
		attrs = make(map[string]interface{})
	}

	return &ProposedSlot{
		Name:  slot.Name,
		Label: slot.Label,
		Attrs: utils.NormalizeInterfaceAttributes(attrs).(map[string]interface{}),
	}, nil
}
