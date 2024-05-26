// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

const x11Summary = `allows interacting with or running as an X11 server`

const x11BaseDeclarationSlots = `
  x11:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
    deny-auto-connection:
      on-classic: false
`

const x11PermanentSlotAppArmor = `
# Description: Allow operating as an X11 display server. This gives privileged access
# to the system.

# needed since X11 is a display server and needs to configure tty devices
capability sys_tty_config,
/dev/tty[0-9]* rw,

# Needed for mode setting via drmSetMaster() and drmDropMaster()
capability sys_admin,

# NOTE: this allows reading and inserting all input events
/dev/input/* rw,

# For using udev
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:input[0-9]* r,
/run/udev/data/+platform:* r,

# the unix socket to use to connect to the display
unix (bind, listen, accept)
     type=stream
     addr="@/tmp/.X11-unix/X[0-9]*",
unix (bind, listen, accept)
     type=stream
     addr="@/tmp/.ICE-unix/[0-9]*",

# On systems with Tegra drivers, X11 needs to create the socket for clients to
# use.
unix (bind, listen, accept)
     type=dgram
     addr="@nvidia[0-9a-f]*",

# For Xorg to detect screens
/sys/devices/pci**/boot_vga r,
/sys/devices/pci**/resources r,

# TODO: enable rules for writing Xwayland Xauth files for clients to read when
# something like gnome-shell is running confined with an x11 slot
`

const x11PermanentSlotSecComp = `
# Description: Allow operating as an X11 server. This gives privileged access
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

const x11ConnectedSlotAppArmor = `
# Description: Allow clients access to the X11 server socket
unix (connect, receive, send, accept)
    type=stream
    addr="@/tmp/.X11-unix/X[0-9]*"
    peer=(label=###PLUG_SECURITY_TAGS###),
# TODO: deprecate and remove this if it doesn't break X11 server snaps.
unix (connect, receive, send, accept)
    type=stream
    addr="@/tmp/.ICE-unix/[0-9]*"
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const x11ConnectedPlugAppArmor = `
# Description: Can access the X server. Restricted because X does not prevent
# eavesdropping or apps interfering with one another.

# The X abstraction doesn't check the peer label, but in this case that's
# ok because x11ConnectedSlotAppArmor will limit which clients can connect
# to the slot implementation.
#include <abstractions/X>
#include <abstractions/fonts>
owner @{HOME}/.local/share/fonts/{,**} r,
/var/cache/fontconfig/   r,
/var/cache/fontconfig/** mr,

# Allow access to the user specific copy of the xauth file specified
# in the XAUTHORITY environment variable, that "snap run" creates on
# startup.
owner /run/user/[0-9]*/.Xauthority r,

# Allow reading an Xwayland Xauth file
# (see https://gitlab.gnome.org/GNOME/mutter/merge_requests/626)
owner /run/user/[0-9]*/.mutter-Xwaylandauth.* r,
owner /run/user/[0-9]*/mutter/Xauthority r,

# Allow reading KDE Plasma's Xwayland Xauth file
owner /run/user/[0-9]*/xauth_* r,


# Needed by QtSystems on X to detect mouse and keyboard. Note, the 'netlink
# raw' rule is not finely mediated by apparmor so we mediate with seccomp arg
# filtering.
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:* r,

# Deny access to ICE granted by abstractions/X
# See: https://bugs.launchpad.net/snapd/+bug/1901489
deny owner @{HOME}/.ICEauthority r,
deny owner /run/user/*/ICEauthority r,
deny unix (connect, receive, send)
    type=stream
    peer=(addr="@/tmp/.ICE-unix/[0-9]*"),
`

const x11ConnectedPlugSecComp = `
# Description: Can access the X server. Restricted because X does not prevent
# eavesdropping or apps interfering with one another.

# Needed by QtSystems on X to detect mouse and keyboard
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
bind
`

type x11Interface struct {
	commonInterface
}

func (iface *x11Interface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if implicitSystemConnectedSlot(slot) {
		// X11 slot is provided by the host system. Bring the host's
		// /tmp/.X11-unix/ directory over to the snap mount namespace.
		return spec.AddMountEntry(osutil.MountEntry{
			Name:    "/var/lib/snapd/hostfs/tmp/.X11-unix",
			Dir:     "/tmp/.X11-unix",
			Options: []string{"bind", "ro"},
		})
	}

	// X11 slot is provided by another snap on the system. Bring that snap's
	// /tmp/.X11-unix/ directory over to the snap mount namespace. Here we
	// rely on the predictable naming of the private /tmp directory of the
	// slot-side snap which is currently provided by snap-confine.

	// But if the same snap is providing both the plug and the slot, this is
	// not necessary.
	if plug.Snap().InstanceName() == slot.Snap().InstanceName() {
		return nil
	}
	slotSnapName := slot.Snap().InstanceName()
	return spec.AddMountEntry(osutil.MountEntry{
		Name:    fmt.Sprintf("/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.%s/tmp/.X11-unix", slotSnapName),
		Dir:     "/tmp/.X11-unix",
		Options: []string{"bind", "ro"},
	})
}

func (iface *x11Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	mylog.Check(iface.commonInterface.AppArmorConnectedPlug(spec, plug, slot))

	// Consult the comments in MountConnectedPlug for the rationale of the control flow.
	if implicitSystemConnectedSlot(slot) {
		spec.AddUpdateNS(`
		/{,var/lib/snapd/hostfs/}tmp/.X11-unix/ rw,
		mount options=(rw, bind) /var/lib/snapd/hostfs/tmp/.X11-unix/ -> /tmp/.X11-unix/,
		mount options=(ro, remount, bind) -> /tmp/.X11-unix/,
		mount options=(rslave) -> /tmp/.X11-unix/,
		umount /tmp/.X11-unix/,
		`)
		return nil
	}
	if plug.Snap().InstanceName() == slot.Snap().InstanceName() {
		return nil
	}
	slotSnapName := slot.Snap().InstanceName()
	spec.AddUpdateNS(fmt.Sprintf(`
	/tmp/.X11-unix/ rw,
	/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.%s/tmp/.X11-unix/ rw,
	mount options=(rw, bind) /var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.%s/tmp/.X11-unix/ -> /tmp/.X11-unix/,
	mount options=(ro, remount, bind) -> /tmp/.X11-unix/,
	mount options=(rslave) -> /tmp/.X11-unix/,
	umount /tmp/.X11-unix/,
	`, slotSnapName, slotSnapName))
	return nil
}

func (iface *x11Interface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := spec.SnapAppSet().PlugLabelExpression(plug)
		snippet := strings.Replace(x11ConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *x11Interface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(x11PermanentSlotSecComp)
	}
	return nil
}

func (iface *x11Interface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(x11PermanentSlotAppArmor)
	}
	return nil
}

func (iface *x11Interface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.TriggerSubsystem("input")
		spec.TagDevice(`KERNEL=="tty[0-9]*"`)
		spec.TagDevice(`KERNEL=="mice"`)
		spec.TagDevice(`KERNEL=="mouse[0-9]*"`)
		spec.TagDevice(`KERNEL=="event[0-9]*"`)
		spec.TagDevice(`KERNEL=="ts[0-9]*"`)
	}
	return nil
}

func init() {
	registerIface(&x11Interface{commonInterface{
		name:                  "x11",
		summary:               x11Summary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  x11BaseDeclarationSlots,
		connectedPlugAppArmor: x11ConnectedPlugAppArmor,
		connectedPlugSecComp:  x11ConnectedPlugSecComp,
		// affects the plug snap because of mount backend
		affectsPlugOnRefresh: true,
	}})
}
