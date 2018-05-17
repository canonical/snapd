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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const opticalDriveSummary = `allows read and write access to optical drives`

const opticalDriveBaseDeclarationSlots = `
  optical-drive:
    allow-installation:
      slot-snap-type:
        - core
`

// opticalDriveInterface is the type for optical drive interfaces.
type opticalDriveInterface struct {
	commonInterface
}

// BeforePrepareSlot checks and possibly modifies a slot.
// Valid "optical-drive" slots may contain the attribute "write".
// If defined, the attribute "write" must be either "true" or "false".
func (iface *opticalDriveInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	write, ok := plug.Attrs["write"].(bool)
	if !ok {
		if plug.Attrs == nil {
			plug.Attrs = make(map[string]interface{})
		}
		write = false
		plug.Attrs["write"] = false
	}

	if write != true && write != false {
		return fmt.Errorf("optical-drive write attribute must be either undefined, true, or false.")
	}

	return nil
}

func (iface *opticalDriveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet("/run/udev/data/b11:[0-9]* r,")
	spec.AddSnippet("@{PROC}/sys/dev/cdrom/info r,")

	var write bool
	if err := plug.Attr("write", &write); err == nil && write == true {
		// Allow read and write access to optical drive block devices
		spec.AddSnippet("/dev/sr[0-9]* rw,")
		spec.AddSnippet("/dev/scd[0-9]* rw,")
		return nil
	}

	// Allow readonly access to optical drive block devices
	spec.AddSnippet("/dev/sr[0-9]* r,")
	spec.AddSnippet("/dev/scd[0-9]* r,")
	return nil
}

func (iface *opticalDriveInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.TagDevice(`KERNEL=="sr[0-9]*"`)
	spec.TagDevice(`KERNEL=="scd[0-9]*"`)
	return nil
}

func (iface *opticalDriveInterface) AutoConnect(plug *interfaces.Plug, _ *interfaces.Slot) bool {
	var write bool
	// Prevent auto connection when write is requested
	if err := plug.Attr("write", &write); err == nil && write == true {
		return false
	}

	// Allow auto connection when write is not requested
	return true
}

func init() {
	registerIface(&opticalDriveInterface{commonInterface: commonInterface{
		name:                 "optical-drive",
		summary:              opticalDriveSummary,
		implicitOnCore:       false,
		implicitOnClassic:    true,
		baseDeclarationSlots: opticalDriveBaseDeclarationSlots,
		reservedForOS:        true,
	}})
}
