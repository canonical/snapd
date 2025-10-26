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

const mediatekAccelConnectedPlugAppArmor = `
# Description: Provide permissions for accessing the hardware accelerators on MediaTek Genio devices

# APU (AI Processing Unit)
/dev/apusys rw,
`
const mediatekAccelVcuConnectedPlugAppArmor = `
# VPU (MediaTek Video Processor Unit)
/dev/vcu rw,

# MDP (MediaTek Data Path)
/dev/vcu1 rw,
`

var mediatekAccelConnectedPlugUDev = []string{
	/* For APU (MediaTek AI Processing Unit) */
	`SUBSYSTEM=="misc", KERNEL=="apusys"`,
}

var mediatekAccelVcuConnectedPlugUDev = []string{
	/* For VPU (MediaTek Video Processor Unit) */
	`SUBSYSTEM=="vcu", KERNEL=="vcu"`,

	/* For MDP (MediaTek Data Path) */
	`SUBSYSTEM=="vcu1", KERNEL=="vcu1"`,
}

type mediatekAccelInterface struct {
	commonInterface
}

func (iface *mediatekAccelInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if p, ok := plug.Attrs["vcu"]; ok {
		if _, ok := p.(bool); !ok {
			return fmt.Errorf(`mediatek-accel "vcu" attribute must be boolean`)
		}
	}

	return nil
}

func (iface *mediatekAccelInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var allowVcu bool
	_ = plug.Attr("vcu", &allowVcu)

	if err := iface.commonInterface.AppArmorConnectedPlug(spec, plug, slot); err != nil {
		return err
	}

	if allowVcu {
		spec.AddSnippet(mediatekAccelVcuConnectedPlugAppArmor)
	}

	return nil
}

func (iface *mediatekAccelInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var allowVcu bool
	_ = plug.Attr("vcu", &allowVcu)

	if err := iface.commonInterface.UDevConnectedPlug(spec, plug, slot); err != nil {
		return err
	}

	if allowVcu {
		for _, rule := range mediatekAccelVcuConnectedPlugUDev {
			spec.TagDevice(rule)
		}
	}

	return nil
}

func init() {
	registerIface(&mediatekAccelInterface{commonInterface{
		name:                  "mediatek-accel",
		summary:               mediatekAccelSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  mediatekAccelBaseDeclarationSlots,
		connectedPlugAppArmor: mediatekAccelConnectedPlugAppArmor,
		connectedPlugUDev:     mediatekAccelConnectedPlugUDev,
	}})
}
