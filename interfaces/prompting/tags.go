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

package prompting

import (
	"fmt"
	"math"
	"sort"

	"github.com/snapcore/snapd/interfaces/apparmor"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

var (
	apparmorInterfaceForMetadataTag = apparmor.InterfaceForMetadataTag
)

// InterfaceFromTagsets returns the most specific interface which applies to
// all of the given tagsets.
//
// Potential interfaces are identified by looking up whether a given tag is
// registered in association with a particular snapd interface.
//
// If none of the given tags are registered with an interface, then returns
// ErrNoInterfaceTags.
//
// If there are tags registered with an interface, but no interface applies to
// every tagset, then returns an error.
func InterfaceFromTagsets(tagsets notify.TagsetMap) (iface string, err error) {
	if len(tagsets) == 0 {
		return "", prompting_errors.ErrNoInterfaceTags
	}

	// First convert tag lists to interface lists (discard irrelevant tags)
	sawAnyInterfaceTags := false
	permInterfaceMap := make(map[notify.AppArmorPermission][]string)
	for perm, tagset := range tagsets {
		ifaces := make([]string, 0, len(tagset))
		for _, tag := range tagset {
			iface, ok := apparmorInterfaceForMetadataTag(tag)
			if ok {
				sawAnyInterfaceTags = true
				ifaces = append(ifaces, iface)
			}
		}
		permInterfaceMap[perm] = ifaces
	}

	if !sawAnyInterfaceTags {
		return "", prompting_errors.ErrNoInterfaceTags
	}

	// Now go through each interface list, prune any interfaces which do not
	// occur in every list, and keep track of how far each interface is from
	// the end of each interface list (we prioritize those at the end).
	ifaceDistances := make(map[string]int)
	// Pre-populate with the interfaces from an arbitrary tagset
	for _, ifaces := range permInterfaceMap {
		for _, iface := range ifaces {
			ifaceDistances[iface] = 0
		}
	}
	// Now prune and tally distances from the end of the lists
	for perm, ifaces := range permInterfaceMap {
		if len(ifaces) == 0 {
			// Some tags were registered with interfaces, but none apply to
			// this permission.
			return "", fmt.Errorf("cannot find interface which applies to permission: %v", perm)
		}

		for i, iface := range ifaces {
			if _, ok := ifaceDistances[iface]; !ok {
				// This interface doesn't apply to a previous permission.
				continue
			}
			// Add the distance from the end of the list, since interfaces at
			// the end are the most "specific" and thus highest precedence.
			ifaceDistances[iface] += len(ifaces) - i - 1
		}

		// Prune any permissions from the map which are not in the current list
		toPrune := make(map[string]bool)
		for iface := range ifaceDistances {
			if !strutil.ListContains(ifaces, iface) {
				toPrune[iface] = true
			}
		}
		for iface := range toPrune {
			delete(ifaceDistances, iface)
		}
	}

	// Check that at least one interface remains
	if len(ifaceDistances) == 0 {
		return "", fmt.Errorf("cannot find interface which applies to all permissions")
	}

	// Determine the interface which on average occurred closest to the end of
	// each list of interfaces associated with tags.
	minDistance := math.MaxInt
	minInterfaces := make([]string, 0, 1) // hopefully there's only one interface
	for iface, distance := range ifaceDistances {
		if distance > minDistance {
			continue
		}
		if distance < minDistance {
			minDistance = distance
			minInterfaces = minInterfaces[:0]
		}
		minInterfaces = append(minInterfaces, iface)
	}

	if len(minInterfaces) == 1 {
		return minInterfaces[0], nil
	}

	// Somehow, more than one interface tied for their average distance from
	// the end of the lists of interfaces for each permission. As an arbitrary
	// tie breaker, choose the interface which comes first lexicographically.
	sort.Strings(minInterfaces)
	return minInterfaces[0], nil
}
