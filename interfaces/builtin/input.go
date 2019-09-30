// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

const inputSummary = `allows input from keyboard/mouse devices`

const inputBaseDeclarationSlots = `
  input:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const inputPermanentSlotAppArmor = `
# Description: Allow input from keyboard/mouse devices.
`

const inputPermanentSlotSecComp = `
# Description: Allow input from keyboard/mouse devices.
`

const inputConnectedSlotAppArmor = `
# Description: Allow input from keyboard/mouse devices.
`

const inputConnectedPlugAppArmor = `
# Description: Allow input from keyboard/mouse devices.
# raw rule is not finely mediated by apparmor so we mediate with seccomp arg
# filtering.
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:* r,
`

const inputConnectedPlugSecComp = `
# Description: Allow input from keyboard/mouse devices.
# Needed to detect mouse and keyboard
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
bind
`

type inputInterface struct {
	commonInterface
}

func (iface *inputInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.TriggerSubsystem("input")
	spec.TagDevice(`KERNEL=="tty[0-9]*"`)
	spec.TagDevice(`KERNEL=="mice"`)
	spec.TagDevice(`KERNEL=="mouse[0-9]*"`)
	spec.TagDevice(`KERNEL=="event[0-9]*"`)
	spec.TagDevice(`KERNEL=="ts[0-9]*"`)
	return nil
}

func init() {
	registerIface(&inputInterface{commonInterface{
		name:                  "input",
		summary:               inputSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  inputBaseDeclarationSlots,
		connectedPlugAppArmor: inputConnectedPlugAppArmor,
		connectedPlugSecComp:  inputConnectedPlugSecComp,
		reservedForOS:         true,
	}})
}
