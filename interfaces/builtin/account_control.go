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

const accountControlSummary = `allows managing non-system user accounts`

const accountControlBaseDeclarationSlots = `
  account-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const accountControlConnectedPlugAppArmor = `
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

# useradd writes the result in the log
#include <abstractions/wutmp>
/var/log/faillog rwk,
`

// Needed because useradd uses a netlink socket
const accountControlConnectedPlugSecComp = `
# useradd requires chowning to 'shadow'
fchown - u:root
fchown32 - u:root

# from libaudit1
bind
socket AF_NETLINK - NETLINK_AUDIT
`

func init() {
	registerIface(&commonInterface{
		name:                  "account-control",
		summary:               accountControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  accountControlBaseDeclarationSlots,
		connectedPlugAppArmor: accountControlConnectedPlugAppArmor,
		connectedPlugSecComp:  accountControlConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
