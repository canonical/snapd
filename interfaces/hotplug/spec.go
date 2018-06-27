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
)

type SlotSpec struct {
	Name  string
	Label string
	Attrs map[string]interface{}
}

type Specification struct {
	slots map[string]SlotSpec
}

func NewSpecification() *Specification {
	return &Specification{
		slots: make(map[string]SlotSpec),
	}
}

func (h *Specification) AddSlot(name, label string, attrs map[string]interface{}) error {
	if _, ok := h.slots[name]; ok {
		return fmt.Errorf("slot %q already exists", name)
	}
	// TODO: use ValidateName here (after moving to utils)
	if attrs == nil {
		attrs = make(map[string]interface{})
	}
	h.slots[name] = SlotSpec{
		Name:  name,
		Label: label,
		Attrs: utils.NormalizeInterfaceAttributes(attrs).(map[string]interface{}),
	}
	return nil
}

func (h *Specification) Slots() []SlotSpec {
	slots := make([]SlotSpec, len(h.slots))
	for _, s := range h.slots {
		slots = append(slots, SlotSpec{
			Name:  s.Name,
			Label: s.Label,
			Attrs: s.Attrs,
		})
	}
	return slots
}
