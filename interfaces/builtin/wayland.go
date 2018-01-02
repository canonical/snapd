// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const waylandSummary = `allows access to compositors supporting wayland protocol`

const waylandBaseDeclarationSlots = `
  wayland:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection:
      on-classic: false
`

const waylandPermanentSlotAppArmor = `
# Description: Allow operating as a Wayland display server. This gives privileged access
# to the system.

# needed since Wayland is a display server and needs to configure tty devices
capability sys_tty_config,
/dev/tty[0-9]* rw,

# Create the Wayland socket and lock file
/run/user/[0-9]*/wayland-[0-9]* rw,
/run/user/[0-9]*/wayland-shared-* rw,
/run/user/[0-9]*/wayland-cursor-shared-* rw,

# Allow write access to create XDG_RUNTIME_DIR (until lp:1656340 is fixed)
/run/user/[0-9]*/ w,

# Needed for mode setting via drmSetMaster() and drmDropMaster()
capability sys_admin,

# Weston probes this on start
/sys/devices/pci**/boot_vga r,

# NOTE: this allows reading and inserting all input events
/dev/input/* rw,

# For using udev
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:input[0-9]* r,
/run/udev/data/+platform:* r,
`

const waylandPermanentSlotSecComp = `
# Description: Allow operating as a Wayland server. This gives privileged access
# to the system.
# Needed for server launch
bind
listen
# Needed by server upon client connect
accept
accept4
# for udev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const waylandConnectedPlugAppArmor = `
# Allow access to the Wayland compositor server socket
owner /run/user/[0-9]*/wayland-[0-9]* rw,
owner /run/user/[0-9]*/wayland-shared-* rw,
owner /run/user/[0-9]*/wayland-cursor-shared-* rw,

# Needed when using QT_QPA_PLATFORM=wayland-egl
/etc/drirc r,
`

type waylandInterface struct{}

func (iface *waylandInterface) Name() string {
	return "wayland"
}

func (iface *waylandInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              waylandSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: waylandBaseDeclarationSlots,
	}
}

func (iface *waylandInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(slotAppLabelExpr(slot))
	return nil
}

func (iface *waylandInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
		spec.AddSnippet(waylandPermanentSlotSecComp)
	}
	return nil
}

func (iface *waylandInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
		spec.AddSnippet(waylandPermanentSlotAppArmor)
	}
	return nil
}

func (iface *waylandInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
		spec.TagDevice(`KERNEL=="tty[0-9]*"`)
		spec.TagDevice(`KERNEL=="mice"`)
		spec.TagDevice(`KERNEL=="mouse[0-9]*"`)
		spec.TagDevice(`KERNEL=="event[0-9]*"`)
		spec.TagDevice(`KERNEL=="ts[0-9]*"`)
	}
	return nil
}

func (iface *waylandInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&waylandInterface{})
}
