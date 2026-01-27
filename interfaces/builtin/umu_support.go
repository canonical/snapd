// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
# Allow basic operations needed by pressure-vessel
capability sys_admin,

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

# Allow Bubblewrap to create directories for bind mounts
/run/host/** rwkl,

# Allow access to tmpfs for intermediate roots
/tmp/** rwkl,

# Allow access to icons directories
owner /home/*/.local/share/icons/ rw,

# Allow access to applications directories
owner /home/*/.local/share/applications/ rw,
owner /home/*/.config/menus/ rw,
owner /home/*/.local/share/desktop-directories/ rw,

# Allow access to pressure-vessel directories
/tmp/pressure-vessel/** mrw,
/run/pressure-vessel/** mrw,

# Avoid Heroic Games Launcher "EACCES: permission denied" error and bwrap: Can't mkdir parents for /run/host/usr: Permission denied
allow file,
`

const umuSupportConnectedPlugSecComp = `
# Description: allow UMU launcher to run without a seccomp profile so that
# pressure-vessel containers can use any features available on the system

@unrestricted
`

const umuSupportSteamInputUDevRules = `
# Valve USB devices
SUBSYSTEM=="usb", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"
KERNEL=="uinput", SUBSYSTEM=="misc", TAG+="uaccess", OPTIONS+="static_node=uinput"
KERNEL=="hidraw*", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"
KERNEL=="hidraw*", KERNELS=="*28DE:*", MODE="0660", TAG+="uaccess"
`

type umuSupportInterface struct {
	commonInterface
}

func (iface *umuSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Similar approach to Steam Support but with more restricted permissions.
	spec.AddSnippet(umuSupportConnectedPlugAppArmor)

	return nil
}

func (iface *umuSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Basic rules for input devices
	spec.AddSnippet(umuSupportSteamInputUDevRules)
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