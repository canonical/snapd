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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const pipewireSummary = `allows access to the pipewire sockets, and offer them`

const pipewireBaseDeclarationSlots = `
  pipewire:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const pipewireConnectedPlugAppArmor = `
owner /run/user/[0-9]*/ r,
owner /run/user/[0-9]*/pipewire-[0-9] rw,
`

const pipewireConnectedPlugAppArmorCore = `
owner /run/user/[0-9]*/###SLOT_SECURITY_TAGS###/pipewire-[0-9] rw,
# To allow to use pipewire in system mode, instead of user mode
owner /var/snap/###SLOT_INSTANCE_NAME###/common/pipewire-[0-9] rw,
`

const pipewireConnectedPlugSecComp = `
shmctl
`

const pipewirePermanentSlotAppArmor = `
owner @{PROC}/@{pid}/exe r,

# For udev
network netlink raw,
/sys/devices/virtual/dmi/id/sys_vendor r,
/sys/devices/virtual/dmi/id/bios_vendor r,
/sys/**/sound/** r,

owner /run/user/[0-9]*/pipewire-[0-9] rwk,
owner /run/user/[0-9]*/pipewire-[0-9]-manager rwk,
`

const pipewirePermanentSlotSecComp = `
# The following are needed for UNIX sockets
personality
setpriority
bind
listen
accept
accept4
shmctl
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

type pipewireInterface struct {
	commonInterface
}

func (iface *pipewireInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(pipewireConnectedPlugAppArmor)
	if !implicitSystemConnectedSlot(slot) {
		old := "###SLOT_SECURITY_TAGS###"
		new := "snap." + slot.Snap().InstanceName() // forms the snap-instance-specific subdirectory name of /run/user/*/ used for XDG_RUNTIME_DIR
		snippet := strings.Replace(pipewireConnectedPlugAppArmorCore, old, new, -1)
		old2 := "###SLOT_INSTANCE_NAME###"
		new2 := slot.Snap().InstanceName() // forms the snap-instance-specific subdirectory name of /var/snap/*/common used for SNAP_COMMON
		snippet = strings.Replace(snippet, old2, new2, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *pipewireInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pipewirePermanentSlotAppArmor)
	return nil
}

func (iface *pipewireInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pipewirePermanentSlotSecComp)
	return nil
}

func init() {
	registerIface(&pipewireInterface{commonInterface: commonInterface{
		name:                 "pipewire",
		summary:              pipewireSummary,
		implicitOnCore:       false,
		implicitOnClassic:    true,
		baseDeclarationSlots: pipewireBaseDeclarationSlots,
		connectedPlugSecComp: pipewireConnectedPlugSecComp,
	}})
}
