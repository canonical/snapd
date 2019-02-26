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

package interfaces

import (
	"sort"

	"github.com/snapcore/snapd/snap"
)

type byConnRef []*ConnRef

func (c byConnRef) Len() int      { return len(c) }
func (c byConnRef) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byConnRef) Less(i, j int) bool {
	return c[i].SortsBefore(c[j])
}

type byPlugSnapAndName []*snap.PlugInfo

func (c byPlugSnapAndName) Len() int      { return len(c) }
func (c byPlugSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlugSnapAndName) Less(i, j int) bool {
	if c[i].Snap.SnapName() != c[j].Snap.SnapName() {
		return c[i].Snap.SnapName() < c[j].Snap.SnapName()
	}
	if c[i].Snap.InstanceKey != c[j].Snap.InstanceKey {
		return c[i].Snap.InstanceKey < c[j].Snap.InstanceKey
	}
	return c[i].Name < c[j].Name
}

type bySlotSnapAndName []*snap.SlotInfo

func (c bySlotSnapAndName) Len() int      { return len(c) }
func (c bySlotSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotSnapAndName) Less(i, j int) bool {
	if c[i].Snap.SnapName() != c[j].Snap.SnapName() {
		return c[i].Snap.SnapName() < c[j].Snap.SnapName()
	}
	if c[i].Snap.InstanceKey != c[j].Snap.InstanceKey {
		return c[i].Snap.InstanceKey < c[j].Snap.InstanceKey
	}
	return c[i].Name < c[j].Name
}

func sortedSnapNamesWithPlugs(m map[string]map[string]*snap.PlugInfo) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedPlugNames(m map[string]*snap.PlugInfo) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSnapNamesWithSlots(m map[string]map[string]*snap.SlotInfo) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedSlotNames(m map[string]*snap.SlotInfo) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type byInterfaceName []Interface

func (c byInterfaceName) Len() int      { return len(c) }
func (c byInterfaceName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byInterfaceName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}
