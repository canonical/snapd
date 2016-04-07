// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"encoding/json"
)

// plugJSON aids in marshaling Plug into JSON.
type plugJSON struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"plug"`
	Interface   string                 `json:"interface"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label"`
	Connections []SlotRef              `json:"connections,omitempty"`
}

// MarshalJSON returns the JSON encoding of plug.
func (plug *Plug) MarshalJSON() ([]byte, error) {
	var names []string
	for name := range plug.Apps {
		names = append(names, name)
	}
	return json.Marshal(&plugJSON{
		Snap:        plug.Snap.Name(),
		Name:        plug.Name,
		Interface:   plug.Interface,
		Attrs:       plug.Attrs,
		Apps:        names,
		Label:       plug.Label,
		Connections: plug.Connections,
	})
}

// slotJSON aids in marshaling Slot into JSON.
type slotJSON struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"slot"`
	Interface   string                 `json:"interface"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label"`
	Connections []PlugRef              `json:"connections,omitempty"`
}

// MarshalJSON returns the JSON encoding of slot.
func (slot *Slot) MarshalJSON() ([]byte, error) {
	var names []string
	for name := range slot.Apps {
		names = append(names, name)
	}
	return json.Marshal(&slotJSON{
		Snap:        slot.Snap.Name(),
		Name:        slot.Name,
		Interface:   slot.Interface,
		Attrs:       slot.Attrs,
		Apps:        names,
		Label:       slot.Label,
		Connections: slot.Connections,
	})
}
