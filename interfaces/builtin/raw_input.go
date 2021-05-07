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
	"github.com/snapcore/snapd/interfaces/udev"
)

const rawInputSummary = `allows access to raw input devices`

const rawInputBaseDeclarationSlots = `
  raw-input:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const rawInputConnectedPlugSecComp = `
# Description: Allow handling input devices.
# for udev
bind
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const rawInputConnectedPlugAppArmor = `
# Description: Allow reading and writing to raw input devices

/dev/input/* rw,

# Allow reading for supported event reports for all input devices. See
# https://www.kernel.org/doc/Documentation/input/event-codes.txt
/sys/devices/**/input[0-9]*/capabilities/* r,

# For using udev
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:input[0-9]* r,
`

var rawInputConnectedPlugUDev = []string{
	`KERNEL=="event[0-9]*", SUBSYSTEM=="input"`,
	`KERNEL=="mice"`,
	`KERNEL=="mouse[0-9]*"`,
	`KERNEL=="ts[0-9]*"`,
}

type rawInputInterface struct {
	commonInterface
}

func (iface *rawInputInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.TriggerSubsystem("input")
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&rawInputInterface{commonInterface{
		name:                  "raw-input",
		summary:               rawInputSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  rawInputBaseDeclarationSlots,
		connectedPlugSecComp:  rawInputConnectedPlugSecComp,
		connectedPlugAppArmor: rawInputConnectedPlugAppArmor,
		connectedPlugUDev:     rawInputConnectedPlugUDev,
	}})
}
