// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const opticalDriveSummary = `allows access to optical drives`

const opticalDriveBaseDeclarationSlots = `
  optical-drive:
    allow-installation:
      slot-snap-type:
        - core
    deny-connection:
      plug-attributes:
        write: true
    deny-auto-connection:
      plug-attributes:
        write: true
`

const opticalDriveConnectedPlugAppArmor = `
# Allow read access to optical drives
/dev/sr[0-9]* r,
/dev/scd[0-9]* r,
@{PROC}/sys/dev/cdrom/info r,
/run/udev/data/b11:[0-9]* r,
`

var opticalDriveConnectedPlugUDev = []string{
	`KERNEL=="sr[0-9]*"`,
	`KERNEL=="scd[0-9]*"`,
}

// opticalDriveInterface is the type for optical drive interfaces.
type opticalDriveInterface struct {
	commonInterface
}

// BeforePrepareSlot checks and possibly modifies a slot.
// Valid "optical-drive" slots may contain the attribute "write".
// If defined, the attribute "write" must be either "true" or "false".
func (iface *opticalDriveInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if 'write' isn't specified, but if it is, it needs to be bool
	if w, ok := plug.Attrs["write"]; ok {
		_, ok = w.(bool)
		if !ok {
			return fmt.Errorf(`optical-drive "write" attribute must be a boolean`)
		}
	}

	return nil
}

func (iface *opticalDriveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var write bool
	_ = plug.Attr("write", &write)

	// Add the common readonly policy
	spec.AddSnippet(opticalDriveConnectedPlugAppArmor)

	// 'write: true' grants write access to the devices
	if write {
		spec.AddSnippet("# Allow write access to optical drives")
		spec.AddSnippet("/dev/sr[0-9]* w,")
		spec.AddSnippet("/dev/scd[0-9]* w,")
	}
	return nil
}

func init() {
	registerIface(&opticalDriveInterface{commonInterface: commonInterface{
		name:                 "optical-drive",
		summary:              opticalDriveSummary,
		implicitOnCore:       false,
		implicitOnClassic:    true,
		baseDeclarationSlots: opticalDriveBaseDeclarationSlots,
		connectedPlugUDev:    opticalDriveConnectedPlugUDev,
		reservedForOS:        true,
	}})
}
