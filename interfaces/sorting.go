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

type byPlugSnapAndName []*Plug

func (c byPlugSnapAndName) Len() int      { return len(c) }
func (c byPlugSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlugSnapAndName) Less(i, j int) bool {
	if c[i].Snap.Name() != c[j].Snap.Name() {
		return c[i].Snap.Name() < c[j].Snap.Name()
	}
	return c[i].Name < c[j].Name
}

type bySlotSnapAndName []*Slot

func (c bySlotSnapAndName) Len() int      { return len(c) }
func (c bySlotSnapAndName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c bySlotSnapAndName) Less(i, j int) bool {
	if c[i].Snap.Name() != c[j].Snap.Name() {
		return c[i].Snap.Name() < c[j].Snap.Name()
	}
	return c[i].Name < c[j].Name
}

type byBackendName []SecurityBackend

func (c byBackendName) Len() int      { return len(c) }
func (c byBackendName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byBackendName) Less(i, j int) bool {
	if c[i].Name() != c[j].Name() {
		return c[i].Name() < c[j].Name()
	}
	return c[i].Name() < c[j].Name()
}
