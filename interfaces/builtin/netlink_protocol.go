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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const netlinkProtocolSummary = `allows communication through the kernel custom netlink protocol`

const netlinkProtocolBaseDeclarationSlots = `
  netlink-protocol:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

// netlinkProtocolInterface type
type netlinkProtocolInterface struct {
	commonInterface
}

// BeforePrepareSlot checks the slot definition is valid
func (iface *netlinkProtocolInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Must have a protocol number
	number, ok := slot.Attrs["protocol"]
	if !ok {
		return fmt.Errorf("netlink-protocol slot must have a protocol number attribute")
	}

	// Valid values of number
	if _, ok := number.(int64); !ok {
		return fmt.Errorf("netlink-protocol slot protocol number attribute must be an int")
	}

	// Slot is good
	return nil
}

func (iface *netlinkProtocolInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(`network netlink raw,`)
	return nil
}

func (iface *netlinkProtocolInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var protocol int64
	if err := slot.Attr("protocol", &protocol); err != nil {
		return err
	}
	spec.AddSnippet(fmt.Sprintf(`# Description: Can access the Linux kernel custom netlink protocol
socket AF_NETLINK - %d`, protocol))
	return nil
}

func (iface *netlinkProtocolInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&netlinkProtocolInterface{
		commonInterface: commonInterface{
			name:                 "netlink-protocol",
			summary:              netlinkProtocolSummary,
			baseDeclarationSlots: netlinkProtocolBaseDeclarationSlots,
		},
	})
}
