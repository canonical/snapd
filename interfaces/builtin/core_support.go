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

import (
	"github.com/snapcore/snapd/interfaces"
)

const coreSupportConnectedPlugAppArmor = `
# Description: Can control all aspects of systemd via the systemctl command,
# update rsyslog configuration, update systemd-timesyncd configuration and
# update/apply sysctl configuration. The interface allows execution of the
# systemctl binary unconfined and modifying all sysctl configuration. As such,
# this gives device ownership to the snap.

/bin/systemctl Uxr,

# Allow modifying rsyslog configuration for such things as remote logging. For
# now, only allow modifying NN-snap*.conf and snap*.conf files.
/etc/rsyslog.d/{,*}                     r,
/etc/rsyslog.d/{,[0-9][0-9]-}snap*.conf w,

# Allow modifying /etc/systemd/timesyncd.conf for adjusting systemd-timesyncd's
# timeservers
/etc/systemd/timesyncd.conf rw,

# Allow modifying sysctl configuration and applying the changes. For now, allow
# reading all sysctl files but only allow modifying NN-snap*.conf and
# snap*.conf files in /etc/sysctl.d.
/etc/sysctl.conf                       r,
/etc/sysctl.d/{,*}                     r,
/etc/sysctl.d/{,[0-9][0-9]-}snap*.conf w,
/{,usr/}{,s}bin/sysctl                 ixr,
@{PROC}/sys/{,**}                      r,
@{PROC}/sys/**                         w,

# Allow modifying logind configuration. For now, allow reading all logind
# configuration but only allow modifying NN-snap*.conf and snap*.conf files
# in /etc/systemd/logind.conf.d.
/etc/systemd/logind.conf                            r,
/etc/systemd/logind.conf.d/{,*}                     r,
/etc/systemd/logind.conf.d/{,[0-9][0-9]-}snap*.conf w,

# Allow managing the hostname with a core config option
/etc/hostname                         rw,
/{,usr/}{,s}bin/hostnamectl           ixr,

# Allow sync to be used
/bin/sync ixr,

# Allow modifying swapfile configuration for swapfile.service shipped in
# the core snap, general mgmt of the service is handled via systemctl
/etc/default/swapfile rw,

# Allow read/write access to the pi2 boot config.txt. WARNING: improperly
# editing this file may render the system unbootable.
owner /boot/uboot/config.txt rwk,
owner /boot/uboot/config.txt.tmp rwk,
`

// NewShutdownInterface returns a new "shutdown" interface.
func NewCoreSupportInterface() interfaces.Interface {
	return &commonInterface{
		name: "core-support",
		connectedPlugAppArmor: coreSupportConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
