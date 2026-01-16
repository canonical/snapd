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

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

type dependency struct {
	Name        string
	MinimumBase int // 0 means "no minimum base"
	MaximumBase int // 0 means "no maximum base"
}

// This is the list of interfaces to add for each interface available
var dependencies = map[string][]dependency{
	"desktop-legacy": {{Name: "accessibility"}},
}

// This is the list of interfaces that can't be manually defined in the
// snapcraft.yaml file.
var forbiddenInterfaces = []string{
	"accessibility",
}

func GetDependenciesFor(plugs []string, slots []string, base string) ([]string, error) {
	baseVersion := 0
	if match, _ := regexp.MatchString("[cC]ore[0-9]+", base); match {
		baseVersion, _ = strconv.Atoi(base[4:])
	}
	dependeciesList := []string{}
	for _, plug := range plugs {
		// Check no forbidden dependency is in the app plugs list
		if slices.Contains(forbiddenInterfaces, plug) {
			return nil, fmt.Errorf("the interface %q is internal and can't be manually defined in the snapcraft.yaml file", plug)
		}
		deps, ok := dependencies[plug]
		if !ok {
			continue
		}
		for _, dep := range deps {
			if baseVersion != 0 {
				if (dep.MinimumBase != 0) && (dep.MinimumBase > baseVersion) {
					continue
				}
				if (dep.MaximumBase != 0) && (dep.MaximumBase < baseVersion) {
					continue
				}
			}
			if slices.Contains(dependeciesList, dep.Name) {
				// Don't add duplicated dependencies
				continue
			}
			if slices.Contains(plugs, dep.Name) {
				// Don't add a dependency plug if it is already a app-specific plug
				continue
			}
			if slices.Contains(slots, dep.Name) {
				// Don't add a dependency plug if it is already an app-specific slot
				continue
			}
			dependeciesList = append(dependeciesList, dep.Name)
		}
	}
	return dependeciesList, nil
}
