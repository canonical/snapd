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

package client

// Connection describes a connection between a plug and a slot
type Connection struct {
	Slot      SlotRef `json:"slot"`
	Plug      PlugRef `json:"plug"`
	Interface string  `json:"interface"`
	// Manual is set for connections that were established manually
	Manual bool `json:"manual,omitempty"`
	// Gadget is set for connections that were enabled by the gadget snap
	Gadget bool `json:"gadget,omitempty"`
}

// Connections contains information about connections, as well as related plugs
// and slots
type Connections struct {
	// Established is the list of connections that are currently present
	Established []Connection `json:"established,omitempty"`
	// Undersired is a list of connections that are explicitly denied
	Undesired []Connection `json:"undesired,omitempty"`
	Plugs     []Plug       `json:"plugs"`
	Slots     []Slot       `json:"slots"`
}
