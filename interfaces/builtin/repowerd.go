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

var repowerdPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the repowerd service. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

# HW alarms
/dev/alarm rw,

# Suspend
/sys/power/** rw,

# Backlight
/sys/**/brightness rw,
/sys/**/max_brightness r,
/sys/**/type r,

# DBus
#include <abstractions/dbus-strict>

dbus (send)
  bus=system
  path=/org/freedesktop/DBus
  interface=org.freedesktop.DBus
  member={GetId,RequestName,ReleaseName,GetConnectionUnixProcessID}
  peer=(name=org.freedesktop.DBus),

dbus (receive, send)
  bus=system
  interface=org.freedesktop.DBus.{Properties,Introspectable},

dbus (bind)
  bus=system
  name=com.canonical.powerd,

dbus (bind)
  bus=system
  name=com.canonical.Unity.Screen,

dbus (receive, send)
  bus=system
  interface=com.canonical.powerd,

dbus (receive, send)
  bus=system
  interface=com.canonical.Unity.Screen,

# logind session
dbus (send)
  bus=system
  path=/org/freedesktop/login1
  interface=org.freedesktop.login1.Manager
  member=GetSessionByPID
  peer=(name=org.freedesktop.login1),
dbus (send)
  bus=system
  path=/org/freedesktop/login1{,/seat/**,/session/**}
  interface=org.freedesktop.DBus.Properties
  member=Get{,All}
  peer=(name=org.freedesktop.login1),
dbus (receive)
  bus=system
  path=/org/freedesktop/login1
  interface=org.freedesktop.login1.Manager
  member={SessionAdded,SessionRemoved}
  peer=(name=org.freedesktop.login1),
dbus (receive)
  bus=system
  path=/org/freedesktop/login1{,/seat/**,/session/**}
  interface=org.freedesktop.DBus.Properties
  member=PropertiesChanged
  peer=(name=org.freedesktop.login1),

# logind power
dbus (send)
  bus=system
  path=/org/freedesktop/login1
  interface=org.freedesktop.login1.Manager
  member={Hibernate,HybridSleep,Inhibit,PowerOff,Suspend}
  peer=(name=org.freedesktop.login1),

# Unity.Display
dbus (send)
  bus=system
  interface=com.canonical.Unity.Display
  path=/com/canonical/Unity/Display
  member={TurnOn,TurnOff}
  peer=(name=com.canonical.Unity.Display),

# Unity.UserActivity
dbus (receive)
  bus=system
  interface=com.canonical.Unity.UserActivity
  path=/com/canonical/Unity/UserActivity
  member=Activity,

# Unity.PowerButton
dbus (receive)
  bus=system
  interface=com.canonical.Unity.PowerButton
  path=/com/canonical/Unity/PowerButton
  member={Press,Release},
dbus (send)
  bus=system
  interface=com.canonical.Unity.PowerButton
  path=/com/canonical/Unity/PowerButton
  member=LongPress,
`)

var repowerdConnectedPlugAppArmor = []byte(`
# Description: Allow using repowerd service. Reserved because this gives
#  privileged access to the repowerd service.
# Usage: reserved

#include <abstractions/dbus-strict>

dbus (receive, send)
  bus=system
  path=/com/canonical/powerd
  peer=(label=###SLOT_SECURITY_TAGS###),

dbus (receive, send)
  bus=system
  path=/com/canonical/Unity/Screen
  peer=(label=###SLOT_SECURITY_TAGS###),
`)

var repowerdConnectedPlugAppArmorClassic = []byte(`
# Allow access to the unconfined repowerd services on classic.
dbus (receive, send)
  bus=system
  path=/com/canonical/powerd
  peer=(label=unconfined),

dbus (receive, send)
  bus=system
  path=/com/canonical/Unity/Screen
  peer=(label=unconfined),
`)

var repowerdPermanentSlotSecComp = []byte(`
# Description: Allow operating as the repowerd service. Reserved because this
# gives
#  privileged access to the system.
# Usage: reserved

# Can communicate with DBus system service
sendmsg
sendto
recvfrom
recvmsg
`)

var repowerdConnectedPlugSecComp = []byte(`
# Description: Allow using repowerd service. Reserved because this gives
#  privileged access to the repowerd service.
# Usage: reserved

# Can communicate with DBus system service
sendmsg
sendto
recvfrom
recvmsg
`)

var repowerdPermanentSlotDBus = []byte(`
<!-- DBus policy for repowerd (upstream version 2016.12) -->
<policy user="root">
  <allow own="com.canonical.Unity.Screen"/>
  <allow send_destination="com.canonical.Unity.Screen"
         send_interface="com.canonical.Unity.Screen"/>

  <allow own="com.canonical.powerd"/>
  <allow send_destination="com.canonical.powerd"
         send_interface="com.canonical.powerd"/>
</policy>

<policy context="default">
  <allow send_destination="com.canonical.Unity.Screen"
         send_interface="org.freedesktop.DBus.Introspectable" />
  <allow send_destination="com.canonical.Unity.Screen"
         send_interface="org.freedesktop.DBus.Properties"
         send_type="method_call" send_member="Get" />
  <allow send_destination="com.canonical.Unity.Screen"
         send_interface="org.freedesktop.DBus.Properties"
         send_type="method_call" send_member="GetAll" />

  <allow send_destination="com.canonical.powerd"
         send_interface="org.freedesktop.DBus.Introspectable" />
  <allow send_destination="com.canonical.powerd"
         send_interface="org.freedesktop.DBus.Properties"
         send_type="method_call" send_member="Get" />
  <allow send_destination="com.canonical.powerd"
         send_interface="org.freedesktop.DBus.Properties"
         send_type="method_call" send_member="GetAll" />

  <allow send_destination="com.canonical.powerd"
         send_interface="com.canonical.powerd"
         send_type="method_call" send_member="userAutobrightnessEnable" />
  <allow send_destination="com.canonical.powerd"
         send_interface="com.canonical.powerd"
         send_type="method_call" send_member="getBrightnessParams" />
  <allow send_destination="com.canonical.powerd"
         send_interface="com.canonical.powerd"
         send_type="method_call" send_member="setUserBrightness" />
  <allow send_destination="com.canonical.powerd"
         send_interface="com.canonical.powerd"
         send_type="method_call" send_member="userAutobrightnessEnable" />
</policy>

<!-- Allow any user at console (adb shell) to send commands this is
     needed for unlocking the screen in tests (LP: 1298869) -->
<policy at_console="true">
  <allow send_destination="com.canonical.Unity.Screen"/>
  <allow send_destination="com.canonical.powerd"/>
</policy>
`)

type RepowerdInterface struct{}

func (iface *RepowerdInterface) Name() string {
	return "repowerd"
}

func (iface *RepowerdInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *RepowerdInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace([]byte(repowerdConnectedPlugAppArmor), old, new, -1)
		if release.OnClassic {
			// Let confined apps access unconfined repowerd on classic
			snippet = append(snippet, repowerdConnectedPlugAppArmorClassic...)
		}
		return snippet, nil
	case interfaces.SecuritySecComp:
		return repowerdConnectedPlugSecComp, nil
	}
	return nil, nil
}

func (iface *RepowerdInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return repowerdPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return repowerdPermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return repowerdPermanentSlotDBus, nil
	}
	return nil, nil
}

func (iface *RepowerdInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *RepowerdInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *RepowerdInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *RepowerdInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
