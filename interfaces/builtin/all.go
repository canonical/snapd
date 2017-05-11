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

package builtin

import (
	"sort"

	"github.com/snapcore/snapd/interfaces"
)

var (
	allInterfaces []interfaces.Interface
	sorted        bool
)

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	if !sorted {
		sort.Sort(byIfaceName(allInterfaces))
		sorted = true
	}
	return allInterfaces
}

// registerIface appends the given interface into the list of all known interfaces.
func registerIface(iface interfaces.Interface) {
	allInterfaces = append(allInterfaces, iface)
	sorted = false
}

type byIfaceName []interfaces.Interface

func (c byIfaceName) Len() int      { return len(c) }
func (c byIfaceName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byIfaceName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}
