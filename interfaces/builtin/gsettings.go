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

const gsettingsConnectedPlugAppArmor = `
# Description: Can access global gsettings of the user's session. Restricted
# because this gives privileged access to sensitive information stored in
# gsettings and allows adjusting settings of other applications.
# Usage: reserved

#include <abstractions/dbus-session-strict>

#include <abstractions/dconf>
owner /{,var/}run/user/*/dconf/user w,
owner @{HOME}/.config/dconf/user w,
dbus (receive, send)
    bus=session
    interface="ca.desrt.dconf.Writer"
    peer=(label=unconfined),
`

const gsettingsConnectedPlugSecComp = `
# Description: Can access global gsettings of the user's session. Restricted
# because this gives privileged access to sensitive information stored in
# gsettings and allows adjusting settings of other applications.

# dbus
recvmsg
send
sendto
sendmsg
`

// NewGsettingsInterface returns a new "gsettings" interface.
func NewGsettingsInterface() interfaces.Interface {
	return &commonInterface{
		name: "gsettings",
		connectedPlugAppArmor: gsettingsConnectedPlugAppArmor,
		connectedPlugSecComp:  gsettingsConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
