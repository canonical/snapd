// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const sdControlSummary = `allows controlling SD cards on certain boards`

const sdControlBaseDeclarationSlots = `
  sd-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const sdControlBaseDeclarationPlugs = `
  sd-control:
    allow-installation: false
    deny-auto-connection: true
`

const dualSDSDControlConnectedPlugApparmor = `
# Description: can manage and control the SD cards using the DualSD driver.

# The main DualSD device node is used to control certain aspects of SD cards on
# the system.
/dev/DualSD rw,
`

var dualSDSDControlConnectedPlugUDev = []string{
	`KERNEL=="DualSD"`,
}

type sdControlInterface struct {
	commonInterface
}

func (iface *sdControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor of the plug

	var flavor string
	_ = plug.Attr("flavor", &flavor)
	switch flavor {
	// only supported flavor for now
	case "dual-sd":
		spec.AddSnippet(dualSDSDControlConnectedPlugApparmor)
	}

	return nil
}

func (iface *sdControlInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor of the plug
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	switch flavor {
	// only supported flavor for now
	case "dual-sd":
		for _, rule := range dualSDSDControlConnectedPlugUDev {
			spec.TagDevice(rule)
		}
	}

	return nil
}

func init() {
	registerIface(&sdControlInterface{commonInterface{
		name:                 "sd-control",
		summary:              sdControlSummary,
		baseDeclarationSlots: sdControlBaseDeclarationSlots,
		baseDeclarationPlugs: sdControlBaseDeclarationPlugs,
		implicitOnCore:       true,
		implicitOnClassic:    true,
	}})
}
