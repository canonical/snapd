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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

var prioritizedSnippetDesktopFileAccess = apparmor.RegisterSnippetKey("desktop-content-access")

type desktopLaunchInterface struct{}

const desktopLaunchSummary = `allows snaps to identify and launch desktop applications in (or from) other snaps`

const desktopLaunchBaseDeclarationPlugs = `
  desktop-launch:
    allow-installation: false
    deny-auto-connection: true
`

const desktopLaunchBaseDeclarationSlots = `
  desktop-launch:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const desktopLaunchConnectedPlugAppArmor = `
# Description: Can identify and launch other snaps.

# Access to the desktop and icon files installed by snaps
/var/lib/snapd/desktop/applications/{,*} r,
/var/lib/snapd/desktop/icons/{,**} r,

# Allow access to all snap metadata
/snap/*/*/** r,

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/io/snapcraft/PrivilegedDesktopLauncher
    interface=io.snapcraft.PrivilegedDesktopLauncher
    member=OpenDesktopEntry
    peer=(label=unconfined),
`

func (iface *desktopLaunchInterface) Name() string {
	return "desktop-launch"
}

func (iface *desktopLaunchInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              desktopLaunchSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationPlugs: desktopLaunchBaseDeclarationPlugs,
		BaseDeclarationSlots: desktopLaunchBaseDeclarationSlots,
	}
}

func (iface *desktopLaunchInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(desktopLaunchConnectedPlugAppArmor)
	// the DesktopFileRules added by other, less privileged, interfaces (like unity7
	// or desktop-legacy) can conflict with the rules in this, more privileged,
	// interface, so they are added there with the minimum priority, while in this
	// one an empty string is added with a bigger privilege value, thus removing
	// those rules when desktop-launch plug is connected.
	spec.AddPrioritizedSnippet("", prioritizedSnippetDesktopFileAccess, 100)
	return nil
}

func (iface *desktopLaunchInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

// Only implicitOnClassic since userd isn't yet usable on core
func init() {
	registerIface(&desktopLaunchInterface{})
}
