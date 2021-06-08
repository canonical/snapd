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
	"github.com/snapcore/snapd/interfaces/udev"
)

const dspSummary = `allows controlling digital signal processors on certain boards`

const dspBaseDeclarationSlots = `
  dsp:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
`

const ambarellaDspConnectedPlugApparmor = `
# Description: can manage and control the integrated digital signal processor on
# the ambarella device. This allows privileged access to hardware and kernel 
# drivers related to the digital signal processor and thus is only allowed on
# specific devices providing the slot via a gadget and is also not auto-
# connected.

# The ucode device node corresponds to the firmware on the digital signal 
# processor
/dev/ucode rw,

# The iav device node is the device node exposed for the specific IAV linux
# device driver used with the CV2x / S6Lm cores on an ambarella device
/dev/iav rw,

# The cavalry device node is used for managing the CV2x vector processor (VP).
/dev/cavalry rw,

# another DSP device node
/dev/lens rw,

# also needed for interfacing with the DSP
/proc/ambarella/vin0_idsp rw,
`

var ambarellaDspConnectedPlugUDev = []string{
	`KERNEL=="iav"`,
	`KERNEL=="cavalry`,
	`KERNEL=="ucode"`,
	`KERNEL=="lens"`,
}

type dspInterface struct {
	commonInterface
}

func (iface *dspInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor of the slot

	var flavor string
	_ = slot.Attr("flavor", &flavor)
	fmt.Println("slot flavor", flavor)
	switch flavor {
	// only supported flavor for now
	case "ambarella":
		spec.AddSnippet(ambarellaDspConnectedPlugApparmor)
	}

	return nil
}

func (iface *dspInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor of the slot
	var flavor string
	_ = slot.Attr("flavor", &flavor)
	switch flavor {
	// only supported flavor for now
	case "ambarella":
		for _, rule := range ambarellaDspConnectedPlugUDev {
			spec.TagDevice(rule)
		}
	}

	return nil
}

func init() {
	registerIface(&dspInterface{commonInterface{
		name:                 "dsp",
		summary:              dspSummary,
		baseDeclarationSlots: dspBaseDeclarationSlots,
	}})
}
