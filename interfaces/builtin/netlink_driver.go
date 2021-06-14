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
	"fmt"
	"regexp"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const netlinkDriverSummary = `allows operating a kernel driver module exposing itself via a netlink protocol family`

const netlinkDriverBaseDeclarationSlots = `
  netlink-driver:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

// netlinkDriverInterface type
type netlinkDriverInterface struct {
	commonInterface
}

const netlinkDriverConnectedPlugApparmor = `
# allow accessing the Linux kernel custom netlink protocol
# this allows all netlink protocol communication - further 
# confinement for particular families/protocols is 
# implemented via seccomp filtering
network netlink raw,
`

// regex for family-name must match:
// * at least 2 characters long
// * must start with letter
// * must not end in a hyphen
// * can contain numbers, letters and hyphens for all character positions except
//   as described above
var familyNameRegexp = regexp.MustCompile(`^[a-z]+[a-z0-9-]*[^\-]$`)

// BeforePrepareSlot checks the slot definition is valid
func (iface *netlinkDriverInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Must have a protocol number identified as family
	number, ok := slot.Attrs["family"]
	if !ok {
		return fmt.Errorf("netlink-driver slot must have a family number attribute")
	}

	// Valid values of number
	if _, ok := number.(int64); !ok {
		return fmt.Errorf("netlink-driver slot family number attribute must be an int")
	}

	// must also have a family-name, used for identifying plug <-> slot
	name, ok := slot.Attrs["family-name"]
	if !ok {
		return fmt.Errorf("netlink-driver slot must have a family-name attribute")
	}

	nameStr, ok := name.(string)
	if !ok {
		return fmt.Errorf("netlink-driver slot family-name attribute must be a string")
	}

	// ensure it matches the regex
	if !familyNameRegexp.MatchString(nameStr) {
		return fmt.Errorf("netlink-driver slot family-name %q is invalid", nameStr)
	}

	// Slot is good
	return nil
}

func (iface *netlinkDriverInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var familyNum int64
	if err := slot.Attr("family", &familyNum); err != nil {
		return err
	}

	var familyName string
	if err := slot.Attr("family-name", &familyName); err != nil {
		return err
	}

	spec.AddSnippet(fmt.Sprintf(`# Description: Can access the Linux kernel custom netlink protocol
# for family %s
socket AF_NETLINK - %d`, familyName, familyNum))
	return nil
}

func (iface *netlinkDriverInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	// ensure that the family name on the plug side matches the family name
	// on the slot side

	var slotFamily, plugFamily string
	if err := plug.Attr("family-name", &plugFamily); err != nil {
		return false
	}

	if err := slot.Attr("family-name", &slotFamily); err != nil {
		return false
	}

	return slotFamily == plugFamily
}

func init() {
	registerIface(&netlinkDriverInterface{
		commonInterface: commonInterface{
			name:                  "netlink-driver",
			summary:               netlinkDriverSummary,
			baseDeclarationSlots:  netlinkDriverBaseDeclarationSlots,
			connectedPlugAppArmor: netlinkDriverConnectedPlugApparmor,
		},
	})
}
