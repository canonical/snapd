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
	if c[i].PlugRef.Snap != c[j].PlugRef.Snap {
		return c[i].PlugRef.Snap < c[j].PlugRef.Snap
	}
	if c[i].PlugRef.Name != c[j].PlugRef.Name {
		return c[i].PlugRef.Name < c[j].PlugRef.Name
	}
	if c[i].SlotRef.Snap != c[j].SlotRef.Snap {
		return c[i].SlotRef.Snap < c[j].SlotRef.Snap
	}
	return c[i].SlotRef.Name < c[j].SlotRef.Name
}

type bySlotRef []SlotRef

func (c bySlotRef) Len() int      { return len(c) }
func (c bySlotRef) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotRef) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

type byPlugRef []PlugRef

func (c byPlugRef) Len() int      { return len(c) }
func (c byPlugRef) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlugRef) Less(i, j int) bool {
	if c[i].Snap != c[j].Snap {
		return c[i].Snap < c[j].Snap
	}
	return c[i].Name < c[j].Name
}

type byPlugSnapAndName []*snap.PlugInfo

func (c byPlugSnapAndName) Len() int      { return len(c) }
func (c byPlugSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlugSnapAndName) Less(i, j int) bool {
	if c[i].Snap.InstanceName() != c[j].Snap.InstanceName() {
		return c[i].Snap.InstanceName() < c[j].Snap.InstanceName()
	}
	return c[i].Name < c[j].Name
}

type bySlotSnapAndName []*snap.SlotInfo

func (c bySlotSnapAndName) Len() int      { return len(c) }
func (c bySlotSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotSnapAndName) Less(i, j int) bool {
	if c[i].Snap.InstanceName() != c[j].Snap.InstanceName() {
		return c[i].Snap.InstanceName() < c[j].Snap.InstanceName()
	}
	return c[i].Name < c[j].Name
}

type byBackendName []SecurityBackend

func (c byBackendName) Len() int      { return len(c) }
func (c byBackendName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byBackendName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
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

type byPlugInfo []*snap.PlugInfo

func (c byPlugInfo) Len() int      { return len(c) }
func (c byPlugInfo) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlugInfo) Less(i, j int) bool {
	if c[i].Snap.InstanceName() != c[j].Snap.InstanceName() {
		return c[i].Snap.InstanceName() < c[j].Snap.InstanceName()
	}
	return c[i].Name < c[j].Name
}

type bySlotInfo []*snap.SlotInfo

func (c bySlotInfo) Len() int      { return len(c) }
func (c bySlotInfo) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotInfo) Less(i, j int) bool {
	if c[i].Snap.InstanceName() != c[j].Snap.InstanceName() {
		return c[i].Snap.InstanceName() < c[j].Snap.InstanceName()
	}
	return c[i].Name < c[j].Name
}
