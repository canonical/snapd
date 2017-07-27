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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

var (
	RegisterIface                     = registerIface
	ResolveSpecialVariable            = resolveSpecialVariable
	SanitizeSlotReservedForOS         = sanitizeSlotReservedForOS
	SanitizeSlotReservedForOSOrGadget = sanitizeSlotReservedForOSOrGadget
	SanitizeSlotReservedForOSOrApp    = sanitizeSlotReservedForOSOrApp
)

func MprisGetName(iface interfaces.Interface, attribs map[string]interface{}) (string, error) {
	return iface.(*mprisInterface).getName(attribs)
}

// MockInterfaces replaces the set of known interfaces and returns a restore function.
func MockInterfaces(ifaces map[string]interfaces.Interface) (restore func()) {
	old := allInterfaces
	allInterfaces = ifaces
	return func() { allInterfaces = old }
}

// Interface returns the interface with the given name (or nil).
func Interface(name string) interfaces.Interface {
	return allInterfaces[name]
}

// MustInterface returns the interface with the given name or panics.
func MustInterface(name string) interfaces.Interface {
	if iface, ok := allInterfaces[name]; ok {
		return iface
	}
	panic(fmt.Errorf("cannot find interface with name %q", name))
}
