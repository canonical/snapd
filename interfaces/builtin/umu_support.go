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

// Support interface for umu-launcher, which  is an alternative variant of 
// Steam Runtime and Tools. Uses the same underlying containerization technology
// as actual steam. See https://github.com/Open-Wine-Components/umu-launcher for details.
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
# Description: allow UMU launcher (via proot-next) to intercept syscalls using
# seccomp user notifications, traverse paths safely with RESOLVE_NO_SYMLINKS,
# and monitor child process exits without requiring an unrestricted profile.
# umu-support-openat2

openat2
`

// same input rules as steam - joysticks and game controllers access
const umuSupportSteamInputUDevRules = steamSupportSteamInputUDevRules

// same input rules as steam - VR controllers
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
