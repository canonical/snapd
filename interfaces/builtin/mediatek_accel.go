// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

const mediatekAccelSummary = `allows access to the hardware accelerators on MediaTek Genio devices`

const mediatekAccelBaseDeclarationSlots = `
  mediatek-accel:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type mediatekAccelRule struct {
	AppArmor string
	UDev     []string
}

const mediatekAccelConnectedPlugAppArmorHeader = `
# Description: Provide permissions for accessing the hardware accelerators on MediaTek Genio devices

`
const mediatekAccelAPUConnectedPlugAppArmorSnippet = `
# Added due to "apu" unit in mediatek-accel plug.
# APU (AI Processing Unit)
/dev/apusys rw,
`

const mediatekAccelVCUConnectedPlugAppArmorSnippet = `
# Added due to "vcu" unit in mediatek-accel plug.
# VPU (MediaTek Video Processor Unit)
/dev/vcu rw,
# MDP (MediaTek Media Data Path) and other vcu in the future
/dev/vcu[0-9]* rw,
`

var mediatekAccelUnitRules = map[string]mediatekAccelRule{
	"apu": {
		AppArmor: mediatekAccelAPUConnectedPlugAppArmorSnippet,
		UDev: []string{
			`SUBSYSTEM=="misc", KERNEL=="apusys"`,
		},
	},
	"vcu": {
		AppArmor: mediatekAccelVCUConnectedPlugAppArmorSnippet,
		UDev: []string{
			/* For VPU (MediaTek Video Processor Unit) */
			`SUBSYSTEM=="vcu", KERNEL=="vcu"`,

			/* For MDP (MediaTek Media Data Path) */
			`SUBSYSTEM=="vcu[0-9]*", KERNEL=="vcu[0-9]*"`,
		},
	},
}

type mediatekAccelInterface struct {
	commonInterface
}

func (iface *mediatekAccelInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	attrVal, exists := plug.Lookup("units")
	if !exists {
		return fmt.Errorf(`mediatek-accel interface requires "units" attribute to be set`)
	}

	// Validate that "units" is a list of strings
	units, ok := attrVal.([]any)
	if !ok {
		return fmt.Errorf(`mediatek-accel "units" attribute must be a list of strings`)
	}

	if len(units) == 0 {
		return fmt.Errorf(`mediatek-accel interface requires at least one unit in "units" attribute`)
	}

	for _, entry := range units {
		name, ok := entry.(string)
		if !ok {
			return fmt.Errorf(`mediatek-accel "units" attribute must be a list of strings`)
		}

		if _, valid := mediatekAccelUnitRules[name]; !valid {
			return fmt.Errorf(`mediatek-accel plug has invalid unit %q in "units" attribute`, name)
		}
	}

	return nil
}

func (iface *mediatekAccelInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(mediatekAccelConnectedPlugAppArmorHeader)

	var units []string
	// validated in BeforePreparePlug
	_ = plug.Attr("units", &units)
	for _, name := range units {
		rule := mediatekAccelUnitRules[name]
		spec.AddSnippet(rule.AppArmor)
	}
	return nil
}

func (iface *mediatekAccelInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var units []string
	// validated in BeforePreparePlug
	_ = plug.Attr("units", &units)
	for _, name := range units {
		rule := mediatekAccelUnitRules[name]
		for _, udev := range rule.UDev {
			spec.TagDevice(udev)
		}
	}
	return nil
}

func init() {
	registerIface(&mediatekAccelInterface{commonInterface{
		name:                 "mediatek-accel",
		summary:              mediatekAccelSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: mediatekAccelBaseDeclarationSlots,
	}})
}
