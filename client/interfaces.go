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

// Plug represents a capacity offered by a snap.
type Plug struct {
	Snap      string                 `json:"snap"`
	Plug      string                 `json:"plug"`
	Interface string                 `json:"interface,omitempty"`
	Attrs     map[string]interface{} `json:"attrs,omitempty"`
	Apps      []string               `json:"apps,omitempty"`
	Label     string                 `json:"label,omitempty"`
}

// Slot represents the potential of a given snap to connect to a given plug.
type Slot struct {
	Snap      string                 `json:"snap"`
	Slot      string                 `json:"slot"`
	Interface string                 `json:"interface,omitempty"`
	Attrs     map[string]interface{} `json:"attrs,omitempty"`
	Apps      []string               `json:"apps,omitempty"`
	Label     string                 `json:"label,omitempty"`
}

// PlugConnections represents a single plug and slots that are connected to it.
type PlugConnections struct {
	Plug
	Connections []Slot `json:"connections"`
}

// SlotConnections represents a single slot and plugs that are connected to it.
type SlotConnections struct {
	Slot
	Connections []Plug `json:"connections"`
}

// InterfaceConnections contains information about all plugs, slots and their connections
type InterfaceConnections struct {
	Plugs []PlugConnections `json:"plugs"`
	Slots []SlotConnections `json:"slots"`
}

// InterfaceAction represents an action performed on the interface system.
type InterfaceAction struct {
	Action string `json:"action"`
	Plug   *Plug  `json:"plug,omitempty"`
	Slot   *Slot  `json:"slot,omitempty"`
}

// AllPlugsAndSlots returns information about all the plugs and their connections.
func (client *Client) InterfaceConnections() (connections InterfaceConnections, err error) {
	err = client.doSync("GET", "/2.0/interfaces", nil, nil, &connections)
	return
}

// performInterfaceAction performs a single action on the interface system.
func (client *Client) performInterfaceAction(sa *InterfaceAction) error {
	b, err := json.Marshal(sa)
	if err != nil {
		return err
	}
	var rsp interface{}
	if err := client.doSync("POST", "/2.0/interfaces", nil, bytes.NewReader(b), &rsp); err != nil {
		return err
	}
	return nil
}

// Connect establishes a connection between a plug and a slot.
// The plug and the slot must have the same interface.
func (client *Client) Connect(plugSnapName, plugName, slotSnapName, slotName string) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "connect",
		Plug: &Plug{
			Snap: plugSnapName,
			Plug: plugName,
		},
		Slot: &Slot{
			Snap: slotSnapName,
			Slot: slotName,
		},
	})
}

// Disconnect breaks the connection between a plug and a slot.
func (client *Client) Disconnect(plugSnapName, plugName, slotSnapName, slotName string) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "disconnect",
		Plug: &Plug{
			Snap: plugSnapName,
			Plug: plugName,
		},
		Slot: &Slot{
			Snap: slotSnapName,
			Slot: slotName,
		},
	})
}

// AddPlug adds a plug to the interface system.
func (client *Client) AddPlug(plug *Plug) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "add-plug",
		Plug:   plug,
	})
}

// RemovePlug removes a plug from the interface system.
func (client *Client) RemovePlug(snapName, plugName string) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "remove-plug",
		Plug: &Plug{
			Snap: snapName,
			Plug: plugName,
		},
	})
}

// AddSlot adds a slot to the system.
func (client *Client) AddSlot(slot *Slot) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "add-slot",
		Slot:   slot,
	})
}

// RemoveSlot removes a slot from the system.
func (client *Client) RemoveSlot(snapName, slotName string) error {
	return client.performInterfaceAction(&InterfaceAction{
		Action: "remove-slot",
		Slot: &Slot{
			Snap: snapName,
			Slot: slotName,
		},
	})
}
