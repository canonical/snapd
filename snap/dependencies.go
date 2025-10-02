// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

import "slices"

// This is the list of interfaces to add for each interface available
var dependencies = map[string][]string{
	"desktop-legacy": {"accessibility"},
}

// This is the list of interfaces that can't be manually defined in the
// snapcraft.yaml file.
var forbidenInterfaces = []string{
	"accessibility",
}

func GetGlobalPlugDependencies(plugs map[string]any, slots map[string]any) map[string]any {
	dependeciesList := map[string]any{}
	for plug := range plugs {
		deps, ok := dependencies[plug]
		if !ok {
			continue
		}
		for _, dep := range deps {
			if _, ok := dependeciesList[dep]; ok {
				// Don't add duplicated dependencies
				continue
			}
			if _, ok := plugs[dep]; ok {
				// Don't add a plug dependency that is already in the global plugs
				continue
			}
			if _, ok := slots[dep]; ok {
				// Don't add a plug dependency that is already in the global slots
				continue
			}
			dependeciesList[dep] = nil
		}
	}
	return dependeciesList
}

func GetDependenciesFor(plugs []string, slots []string, globalPlugs map[string]*PlugInfo, globalSlots map[string]*SlotInfo) []string {
	dependeciesList := []string{}
	for _, plug := range plugs {
		deps, ok := dependencies[plug]
		if !ok {
			continue
		}
		for _, dep := range deps {
			if slices.Contains(dependeciesList, dep) {
				// Don't add duplicated dependencies
				continue
			}
			if slices.Contains(plugs, dep) {
				// Don't add a dependency plug if it is already a defined plug
				continue
			}
			if slices.Contains(slots, dep) {
				// Don't add a dependency plug if it is already a slot
				continue
			}
			if _, ok := globalSlots[dep]; ok {
				// Don't add a plug dependency that is already in the global slots
				continue
			}
			dependeciesList = append(dependeciesList, dep)
		}
	}
	return dependeciesList
}

func CheckInterfaceIsInvalid(iface string) bool {
	return slices.Contains(forbidenInterfaces, iface)
}
