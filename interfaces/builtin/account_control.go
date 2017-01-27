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
)

const accountControlConnectedPlugAppArmor = `
# Allow creating, modifying and deleting non-system users and account password.
/{,usr/}sbin/chpasswd ixr,
/{,usr/}sbin/user{add,del} ixr,

# Only allow modifying the non-system extrausers database
/var/lib/extrausers/ r,
/var/lib/extrausers/** rwkl,

# Needed by useradd
/etc/login.defs r,
/etc/default/useradd r,
/etc/default/nss r,
/etc/pam.d/{,*} r,

# Useradd needs netlink
network netlink raw,

# Capabilities needed by useradd
capability audit_write,
capability chown,
capability fsetid,

# useradd write the result in the log
/var/log/lastlog rw,
/var/log/faillog rw,
`

// Needed because useradd uses a netlink socket
const accountControlConnectedPlugSecComp = `
sendto
recvfrom
fchown
fchown32
`

// Interface which allows to handle the user accounts.
func NewAccountControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "account-control",
		connectedPlugAppArmor: accountControlConnectedPlugAppArmor,
		connectedPlugSecComp:  accountControlConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
