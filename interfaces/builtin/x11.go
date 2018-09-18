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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
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

# For Xorg to detect screens
/sys/devices/pci**/boot_vga r,
/sys/devices/pci**/resources r,
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

# Needed by QtSystems on X to detect mouse and keyboard. Note, the 'netlink
# raw' rule is not finely mediated by apparmor so we mediate with seccomp arg
# filtering.
network netlink raw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:* r,
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

func (iface *x11Interface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if !release.OnClassic {
		old := "###PLUG_SECURITY_TAGS###"
		new := plugAppLabelExpr(plug)
		snippet := strings.Replace(x11ConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *x11Interface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
		spec.AddSnippet(x11PermanentSlotSecComp)
	}
	return nil
}

func (iface *x11Interface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
		spec.AddSnippet(x11PermanentSlotAppArmor)
	}
	return nil
}

func (iface *x11Interface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	if !release.OnClassic {
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
	}})
}
