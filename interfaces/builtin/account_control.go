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
	"strconv"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/osutil"
)

const accountControlSummary = `allows managing non-system user accounts`

const accountControlBaseDeclarationSlots = `
  account-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const accountControlConnectedPlugAppArmor = `
#include <abstractions/dbus-strict>
# Introspection of org.freedesktop.Accounts
dbus (send)
    bus=system
    path=/org/freedesktop/Accounts{,/User[0-9]*}
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/Accounts
    interface=org.freedesktop.Accounts
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/Accounts/User[0-9]*
    interface=org.freedesktop.Accounts.User
    peer=(label=unconfined),
# Read all properties from Accounts
dbus (send)
    bus=system
    path=/org/freedesktop/Accounts{,/User[0-9]*}
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),
# Receive Accounts property changed events
dbus (receive)
    bus=system
    path=/org/freedesktop/Accounts{,/User[0-9]*}
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

/{,usr/}sbin/chpasswd ixr,
/{,usr/}sbin/user{add,del} ixr,

# Allow modifying the non-system extrausers NSS database. The extrausers
# database is used on Ubuntu Core devices to manage both privileged and
# unprivileged users (since /etc/passwd, /etc/group, etc are all read-only).
/var/lib/extrausers/ r,
/var/lib/extrausers/** rwkl,

# Needed by useradd
/etc/login.defs r,
/etc/default/useradd r,
/etc/default/nss r,
/etc/pam.d/{,*} r,
/{,usr/}sbin/pam_tally2 ixr,

# Needed by chpasswd
/{,usr/}lib/@{multiarch}/security/* ixr,

# Useradd needs netlink
network netlink raw,

# Capabilities needed by useradd
capability audit_write,
capability chown,
capability fsetid,

# useradd writes the result in the log
# faillog tracks failed events, lastlog maintain records of the last
# time a user successfully logged in, tallylog maintains records of
# failures.
#include <abstractions/wutmp>
/var/log/faillog rwk,
/var/log/lastlog rwk,
/var/log/tallylog rwk,
`

// Needed because useradd uses a netlink socket, {{group}} is used as a
// placeholder argument for the actual ID of a group owning /etc/shadow
const accountControlConnectedPlugSecCompTemplate = `
# useradd requires chowning to 0:'{{group}}'
fchown - u:root {{group}}
fchown32 - u:root {{group}}

# from libaudit1
bind
socket AF_NETLINK - NETLINK_AUDIT
`

type accountControlInterface struct {
	commonInterface
	secCompSnippet string
}

func makeAccountControlSecCompSnippet() (string, error) {
	gid, err := osutil.FindGidOwning("/etc/shadow")
	if err != nil {
		return "", err
	}

	snippet := strings.Replace(accountControlConnectedPlugSecCompTemplate,
		"{{group}}", strconv.FormatUint(gid, 10), -1)

	return snippet, nil
}

func (iface *accountControlInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if iface.secCompSnippet == "" {
		// Cache the snippet after it's successfully computed once
		snippet, err := makeAccountControlSecCompSnippet()
		if err != nil {
			return err
		}
		iface.secCompSnippet = snippet
	}
	spec.AddSnippet(iface.secCompSnippet)
	return nil
}

func init() {
	registerIface(&accountControlInterface{commonInterface: commonInterface{
		name:                  "account-control",
		summary:               accountControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  accountControlBaseDeclarationSlots,
		connectedPlugAppArmor: accountControlConnectedPlugAppArmor,
		// handled by SecCompConnectedPlug
		connectedPlugSecComp: "",
	}})
}
