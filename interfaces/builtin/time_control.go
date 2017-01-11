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
)

const timeControlConnectedPlugAppArmor = `
# Description: Can set time and date via systemd' timedated D-Bus interface.
# Can read all properties of /org/freedesktop/timedate1 D-Bus object; see
# https://www.freedesktop.org/wiki/Software/systemd/timedated/; This also
# gives full access to the RTC device nodes and relevant parts of sysfs.
# Usage: reserved

#include <abstractions/dbus-strict>

# Introspection of org.freedesktop.timedate1
dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.timedate1
    member="Set{Time,LocalRTC}"
    peer=(label=unconfined),

# Read all properties from timedate1
dbus (send)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# Receive timedate1 property changed events
dbus (receive)
    bus=system
    path=/org/freedesktop/timedate1
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=unconfined),

# Allow write access to system real-time clock
# See 'man 4 rtc' for details.

capability sys_time,

/dev/rtc[0-9]* rw,

# Access to the sysfs nodes are needed by rtcwake for example
# to program scheduled wakeups in the future.
/sys/class/rtc/*/ rw,
/sys/class/rtc/*/** rw,

# As the core snap ships the hwclock utility we can also allow
# clients to use it now that they have access to the relevant
# device nodes.
/sbin/hwclock ixr,
`
const timeControlConnectedPlugSecComp = `
# dbus
connect
getsockname
recvmsg
recvfrom
send
sendto
sendmsg
socket
`

// The type for the rtc interface
type TimeControlInterface struct{}

// Getter for the name of the rtc interface
func (iface *TimeControlInterface) Name() string {
	return "time-control"
}

func (iface *TimeControlInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *TimeControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget or os snap
	if !(slot.Snap.Type == "os") {
		return fmt.Errorf("%s slots are reserved for the operating system snap", iface.Name())
	}
	return nil
}

// Checks and possibly modifies a plug
func (iface *TimeControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

// Returns snippet granted on install
func (iface *TimeControlInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// Getter for the security snippet specific to the plug
func (iface *TimeControlInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(timeControlConnectedPlugAppArmor), nil

	case interfaces.SecuritySecComp:
		return []byte(timeControlConnectedPlugSecComp), nil

	case interfaces.SecurityUDev:
		var tagSnippet bytes.Buffer
		const udevRule = `KERNEL=="/dev/rtc0", TAG+="%s"`
		for appName := range plug.Apps {
			tag := udevSnapSecurityName(plug.Snap.Name(), appName)
			tagSnippet.WriteString(fmt.Sprintf(udevRule, tag))
		}
		return tagSnippet.Bytes(), nil
	}
	return nil, nil
}

// No extra permissions granted on connection
func (iface *TimeControlInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// No permissions granted to plug permanently
func (iface *TimeControlInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *TimeControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}
