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
	"github.com/snapcore/snapd/interfaces/utils"
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
	var badPlugs []string
	var badSlots []string

	for plugName, plugInfo := range snapInfo.Plugs {
		iface, ok := allInterfaces[plugInfo.Interface]
		if !ok {
			snapInfo.BadInterfaces[plugName] = fmt.Sprintf("unknown interface %q", plugInfo.Interface)
			badPlugs = append(badPlugs, plugName)
			continue
		}
		// Reject plug with invalid name
		if err := utils.ValidateName(plugName); err != nil {
			snapInfo.BadInterfaces[plugName] = err.Error()
			badPlugs = append(badPlugs, plugName)
			continue
		}
		if err := interfaces.BeforePreparePlug(iface, plugInfo); err != nil {
			snapInfo.BadInterfaces[plugName] = err.Error()
			badPlugs = append(badPlugs, plugName)
			continue
		}
	}

	for slotName, slotInfo := range snapInfo.Slots {
		iface, ok := allInterfaces[slotInfo.Interface]
		if !ok {
			snapInfo.BadInterfaces[slotName] = fmt.Sprintf("unknown interface %q", slotInfo.Interface)
			badSlots = append(badSlots, slotName)
			continue
		}
		// Reject slot with invalid name
		if err := utils.ValidateName(slotName); err != nil {
			snapInfo.BadInterfaces[slotName] = err.Error()
			badSlots = append(badSlots, slotName)
			continue
		}
		if err := interfaces.BeforePrepareSlot(iface, slotInfo); err != nil {
			snapInfo.BadInterfaces[slotName] = err.Error()
			badSlots = append(badSlots, slotName)
			continue
		}
	}

	// remove any bad plugs and slots
	for _, plugName := range badPlugs {
		delete(snapInfo.Plugs, plugName)
		for _, app := range snapInfo.Apps {
			delete(app.Plugs, plugName)
		}
		for _, hook := range snapInfo.Hooks {
			delete(hook.Plugs, plugName)
		}
	}
	for _, slotName := range badSlots {
		delete(snapInfo.Slots, slotName)
		for _, app := range snapInfo.Apps {
			delete(app.Slots, slotName)
		}
		for _, hook := range snapInfo.Hooks {
			delete(hook.Slots, slotName)
		}
	}
}

func MockInterface(iface interfaces.Interface) func() {
	name := iface.Name()
	allInterfaces[name] = iface
	return func() {
		delete(allInterfaces, name)
	}
}

type byIfaceName []interfaces.Interface

func (c byIfaceName) Len() int      { return len(c) }
func (c byIfaceName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byIfaceName) Less(i, j int) bool {
	return c[i].Name() < c[j].Name()
}
