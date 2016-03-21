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

// AppNames returns a list of applications names.
func (plug *PlugInfo) AppNames() []string {
	var names []string
	for name := range plug.Apps {
		names = append(names, name)
	}
	return names
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

// AppNames returns a list of applications names.
func (slot *SlotInfo) AppNames() []string {
	var names []string
	for name := range slot.Apps {
		names = append(names, name)
	}
	return names
}

// AppInfo provides information about a plug.
type AppInfo struct {
	Snap *Info

	Name    string
	Command string
	Plugs   map[string]*PlugInfo
	Slots   map[string]*SlotInfo
}
