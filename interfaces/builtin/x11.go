// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/x
const x11ConnectedPlugAppArmor = `
# Description: Can access the X server. Restricted because X does not prevent
# eavesdropping or apps interfering with one another.

#include <abstractions/X>
#include <abstractions/fonts>

/var/cache/fontconfig/   r,
/var/cache/fontconfig/** mr,

# Allow access to the user specific copy of the xauth file specified
# in the XAUTHORITY environment variable, that "snap run" creates on
# startup.
owner /run/user/[0-9]*/Xauthority r,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/x
const x11ConnectedPlugSecComp = `
# Description: Can access the X server. Restricted because X does not prevent
# eavesdropping or apps interfering with one another.

shutdown
`

// NewX11Interface returns a new "x11" interface.
func NewX11Interface() interfaces.Interface {
	return &commonInterface{
		name: "x11",
		connectedPlugAppArmor: x11ConnectedPlugAppArmor,
		connectedPlugSecComp:  x11ConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
