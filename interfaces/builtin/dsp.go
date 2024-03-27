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

# Ambarella kernel debug driver to allow user space setting the CV2x registers
/dev/ambad rw,

# another DSP device node
/dev/lens rw,

# various ambarella specific DSP control parameters
/proc/ambarella/iav r,
/proc/ambarella/ambnl/** rw,
/proc/ambarella/udc r,
/proc/ambarella/clock r,
/proc/ambarella/dsp_print rw,
/proc/ambarella/hdmi_edid r,
/proc/ambarella/cma r,
/proc/ambarella/ambarella_hwtimer rw,
/proc/ambarella/ambarella_hwtimer_outfreq rw,
/proc/ambarella/vapi_sync r,
/proc/ambarella/dsp_state r,

# to match vin0_idsp, vin1_idsp, vin2_idsp, etc.
/proc/ambarella/vin[0-9]_idsp r,

# to match e0021000.dma and e0020000.dma
/proc/ambarella/[0-9a-e][0-9a-e][0-9a-e][0-9a-e][0-9a-e][0-9a-e][0-9a-e][0-9a-e].dma rw,

# needed to control the usb device attached to the DSP
/proc/ambarella/usbphy0 rw,
`

var ambarellaDspConnectedPlugUDev = []string{
	`KERNEL=="iav"`,
	`KERNEL=="cavalry"`,
	`KERNEL=="ucode"`,
	`KERNEL=="lens"`,
	`KERNEL=="ambad"`,
}

type dspInterface struct {
	commonInterface
}

func (iface *dspInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor of the slot

	var flavor string
	_ = slot.Attr("flavor", &flavor)
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
