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
)

type SlotSpec struct {
	Name  string
	Label string
	Attrs map[string]interface{}
}

type Specification struct {
	deviceKey string
	slots     []SlotSpec
}

func NewSpecification(deviceKey string) (*Specification, error) {
	if deviceKey == "" {
		return nil, fmt.Errorf("invalid device key %q", deviceKey)
	}
	return &Specification{
		deviceKey: deviceKey,
	}, nil
}

func (h *Specification) AddSlot(name, label string, attrs map[string]interface{}) {
	// FIXME: normalize attributes
	h.slots = append(h.slots, SlotSpec{Name: name, Label: label, Attrs: attrs})
}

func (h *Specification) Slots() []SlotSpec {
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
