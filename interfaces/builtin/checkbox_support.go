// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/udev"
)

const checkboxSupportSummary = `allows checkbox to execute arbitrary system tests`

const checkboxSupportBaseDeclarationPlugs = `
  checkbox-support:
    allow-installation: false
    deny-auto-connection: true
`

const checkboxSupportBaseDeclarationSlots = `
  checkbox-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type checkboxSupportInterface struct {
	// The checkbox-support interface is exactly the steam-support interface
	// with the exception that it is also allowed to run on core devices.
	steamSupportInterface
}

func (iface *checkboxSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// we inherit one from steamSupportInterface, but none of the snippets are
	// useful as the interface can manage device cgroup, giving it unrestricted
	// access to devices
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&checkboxSupportInterface{steamSupportInterface{commonInterface{
		name:                 "checkbox-support",
		summary:              checkboxSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		controlsDeviceCgroup: true, // checkbox is exempt from device filtering
		baseDeclarationSlots: checkboxSupportBaseDeclarationSlots,
		baseDeclarationPlugs: checkboxSupportBaseDeclarationPlugs,
		connectedPlugSecComp: steamSupportConnectedPlugSecComp,
	}}})
}
