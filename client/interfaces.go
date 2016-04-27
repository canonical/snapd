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

package client

import (
	"bytes"
	"encoding/json"
)

// Plug represents the potential of a given snap to connect to a slot.
type Plug struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"plug"`
	Interface   string                 `json:"interface,omitempty"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label,omitempty"`
	Connections []SlotRef              `json:"connections,omitempty"`
}

// PlugRef is a reference to a plug.
type PlugRef struct {
	Snap string `json:"snap"`
	Name string `json:"plug"`
}

// Slot represents a capacity offered by a snap.
type Slot struct {
	Snap        string                 `json:"snap"`
	Name        string                 `json:"slot"`
	Interface   string                 `json:"interface,omitempty"`
	Attrs       map[string]interface{} `json:"attrs,omitempty"`
	Apps        []string               `json:"apps,omitempty"`
	Label       string                 `json:"label,omitempty"`
	Connections []PlugRef              `json:"connections,omitempty"`
}

// SlotRef is a reference to a slot.
type SlotRef struct {
	Snap string `json:"snap"`
	Name string `json:"slot"`
}

// Interfaces contains information about all plugs, slots and their connections
type Interfaces struct {
	Plugs []Plug `json:"plugs"`
	Slots []Slot `json:"slots"`
}

// InterfaceAction represents an action performed on the interface system.
type InterfaceAction struct {
	Action string `json:"action"`
	Plugs  []Plug `json:"plugs,omitempty"`
	Slots  []Slot `json:"slots,omitempty"`
}

// Interfaces returns all plugs, slots and their connections.
func (client *Client) Interfaces() (interfaces Interfaces, err error) {
	_, err = client.doSync("GET", "/v2/interfaces", nil, nil, nil, &interfaces)
	return
}

// performInterfaceAction performs a single action on the interface system.
func (client *Client) performInterfaceAction(sa *InterfaceAction) (changeID string, err error) {
	b, err := json.Marshal(sa)
	if err != nil {
		return "", err
	}
	return client.doAsync("POST", "/v2/interfaces", nil, nil, bytes.NewReader(b))
}

// Connect establishes a connection between a plug and a slot.
// The plug and the slot must have the same interface.
func (client *Client) Connect(plugSnapName, plugName, slotSnapName, slotName string) (changeID string, err error) {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "connect",
		Plugs:  []Plug{{Snap: plugSnapName, Name: plugName}},
		Slots:  []Slot{{Snap: slotSnapName, Name: slotName}},
	})
}

// Disconnect breaks the connection between a plug and a slot.
func (client *Client) Disconnect(plugSnapName, plugName, slotSnapName, slotName string) (changeID string, err error) {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "disconnect",
		Plugs:  []Plug{{Snap: plugSnapName, Name: plugName}},
		Slots:  []Slot{{Snap: slotSnapName, Name: slotName}},
	})
}
