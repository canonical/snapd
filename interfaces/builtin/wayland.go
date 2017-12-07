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
	"strings"

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
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const waylandPermanentSlotAppArmor = `
# Description: Allow operating as a Wayland display server. This gives privileged access
# to the system.

# needed since Wayland is the display server, to configure tty devices
capability sys_tty_config,
/dev/tty[0-9]* rw,

/{dev,run}/shm/\#* mrw,

/run/wayland-[0-9]* rw,
/run/user/[0-9]*/wayland-[0-9]* rw,

# XWayland requires access to this
/run/xwayland-shared-* rw,

# Needed for mode setting via drmSetMaster() and drmDropMaster()
capability sys_admin,

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
shmctl
# for udev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const waylandConnectedSlotAppArmor = `
# Description: Permit clients to use Wayland
unix (receive, send) type=seqpacket addr=none peer=(label=###PLUG_SECURITY_TAGS###),
`

const waylandConnectedPlugAppArmor = `
# Description: Permit clients to connect to compositors supporting the Wayland protocol
unix (receive, send) type=seqpacket addr=none peer=(label=###SLOT_SECURITY_TAGS###),

# Allow access to the Wayland compositor server socket
owner /run/wayland-[0-9]* rw,
owner /run/user/*/wayland-[0-9]* rw,

# XWayland needs access to this
/run/xwayland-shared-* rw,

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
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	if release.OnClassic {
		// If we're running on classic Wayland will be part
		// of the OS snap and will run unconfined.
		new = "unconfined"
	} else {
		new = slotAppLabelExpr(slot)
	}
	snippet := strings.Replace(waylandConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
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

func (iface *waylandInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !release.OnClassic {
		old := "###PLUG_SECURITY_TAGS###"
		new := plugAppLabelExpr(plug)
		snippet := strings.Replace(waylandConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *waylandInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TagDevice(`KERNEL=="tty[0-9]*"`)
	spec.TagDevice(`KERNEL=="mice"`)
	spec.TagDevice(`KERNEL=="mouse[0-9]*"`)
	spec.TagDevice(`KERNEL=="event[0-9]*"`)
	spec.TagDevice(`KERNEL=="ts[0-9]*"`)
	return nil
}

func (iface *waylandInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&waylandInterface{})
}
