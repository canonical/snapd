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
	"sort"

	"github.com/snapcore/snapd/interfaces/utils"
)

// SlotSpec is a definition of the slot to create in response to udev event.
type SlotSpec struct {
	Name  string
	Label string
	Attrs map[string]interface{}
}

type Specification struct {
	slots map[string]*SlotSpec
}

func NewSpecification() *Specification {
	return &Specification{
		slots: make(map[string]*SlotSpec),
	}
}

func (h *Specification) AddSlot(slotSpec *SlotSpec) error {
	if _, ok := h.slots[slotSpec.Name]; ok {
		return fmt.Errorf("slot %q already exists", slotSpec.Name)
	}
	// TODO: use ValidateName here (after moving to utils)
	attrs := slotSpec.Attrs
	if attrs == nil {
		attrs = make(map[string]interface{})
	} else {
		attrs = utils.CopyAttributes(slotSpec.Attrs)
	}
	h.slots[slotSpec.Name] = &SlotSpec{
		Name:  slotSpec.Name,
		Label: slotSpec.Label,
		Attrs: utils.NormalizeInterfaceAttributes(attrs).(map[string]interface{}),
	}
	return nil
}

func (h *Specification) Slots() []*SlotSpec {
	keys := make([]string, 0, len(h.slots))
	for k := range h.slots {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	slots := make([]*SlotSpec, 0, len(h.slots))
	for _, k := range keys {
		slots = append(slots, h.slots[k])
	}
	return slots
}
