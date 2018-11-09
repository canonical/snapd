// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

const deviceButtonsSummary = `allows access to device buttons as input events`

const deviceButtonsBaseDeclarationSlots = `
  device-buttons:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const deviceButtonsConnectedPlugAppArmor = `
# Description: Allow reading and writing events to device buttons exposed as
#              input events.

#
# evdev-based interface
#

# /dev/input/event* is unfortunately not namespaced and includes all input
# devices, including keyboards and mice, which allows input sniffing and
# injection. Until we have inode tagging of devices, we use a glob rule here
# and rely on udev tagging to only add evdev devices to the snap's device
# cgroup that are marked with ENV{ID_INPUT_KEY}=="1" and are not marked with
# ENV{ID_INPUT_KEYBOARD}. As such, even though AppArmor allows all evdev,
# the device cgroup does not.
/dev/input/event[0-9]* rw,

# Allow reading for supported event reports for all input devices. See
# https://www.kernel.org/doc/Documentation/input/event-codes.txt
# FIXME: this is a very minor information leak and snapd should instead query
# udev for the specific accesses associated with the above devices.
/sys/devices/**/input[0-9]*/capabilities/* r,
`

// Add the device buttons realized in terms of GPIO. They come up with
// ENV{ID_INPUT_KEY} set to "1" value and at the same time make sure these are
// not a keyboard.
//
// Because of the unconditional /dev/input/event[0-9]* AppArmor rule, we need
// to ensure that the device cgroup is in effect even when there are no
// gpio keys present so that we don't give away all input to the snap.
var deviceButtonsConnectedPlugUDev = []string{
	`KERNEL=="event[0-9]*", SUBSYSTEM=="input", ENV{ID_INPUT_KEY}=="1", ENV{ID_INPUT_KEYBOARD}!="1"`,
	`KERNEL=="full", SUBSYSTEM=="mem"`,
}

type deviceButtonsInterface struct {
	commonInterface
}

func (iface *deviceButtonsInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.TriggerSubsystem("key")
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&deviceButtonsInterface{commonInterface{
		name:                  "device-buttons",
		summary:               deviceButtonsSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  deviceButtonsBaseDeclarationSlots,
		connectedPlugAppArmor: deviceButtonsConnectedPlugAppArmor,
		connectedPlugUDev:     deviceButtonsConnectedPlugUDev,
		reservedForOS:         true,
	}})
}
