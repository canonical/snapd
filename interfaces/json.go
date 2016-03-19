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

// PlugJSON aids in marshaling Plug into JSON.
type PlugJSON struct {
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
	return json.Marshal(&PlugJSON{
		Snap:        plug.Snap.Name,
		Name:        plug.Name,
		Interface:   plug.Interface,
		Attrs:       plug.Attrs,
		Apps:        plug.AppNames(),
		Label:       plug.Label,
		Connections: plug.Connections,
	})
}

// SlotJSON aids in marshaling Slot into JSON.
type SlotJSON struct {
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
	return json.Marshal(&SlotJSON{
		Snap:        slot.Snap.Name,
		Name:        slot.Name,
		Interface:   slot.Interface,
		Attrs:       slot.Attrs,
		Apps:        slot.AppNames(),
		Label:       slot.Label,
		Connections: slot.Connections,
	})
}
