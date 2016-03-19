// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snap

// Info provides information about snaps.
type Info struct {
	Name        string
	Developer   string
	Version     string
	Type        Type
	Channel     string
	Description string
	Apps        map[string]*AppInfo
	Plugs       map[string]*PlugInfo
	Slots       map[string]*SlotInfo
}

// PlugInfo provides information about a plug.
type PlugInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
}

// SlotInfo provides information about a slot.
type SlotInfo struct {
	Snap *Info

	Name      string
	Interface string
	Attrs     map[string]interface{}
	Label     string
	Apps      map[string]*AppInfo
}

// AppInfo provides information about a plug.
type AppInfo struct {
	Snap *Info

	Name string

	// TODO: rest of the app fields

	plugs []string
	slots []string
}

// Plugs returns all of the plugs bound to this application.
func (app *AppInfo) Plugs() []*PlugInfo {
	plugs := make([]*PlugInfo, 0, len(app.plugs))
	for _, name := range app.plugs {
		plugs = append(plugs, app.Snap.Plugs[name])
	}
	return plugs
}

// Slots returns all of the slots bound to this application.
func (app *AppInfo) Slots() []*SlotInfo {
	slots := make([]*SlotInfo, 0, len(app.slots))
	for _, name := range app.slots {
		slots = append(slots, app.Snap.Slots[name])
	}
	return slots
}
