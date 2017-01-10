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
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/release"
)

const upowerControlPermanentSlotAppArmor = `
# Description: Allow operating as the UPower service.

network netlink,
network netlink raw,

# DBus accesses
#include <abstractions/dbus-strict>

dbus (send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member={Request,Release}Name
   peer=(name=org.freedesktop.DBus),

dbus (receive, send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member=GetConnectionUnixProcessID
   peer=(label=unconfined),

dbus (receive, send)
   bus=system
   path=/org/freedesktop/DBus
   interface=org.freedesktop.DBus
   member=GetConnectionUnixUser
   peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.freedesktop.UPower",

# Allow traffic to/from our path and interface with any method
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UPower{,/**}
    interface=org.freedesktop.UPower*,

# Allow traffic to/from org.freedesktop.DBus for the UPower service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UPower{,/**}
    interface=org.freedesktop.DBus.*,

dbus (receive, send)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=unconfined),

# Allow access to logind service as we need to query it for possible
# power states and trigger these when the battery gets low and the
# system enters a critical state.
dbus (send)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.login1.Manager
    member={CanPowerOff,CanSuspend,CanHibernate,CanHybridSleep,PowerOff,Suspend,Hibernate,HybrisSleep},
    peer=(label=unconfined),
`

const upowerControlPermanentSlotSeccomp = `
bind
getsockname
recvmsg
sendmsg
sendto
recvfrom
`

const upowerControlPermanentSlotDBus = `
<!-- DBus policy for upower (upstream version 0.99.4) -->
<policy user="root">
  <allow own="org.freedesktop.UPower"/>
</policy>
<policy context="default">

  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.DBus.Introspectable"/>

  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.DBus.Peer"/>
  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.DBus.Properties"/>
  <allow send_destination="org.freedesktop.UPower.Device"
         send_interface="org.freedesktop.DBus.Properties"/>
  <allow send_destination="org.freedesktop.UPower.KbdBacklight"
         send_interface="org.freedesktop.DBus.Properties"/>
  <allow send_destination="org.freedesktop.UPower.Wakeups"
         send_interface="org.freedesktop.DBus.Properties"/>

  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.UPower"/>
  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.UPower.Device"/>
  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.UPower.KbdBacklight"/>
  <allow send_destination="org.freedesktop.UPower"
         send_interface="org.freedesktop.UPower.Wakeups"/>
</policy>
`

const upowerControlConnectedPlugAppArmor = `
#include <abstractions/dbus-strict>

# Allow all access to UPower service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UPower{,/**}
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const upowerControlConnectedPlugSecComp = `
getsockname
recvmsg
sendmsg
sendto
recvfrom
`

type UpowerControlInterface struct{}

func (iface *UpowerControlInterface) Name() string {
	return "upower-control"
}

func (iface *UpowerControlInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *UpowerControlInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		var new []byte
		if release.OnClassic {
			// Let confined apps access unconfined upower on classic
			new = []byte("unconfined")
		} else {
			new = slotAppLabelExpr(slot)
		}
		snippet := bytes.Replace([]byte(upowerControlConnectedPlugAppArmor), old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return []byte(upowerControlConnectedPlugSecComp), nil
	}
	return nil, nil
}

func (iface *UpowerControlInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus:
		return []byte(upowerControlPermanentSlotDBus), nil
	case interfaces.SecurityAppArmor:
		return []byte(upowerControlPermanentSlotAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(upowerControlPermanentSlotSeccomp), nil
	}
	return nil, nil
}

func (iface *UpowerControlInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *UpowerControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *UpowerControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *UpowerControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
