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

var mediatekAccelUnitRules = map[string]mediatekAccelRule{
	"apu": {
		AppArmor: "# APU (AI Processing Unit)\n" +
			"/dev/apusys rw,\n",
		UDev: []string{
			`SUBSYSTEM=="misc", KERNEL=="apusys"`,
		},
	},
	"vcu": {
		AppArmor: "# VPU (MediaTek Video Processor Unit)\n" +
			"/dev/vcu rw,\n" +
			"# MDP (MediaTek Media Data Path) and other vcu in the future\n" +
			"/dev/vcu[0-9]* rw,\n",
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
	allDisabled := true

	for name := range mediatekAccelUnitRules {
		if p, ok := plug.Attrs[name]; ok {
			v, ok := p.(bool)
			if !ok {
				return fmt.Errorf(`mediatek-accel "%s" attribute must be boolean`, name)
			}
			if v {
				allDisabled = false
			}
		}
	}
	if allDisabled {
		return fmt.Errorf(`cannot connect mediatek-accel interface without any units enabled`)
	}

	return nil
}

func (iface *mediatekAccelInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if err := iface.commonInterface.AppArmorConnectedPlug(spec, plug, slot); err != nil {
		return err
	}
	spec.AddSnippet(mediatekAccelConnectedPlugAppArmorHeader)
	for name, rule := range mediatekAccelUnitRules {
		v := false
		_ = plug.Attr(name, &v)
		if v {
			spec.AddSnippet(rule.AppArmor)
		}
	}
	return nil
}

func (iface *mediatekAccelInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if err := iface.commonInterface.UDevConnectedPlug(spec, plug, slot); err != nil {
		return err
	}

	for name, rule := range mediatekAccelUnitRules {
		v := false
		_ = plug.Attr(name, &v)
		if v {
			for _, udev := range rule.UDev {
				spec.TagDevice(udev)
			}
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
