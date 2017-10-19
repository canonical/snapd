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
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

func init() {
	snap.SanitizePlugsSlots = SanitizePlugsSlots
}

var (
	allInterfaces map[string]interfaces.Interface
)

// Interfaces returns all of the built-in interfaces.
func Interfaces() []interfaces.Interface {
	ifaces := make([]interfaces.Interface, 0, len(allInterfaces))
	for _, iface := range allInterfaces {
		ifaces = append(ifaces, iface)
	}
	sort.Sort(byIfaceName(ifaces))
	return ifaces
}

// registerIface appends the given interface into the list of all known interfaces.
func registerIface(iface interfaces.Interface) {
	if allInterfaces[iface.Name()] != nil {
		panic(fmt.Errorf("cannot register duplicate interface %q", iface.Name()))
	}
	if allInterfaces == nil {
		allInterfaces = make(map[string]interfaces.Interface)
	}
	allInterfaces[iface.Name()] = iface
}

func SanitizePlugsSlots(snapInfo *snap.Info) {
	for plugName, plugInfo := range snapInfo.Plugs {
		iface, ok := allInterfaces[plugInfo.Interface]
		if !ok {
			snapInfo.BadInterfaces[plugName] = fmt.Sprintf("unknown interface %q", plugInfo.Interface)
			continue
		}
		// Reject plug with invalid name
		if err := interfaces.ValidateName(plugName); err != nil {
			snapInfo.BadInterfaces[plugName] = err.Error()
			continue
		}
		plug := &interfaces.Plug{PlugInfo: plugInfo}
		if err := plug.Sanitize(iface); err != nil {
			snapInfo.BadInterfaces[plugName] = err.Error()
			continue
		}
	}

	for slotName, slotInfo := range snapInfo.Slots {
		iface, ok := allInterfaces[slotInfo.Interface]
		if !ok {
			snapInfo.BadInterfaces[slotName] = fmt.Sprintf("unknown interface %q", slotInfo.Interface)
			continue
		}
		// Reject slot with invalid name
		if err := interfaces.ValidateName(slotName); err != nil {
			snapInfo.BadInterfaces[slotName] = err.Error()
			continue
		}
		slot := &interfaces.Slot{SlotInfo: slotInfo}
		if err := slot.Sanitize(iface); err != nil {
			snapInfo.BadInterfaces[slotName] = err.Error()
			continue
		}
	}
}

type byIfaceName []interfaces.Interface

func (c byIfaceName) Len() int      { return len(c) }
func (c byIfaceName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byIfaceName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}
