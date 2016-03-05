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
	"fmt"

	"github.com/ubuntu-core/snappy/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/network
const connectedPlugAppArmor = `
# Description: Can access the network as a client.
# Usage: common
#include <abstractions/nameservice>
#include <abstractions/ssl_certs>

@{PROC}/sys/net/core/somaxconn r,
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/seccomp/policygroups/ubuntu-core/16.04/network
const connectedPlugSecComp = `
# Description: Can access the network as a client.
# Usage: common
connect
getpeername
getsockname
getsockopt
recv
recvfrom
recvmmsg
recvmsg
send
sendmmsg
sendmsg
sendto
setsockopt
shutdown

# LP: #1446748 - limit this to AF_UNIX/AF_LOCAL and perhaps AF_NETLINK
socket

# This is an older interface and single entry point that can be used instead
# of socket(), bind(), connect(), etc individually. While we could allow it,
# we wouldn't be able to properly arg filter socketcall for AF_INET/AF_INET6
# when LP: #1446748 is implemented.
#socketcall
`

// NetworkInterface implements the "network" interface.
//
// Snaps that have a connected plug of this type can access the network as a
// client. The OS snap will have the only slot of this type.
//
// Usage: common
type NetworkInterface struct{}

// Name returns the string "network".
func (iface *NetworkInterface) Name() string {
	return "network"
}

// SanitizeSlot checks and possibly modifies a slot.
func (iface *NetworkInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	if slot.Snap != "ubuntu-core" {
		return fmt.Errorf("slots using the network interface are reserved for ubuntu-core")
	}
	return nil
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *NetworkInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

// PermanentPlugSnippet returns the snippet of text for the given security
// system that is used during the whole lifetime of affected applications,
// whether the plug is connected or not.
//
// Plugs don't get any permanent security snippets.
func (iface *NetworkInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedPlugSnippet returns the snippet of text for the given security
// system that is used by affected application, while a specific connection
// between a plug and a slot exists.
//
// Connected plugs get the static seccomp and apparmor blobs defined at the top
// of the file. They are not really connection specific in this case.
func (iface *NetworkInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(connectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(connectedPlugSecComp), nil
	case interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// PermanentSlotSnippet returns the snippet of text for the given security
// system that is used during the whole lifetime of affected applications,
// whether the slot is connected or not.
//
// Slots don't get any permanent security snippets.
func (iface *NetworkInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// ConnectedSlotSnippet returns the snippet of text for the given security
// system that is used by affected application, while a specific connection
// between a plug and a slot exists.
//
// Slots don't get any per-connection security snippets.
func (iface *NetworkInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}
