// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type ByConnRef byConnRef

func (c ByConnRef) Len() int           { return byConnRef(c).Len() }
func (c ByConnRef) Swap(i, j int)      { byConnRef(c).Swap(i, j) }
func (c ByConnRef) Less(i, j int) bool { return byConnRef(c).Less(i, j) }

type ByPlugSnapAndName byPlugSnapAndName

func (c ByPlugSnapAndName) Len() int           { return byPlugSnapAndName(c).Len() }
func (c ByPlugSnapAndName) Swap(i, j int)      { byPlugSnapAndName(c).Swap(i, j) }
func (c ByPlugSnapAndName) Less(i, j int) bool { return byPlugSnapAndName(c).Less(i, j) }

type BySlotSnapAndName bySlotSnapAndName

func (c BySlotSnapAndName) Len() int           { return bySlotSnapAndName(c).Len() }
func (c BySlotSnapAndName) Swap(i, j int)      { bySlotSnapAndName(c).Swap(i, j) }
func (c BySlotSnapAndName) Less(i, j int) bool { return bySlotSnapAndName(c).Less(i, j) }

type ByInterfaceName byInterfaceName

func (c ByInterfaceName) Len() int           { return byInterfaceName(c).Len() }
func (c ByInterfaceName) Swap(i, j int)      { byInterfaceName(c).Swap(i, j) }
func (c ByInterfaceName) Less(i, j int) bool { return byInterfaceName(c).Less(i, j) }

// MockIsHomeUsingNFS mocks the real implementation of osutil.IsHomeUsingNFS
func MockIsHomeUsingNFS(new func() (bool, error)) (restore func()) {
	old := isHomeUsingNFS
	isHomeUsingNFS = new
	return func() {
		isHomeUsingNFS = old
	}
}

// MockIsRootWritableOverlay mocks the real implementation of
// osutil.IsRootWritableOverlay
func MockIsRootWritableOverlay(new func() (string, error)) (restore func()) {
	old := isRootWritableOverlay
	isRootWritableOverlay = new
	return func() {
		isRootWritableOverlay = old
	}
}

func MockReadBuildID(mock func(p string) (string, error)) (restore func()) {
	old := readBuildID
	readBuildID = mock
	return func() {
		readBuildID = old
	}
}

type SystemKey = systemKey

var SystemKeyVersion = systemKeyVersion
