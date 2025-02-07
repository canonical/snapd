// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/mount"
)

const npuSummary = `allows access to Npu stack`

const npuBaseDeclarationSlots = `
  npu:
    allow-installation:
      slot-snap-type:
        - core
`

const npuConnectedPlugAppArmor = `
# Description: Can access npu.

# Mediatek Genio APU devices
/dev/apusys* rw,
`

type npuInterface struct {
	commonInterface
}

var npuConnectedPlugUDev = []string{
	`SUBSYSTEM=="misc", KERNEL=="apusys"`,
}

func (iface *npuInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(npuConnectedPlugAppArmor)
	apparmor.GenWritableProfile(
		spec.AddUpdateNSf,
		nvProfilesDirInMountNs,
		3,
	)

	return nil
}

func (iface *npuInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return nil
}

func init() {
	registerIface(&npuInterface{
		commonInterface: commonInterface{
			name:                 "npu",
			summary:              npuSummary,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			baseDeclarationSlots: npuBaseDeclarationSlots,
			connectedPlugUDev:    npuConnectedPlugUDev,
		},
	})
}
