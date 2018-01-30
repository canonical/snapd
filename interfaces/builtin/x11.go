// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const x11Summary = `allows interacting with the X11 server`

const x11BaseDeclarationSlots = `
  x11:
    allow-installation:
      slot-snap-type:
        - core
`

const x11ConnectedPlugAppArmor = `
# Description: Can access the X server. Restricted because X does not prevent
# eavesdropping or apps interfering with one another.

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

func init() {
	registerIface(&commonInterface{
		name:                  "x11",
		summary:               x11Summary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  x11BaseDeclarationSlots,
		connectedPlugAppArmor: x11ConnectedPlugAppArmor,
		connectedPlugSecComp:  x11ConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
