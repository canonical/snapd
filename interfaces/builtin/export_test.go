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

func MprisGetName(iface interfaces.Interface, attribs map[string]interface{}) (string, error) {
	return iface.(*mprisInterface).getName(attribs)
}

var ResolveSpecialVariable = resolveSpecialVariable

// MustInterface returns the interface with the given name or panicks.
func MustInterface(name string) interfaces.Interface {
	for _, iface := range allInterfaces {
		if iface.Name() == name {
			return iface
		}
	}
	panic(fmt.Errorf("cannot find interface with name %q", name))

}
