// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const packageKitControlSummary = `allows control of the PackageKit service`

const packageKitControlBaseDeclarationPlugs = `
  packagekit-control:
    allow-installation: false
    deny-auto-connection: true
`

const packageKitControlBaseDeclarationSlots = `
  packagekit-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const packageKitControlConnectedPlugAppArmor = `
# Description: Allow access to PackageKit service which gives
# privileged access to native package management on the system

#include <abstractions/dbus-strict>

# Allow communication with the main PackageKit end point.
dbus (receive, send)
        bus=system
        path=/org/freedesktop/PackageKit
        interface=org.freedesktop.PackageKit
        peer=(label=###SLOT_SECURITY_TAGS###),
dbus (receive, send)
        bus=system
        path=/org/freedesktop/PackageKit
        interface=org.freedesktop.PackageKit.Offline
        peer=(label=###SLOT_SECURITY_TAGS###),

# Read all properties from PackageKit
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
        bus=system
        path=/org/freedesktop/PackageKit
        interface=org.freedesktop.DBus.Properties
        member=Get{,All},
dbus (receive)
        bus=system
        path=/org/freedesktop/PackageKit
        interface=org.freedesktop.DBus.Properties
        member=PropertiesChanged
        peer=(label=###SLOT_SECURITY_TAGS###),

# Allow clients to introspect the service
# do not use peer=(label=unconfined) here since this is DBus activated
dbus (send)
	bus=system
	path=/org/freedesktop/PackageKit
	interface=org.freedesktop.DBus.Introspectable
	member=Introspect,

# Allow communication with PackageKit transactions.  Transactions are
# exported with random object paths that currently take the form
# "/{number}_{hexstring}".  If PackageKit (or a reimplementation of
# packagekitd) changes this, then these rules will need to change too.
dbus (receive, send)
        bus=system
        path=/[0-9]*_[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]
        interface=org.freedesktop.PackageKit.Transaction
        peer=(label=unconfined),
dbus (send)
        bus=system
        path=/[0-9]*_[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]
        interface=org.freedesktop.DBus.Properties
        member=Get{,All}
        peer=(label=unconfined),
dbus (receive)
        bus=system
        path=/[0-9]*_[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]
        interface=org.freedesktop.DBus.Properties
        member=PropertiesChanged
        peer=(label=unconfined),
dbus (send)
	bus=system
        path=/[0-9]*_[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]
	interface=org.freedesktop.DBus.Introspectable
	member=Introspect
	peer=(label=unconfined),
`

type packageKitInterface struct{}

func (iface *packageKitInterface) Name() string {
	return "packagekit-control"
}

func (iface *packageKitInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              packageKitControlSummary,
		ImplicitOnCore:       false, // Maybe... osutil.IsExecutable("/usr/libexec/packagekitd"), ?
		ImplicitOnClassic:    true,
		BaseDeclarationPlugs: packageKitControlBaseDeclarationPlugs,
		BaseDeclarationSlots: packageKitControlBaseDeclarationSlots,
	}
}

func (iface *packageKitInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	if implicitSystemConnectedSlot(slot) {
		// Let confined apps access unconfined packagekit on classic
		new = "unconfined"
	}
	snippet := strings.Replace(packageKitControlConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *packageKitInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&packageKitInterface{})
}
