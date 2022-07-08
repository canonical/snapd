// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

// Package schemas holds structs for reading and writing interface-related data.
package schemas

import "github.com/snapcore/snapd/snap"

// Connection holds properties of an interface connection.
type Connection struct {
	Auto      bool   `json:"auto,omitempty" yaml:"auto"`
	ByGadget  bool   `json:"by-gadget,omitempty" yaml:"by-gadget"`
	Interface string `json:"interface,omitempty" yaml:"interface"`
	// Undesired tracks connections that were manually disconnected after being auto-connected,
	// so that they are not automatically reconnected again in the future.
	Undesired        bool                   `json:"undesired,omitempty" yaml:"undesired"`
	StaticPlugAttrs  map[string]interface{} `json:"plug-static,omitempty" yaml:"plug-static,omitempty"`
	DynamicPlugAttrs map[string]interface{} `json:"plug-dynamic,omitempty" yaml:"plug-dynamic,omitempty"`
	StaticSlotAttrs  map[string]interface{} `json:"slot-static,omitempty" yaml:"slot-static,omitempty"`
	DynamicSlotAttrs map[string]interface{} `json:"slot-dynamic,omitempty" yaml:"slot-dynamic,omitempty"`
	// Hotplug-related attributes: HotplugGone indicates a connection that
	// disappeared because the device was removed, but may potentially be
	// restored in the future if we see the device again. HotplugKey is the
	// key of the associated device; it's empty for connections of regular
	// slots.
	HotplugGone bool            `json:"hotplug-gone,omitempty" yaml:"hotplug-gone,omitempty"`
	HotplugKey  snap.HotplugKey `json:"hotplug-key,omitempty" yaml:"hotplug-key,omitempty"`
}
