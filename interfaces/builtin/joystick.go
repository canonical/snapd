// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

const joystickSummary = `allows access to joystick devices`

const joystickBaseDeclarationSlots = `
  joystick:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const joystickConnectedPlugAppArmor = `
# Description: Allow reading and writing to joystick devices

#
# Old joystick interface
#

# Per https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
# only js0-js31 is valid so limit the /dev and udev entries to those devices.
/dev/input/js{[0-9],[12][0-9],3[01]} rw,
/run/udev/data/c13:{[0-9],[12][0-9],3[01]} r,

#
# New evdev-joystick interface
#

# Per https://github.com/torvalds/linux/blob/master/Documentation/admin-guide/devices.txt
# the minor is 65 and up so limit udev to that.
/run/udev/data/c13:{6[5-9],[7-9][0-9],[1-9][0-9][0-9]*} r,

# /dev/input/event* is unfortunately not namespaced and includes all input
# devices, including keyboards and mice, which allows input sniffing and
# injection. Until we have inode tagging of devices, we use a glob rule here
# and rely on udev tagging to only add evdev devices to the snap's device
# cgroup that are marked with ENV{ID_INPUT_JOYSTICK}=="1". As such, even though
# AppArmor allows all evdev, the device cgroup does not.
/dev/input/event[0-9]* rw,

# Allow reading for supported event reports for all input devices. See
# https://www.kernel.org/doc/Documentation/input/event-codes.txt
# FIXME: this is a very minor information leak and snapd should instead query
# udev for the specific accesses associated with the above devices.
/sys/devices/**/input[0-9]*/capabilities/* r,
`

// Add the old joystick device (js*) and any evdev input interfaces which are
// marked as joysticks. Note, some input devices are known to come up as
// joysticks when they are not and while this rule would tag them, on systems
// where this is happening the device is non-functional for its intended
// purpose. In other words, in practice, users with such devices will have
// updated their udev rules to set ENV{ID_INPUT_JOYSTICK}="" to make it work,
// which means this rule will no longer match.
//
// Because of the unconditional /dev/input/event[0-9]* AppArmor rule, we need
// to ensure that the device cgroup is in effect even when there are no
// joysticks present so that we don't give away all input to the snap. Use
// /dev/full for this purpose.
var joystickConnectedPlugUDev = []string{
	`KERNEL=="js[0-9]*"`,
	`KERNEL=="event[0-9]*", SUBSYSTEM=="input", ENV{ID_INPUT_JOYSTICK}=="1"`,
	`KERNEL=="full", SUBSYSTEM=="mem"`,
}

type joystickInterface struct {
	commonInterface
}

func (iface *joystickInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.TriggerSubsystem("input/joystick")
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&joystickInterface{commonInterface{
		name:                  "joystick",
		summary:               joystickSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  joystickBaseDeclarationSlots,
		connectedPlugAppArmor: joystickConnectedPlugAppArmor,
		connectedPlugUDev:     joystickConnectedPlugUDev,
		reservedForOS:         true,
	}})
}
