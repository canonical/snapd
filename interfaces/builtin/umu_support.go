// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
)

const umuSupportSummary = `allows UMU launcher to configure pressure-vessel containers`

const umuSupportBaseDeclarationPlugs = `
  umu-support:
    allow-installation: true
    deny-auto-connection: true
`

const umuSupportBaseDeclarationSlots = `
  umu-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const umuSupportConnectedPlugAppArmor = `
# Allow pv-adverb to list and access /proc entries
@{PROC}/@{pids}/ r,
@{PROC}/@{pids}/** r,

# Permission for proot-next bwrap root directory
owner /run/user/[0-9]*/bwrap/ rw,
owner /run/user/[0-9]*/bwrap/** mrwklix,

# Access to mounting points
/media/ r,
/mnt/ r,
/run/media/ r,
`

const umuSupportConnectedPlugSecComp = `
# Description: allow UMU launcher to run without a seccomp profile so that
# pressure-vessel containers can use any features available on the system
# without having to perpetually update the snapd interface side.

@unrestricted
`

const umuSupportSteamInputUDevRules = steamSupportSteamInputUDevRules

const umuSupportSteamVRUDevRules = steamSupportSteamVRUDevRules

type umuSupportInterface struct {
	commonInterface
}

func (iface *umuSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(umuSupportSteamInputUDevRules)
	spec.AddSnippet(umuSupportSteamVRUDevRules)
	return iface.commonInterface.UDevConnectedPlug(spec, plug, slot)
}

func init() {
	registerIface(&umuSupportInterface{commonInterface{
		name:                  "umu-support",
		summary:               umuSupportSummary,
		implicitOnCore:        release.OnCoreDesktop,
		implicitOnClassic:     true,
		baseDeclarationSlots:  umuSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  umuSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: umuSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  umuSupportConnectedPlugSecComp,
	}})
}
