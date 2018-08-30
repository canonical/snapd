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

	"github.com/snapcore/snapd/interfaces/utils"

	"github.com/snapcore/snapd/snap"
)

// Definer can be implemented by interfaces that need to create slots in response to hotplug events
type Definer interface {
	HotplugDeviceDetected(di *HotplugDeviceInfo, spec *Specification) error
}

// SlotSpec is a definition of the slot to create in response to udev event.
type SlotSpec struct {
	// XXX: Name is the name the interface wants to give to the slot; we
	// might want to mediate this though (e.g. generate automatically), so this
	// may change/go away.
	Name  string
	Label string
	Attrs map[string]interface{}
}

// Specification contains a slot definition to create in response to uevent.
type Specification struct {
	slot *SlotSpec
}

// NewSpecification creates an empty hotplug Specification.
func NewSpecification() *Specification {
	return &Specification{}
}

// SetSlot adds a specification of a slot.
func (h *Specification) SetSlot(slotSpec *SlotSpec) error {
	if h.slot != nil {
		return fmt.Errorf("slot specification already created")
	}
	if err := snap.ValidateSlotName(slotSpec.Name); err != nil {
		return err
	}
	attrs := slotSpec.Attrs
	if attrs == nil {
		attrs = make(map[string]interface{})
	} else {
		attrs = utils.CopyAttributes(slotSpec.Attrs)
	}
	h.slot = &SlotSpec{
		Name:  slotSpec.Name,
		Label: slotSpec.Label,
		Attrs: utils.NormalizeInterfaceAttributes(attrs).(map[string]interface{}),
	}
	return nil
}

// Slot returns specification of the slot created by given interface.
func (h *Specification) Slot() *SlotSpec {
	return h.slot
}
