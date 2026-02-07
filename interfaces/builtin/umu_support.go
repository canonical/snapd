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
    allow-installation: false
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
# Allow pressure-vessel to set up its Bubblewrap sandbox
@{PROC}/sys/kernel/overflowuid r,
@{PROC}/sys/kernel/overflowgid r,
@{PROC}/sys/kernel/sched_autogroup_enabled r,
owner @{PROC}/@{pid}/uid_map rw,
owner @{PROC}/@{pid}/gid_map rw,
owner @{PROC}/@{pid}/setgroups rw,
owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountinfo r,

# Allow mounting operations
mount,
umount,
pivot_root,

# Allow access to user namespaces
userns,

# Permission for the new root
/newroot/** rwkl,

# Specific pressure-vessel assemblies
mount options=(rw, rbind) /oldroot/usr/ -> /newroot/run/host/usr/,

# Restrictive host access
/run/host/usr/lib/** mr,

# Permission for bwrap temporary files
/bindfile* rw,

# Mounting temporary files
mount options=(rw, rbind) /bindfile* -> /newroot/**,

# Permission to read mount paths.
/media/ r,
/mnt/ r,
/run/media/ r,

# Broad execution permissions for container binaries
/usr/bin/steam-runtime-launcher-interface-* ixr,
/usr/lib/pressure-vessel/from-host/libexec/steam-runtime-tools-*/* ixr,

# Allow access to pressure-vessel directories
/run/pressure-vessel/** mrw,
/var/pressure-vessel/** mrw,

# Allow access to icons and shortcuts directories
owner /home/*/.config/menus/{,**} rw,
owner /home/*/.local/share/applications/{,**} rw,
owner /home/*/.local/share/desktop-directories/{,**} rw,
owner /home/*/.local/share/icons/{,**} rw,

# Permissions to access certain binaries
# For UMU launcher
/usr/bin/zenity ixr,
/run/host/usr/sbin/ldconfig* ixr,
# For Heroic Games Launcher
/usr/bin/df ixr,

# Allow basic operations needed by pressure-vessel
capability sys_admin,
`

const umuSupportConnectedPlugSecComp = `
# Description: allow UMU launcher to run without a seccomp profile so that
# pressure-vessel containers can use any features available on the system

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