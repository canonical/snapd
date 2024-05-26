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

import (
	"net/url"

	"github.com/ddkwork/golibrary/mylog"
)

// Connection describes a connection between a plug and a slot.
type Connection struct {
	Slot      SlotRef `json:"slot"`
	Plug      PlugRef `json:"plug"`
	Interface string  `json:"interface"`
	// Manual is set for connections that were established manually.
	Manual bool `json:"manual"`
	// Gadget is set for connections that were enabled by the gadget snap.
	Gadget bool `json:"gadget"`
	// SlotAttrs is the list of attributes of the slot side of the connection.
	SlotAttrs map[string]interface{} `json:"slot-attrs,omitempty"`
	// PlugAttrs is the list of attributes of the plug side of the connection.
	PlugAttrs map[string]interface{} `json:"plug-attrs,omitempty"`
}

// Connections contains information about connections, as well as related plugs
// and slots.
type Connections struct {
	// Established is the list of connections that are currently present.
	Established []Connection `json:"established"`
	// Undersired is a list of connections that are manually denied.
	Undesired []Connection `json:"undesired"`
	Plugs     []Plug       `json:"plugs"`
	Slots     []Slot       `json:"slots"`
}

// ConnectionOptions contains criteria for selecting matching connections, plugs
// and slots.
type ConnectionOptions struct {
	// Snap selects connections with the snap on one of the sides, as well
	// as plugs and slots of a given snap.
	Snap string
	// Interface selects connections, plugs or slots using given interface.
	Interface string
	// All when true, selects established and undesired connections as well
	// as all disconnected plugs and slots.
	All bool
}

// Connections returns matching plugs, slots and their connections. Unless
// specified by matching options, returns established connections.
func (client *Client) Connections(opts *ConnectionOptions) (Connections, error) {
	var conns Connections
	query := url.Values{}
	if opts != nil && opts.Snap != "" {
		query.Set("snap", opts.Snap)
	}
	if opts != nil && opts.Interface != "" {
		query.Set("interface", opts.Interface)
	}
	if opts != nil && opts.All {
		query.Set("select", "all")
	}
	_ := mylog.Check2(client.doSync("GET", "/v2/connections", query, nil, nil, &conns))
	return conns, err
}
