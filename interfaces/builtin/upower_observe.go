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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const upowerObservePermanentSlotAppArmor = `
# Description: Allow operating as the UPower service.

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
   member="GetConnectionUnix{ProcessID,User}"
   peer=(label=unconfined),

# Allow binding the service to the requested connection name
dbus (bind)
    bus=system
    name="org.freedesktop.UPower",

# Allow read-only access to service properties
dbus (receive)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.DBus.Properties
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=unconfined),

# Allow receiving any signals from the logind service
dbus (receive)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.login1.*
    peer=(label=unconfined),

# Allow access to logind service as we need to query it for possible
# power states and trigger these when the battery gets low and the
# system enters a critical state.
dbus (send)
    bus=system
    path=/org/freedesktop/login1{,/**}
    interface=org.freedesktop.login1.Manager
    member={CanPowerOff,CanSuspend,CanHibernate,CanHybridSleep,PowerOff,Suspend,Hibernate,HybrisSleep}
    peer=(label=unconfined),
`

const upowerObserveConnectedSlotAppArmor = `
# Allow traffic to/from our path and interface with any method
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UPower{,/**}
    interface=org.freedesktop.UPower*
    peer=(label=###PLUG_SECURITY_TAGS###),

# Allow traffic to/from org.freedesktop.DBus for the UPower service
dbus (receive, send)
    bus=system
    path=/org/freedesktop/UPower{,/**}
    interface=org.freedesktop.DBus.*
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const upowerObservePermanentSlotSeccomp = `
# libudev
bind
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

const upowerObservePermanentSlotDBus = `
<!-- DBus policy for upower (based on upstream version 0.99.4) -->
<policy user="root">
  <allow own="org.freedesktop.UPower"/>
</policy>
<policy context="default">
  <deny own="org.freedesktop.UPower"/>

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

const upowerObserveConnectedPlugAppArmor = `
# Description: Can query UPower for power devices, history and statistics.

#include <abstractions/dbus-strict>

# Find all devices monitored by UPower
dbus (send)
    bus=system
    path=/org/freedesktop/UPower
    interface=org.freedesktop.UPower
    member=EnumerateDevices
    peer=(label=###SLOT_SECURITY_TAGS###),

# Read all properties from UPower and devices
dbus (send)
    bus=system
    path=/org/freedesktop/UPower{,/devices/**}
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/org/freedesktop/UPower/Wakeups
    interface=org.freedesktop.DBus.Properties
    member=Get{,All}
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/org/freedesktop/UPower
    interface=org.freedesktop.UPower
    member=GetCriticalAction
    peer=(label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/org/freedesktop/UPower/devices/**
    interface=org.freedesktop.UPower.Device
    member=GetHistory
    peer=(label=###SLOT_SECURITY_TAGS###),

# Receive property changed events
dbus (receive)
    bus=system
    path=/org/freedesktop/UPower{,/devices/**}
    interface=org.freedesktop.DBus.Properties
    member=PropertiesChanged
    peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service
dbus (send)
    bus=system
    interface=org.freedesktop.DBus.Introspectable
    path=/org/freedesktop/UPower
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type UpowerObserveInterface struct{}

func (iface *UpowerObserveInterface) Name() string {
	return "upower-observe"
}

func (iface *UpowerObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	if release.OnClassic {
		// Let confined apps access unconfined upower on classic
		new = "unconfined"
	}
	snippet := strings.Replace(upowerObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *UpowerObserveInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(upowerObservePermanentSlotAppArmor)
	return nil
}

func (iface *UpowerObserveInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(upowerObservePermanentSlotSeccomp)
	return nil
}

func (iface *UpowerObserveInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(upowerObservePermanentSlotDBus)
	return nil
}

func (iface *UpowerObserveInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	snippet := strings.Replace(upowerObserveConnectedSlotAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *UpowerObserveInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *UpowerObserveInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	if slot.Snap.Type != snap.TypeApp && slot.Snap.Type != snap.TypeOS {
		return fmt.Errorf("%s slots are reserved for the operating system or application snaps", iface.Name())
	}
	return nil
}

func (iface *UpowerObserveInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&UpowerObserveInterface{})
}
