// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
)

const waylandSummary = `allows access to compositors supporting wayland protocol`

const waylandBaseDeclarationSlots = `
  wayland:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
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
owner /run/user/[0-9]*/wayland-[0-9]* rwk,
# Allow access to common client Wayland sockets from non-snap clients
/run/user/[0-9]*/{mesa,mutter,sdl,wayland-cursor,weston,xwayland}-shared-* rw,
# Some Wayland based toolkits (Qt, GTK3, SDL2) and Xwayland create shm files to pass
# to the server. Although they are passed by FD we still need rw access to the file.
/run/user/[0-9]*/snap.*/{wayland-cursor,xwayland}-shared-* rw,

# Allow write access to create /run/user/* to create XDG_RUNTIME_DIR (until
# lp:1738197 is fixed). Note this is not needed if creating a session using
# logind (as provided by the login-session-control snapd interface).
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

# MESA reads this dri config file
/etc/drirc r,
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

const waylandConnectedSlotAppArmor = `
# Allow access to common client Wayland sockets for connected snaps
owner /run/user/[0-9]*/###PLUG_SECURITY_TAGS###/{mesa,mutter,sdl,wayland-cursor,weston,xwayland}-shared-* rw,
`

const waylandConnectedPlugAppArmor = `
# Allow access to the Wayland compositor server socket
owner /run/user/[0-9]*/wayland-[0-9]* rw,

# Needed when using QT_QPA_PLATFORM=wayland-egl (MESA dri config)
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
	spec.AddSnippet(waylandConnectedPlugAppArmor)
	return nil
}

func (iface *waylandInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := "snap." + plug.Snap().InstanceName() // forms the snap-instance-specific subdirectory name of /run/user/*/ used for XDG_RUNTIME_DIR
	snippet := strings.Replace(waylandConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *waylandInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(waylandPermanentSlotSecComp)
	return nil
}

func (iface *waylandInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(waylandPermanentSlotAppArmor)
	return nil
}

func (iface *waylandInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TriggerSubsystem("input")
	spec.TagDevice(`KERNEL=="tty[0-9]*"`)
	spec.TagDevice(`KERNEL=="mice"`)
	spec.TagDevice(`KERNEL=="mouse[0-9]*"`)
	spec.TagDevice(`KERNEL=="event[0-9]*"`)
	spec.TagDevice(`KERNEL=="ts[0-9]*"`)
	return nil
}

func (iface *waylandInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&waylandInterface{})
}
