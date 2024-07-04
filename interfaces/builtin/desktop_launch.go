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

// The desktop-legacy and unity7 interfaces denies access to the contents of the .desktop files
// located at /var/lib/snapd/desktop/applications; only the list of files is readable.
// The desktop-launch interface, instead, gives access to those files contents. But since, in
// AppArmor, a deny access is "stronger", if the desktop-legacy or the unity7 interfaces are
// connected, the access to those contents will be forbidden, no matter what the desktop-launch
// interface adds to the AppArmor configuration.
//
// To fix this, a prioritized block is defined with `prioritizedSnippetDesktopFileAccess`, where
// the AppArmor code inserted by desktop-legacy and unity7 to forbid access to those files is
// given a lower priority than the code inserted by desktop-launch to allow it. This way, if
// only the desktop-legacy or the unity7 interfaces are connected, everything will work as
// before (no access to the contents of the .desktop files), but if the desktop-launch interface
// is connected too (which, let's remind, it's a very privileged interface), then access to those
// .desktop files is granted, because the code from desktop-legacy and unity7 is removed.

const desktopLegacyAndUnity7Priority = 0
const desktopLaunchPriority = 100

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
	spec.AddPrioritizedSnippet("", prioritizedSnippetDesktopFileAccess, desktopLaunchPriority)
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
