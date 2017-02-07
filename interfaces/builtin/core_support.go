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

const coreSupportConnectedPlugAppArmor = `
# Description: Can control all aspects of systemd via the systemctl command
# and update rsyslog configuration. The interface allows execution of the
# systemctl binary unconfined. As such, this gives device ownership to the
# snap.

/bin/systemctl Uxr,

# Allow modifying rsyslog configuration for such things as remote logging. For
# now, only allow modifying NN-snap*.conf and snap*.conf files.
/etc/rsyslog.d/{,*}                     r,
/etc/rsyslog.d/{,[0-9][0-9]-}snap*.conf w,

# Allow modifying /etc/systemd/timesyncd.conf for adjusting systemd-timesyncd's
# timeservers
/etc/systemd/timesyncd.conf rw,
`

const coreSupportConnectedPlugSecComp = `
sendmsg
recvmsg
sendto
recvfrom
`

// NewShutdownInterface returns a new "shutdown" interface.
func NewCoreSupportInterface() interfaces.Interface {
	return &commonInterface{
		name: "core-support",
		connectedPlugAppArmor: coreSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  coreSupportConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
