// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

// The pipewire-server interface is designed to allow a snap to get full
// access to the pipewire socket. This is useful in these cases:
//
// * when snapping pipewire itself, to allow it to offer its socket
// * when snapping xdg-desktop-portal, to allow it to offer pipewire connections through portals
// * when snapping Gnome Shell, to allow it to share the screen
//
// this interface is NOT needed in any other cases, because for that cases the
// current audio-playback, audio-record interfaces and the portals offered by
// xdg-desktop-portal are enough.
//
// This interface only adds the bare minimum for pipewire over audio-playback and
// audio-record interfaces, so both must be also set and plugged to have full access.
// For example, this interface doesn't give access to .../pulse/native socket, because
// it is already available in the audio-playback interface. It makes no sense to
// duplicate everything here, because it just makes more complex to fix any bug.

const pipewireServerSummary = `allows full access to the pipewire socket (don't needed for normal apps)`

const pipewireServerBaseDeclarationSlots = `
  pipewire-server:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
    deny-auto-connection: true
`

const pipewireServerConnectedPlugAppArmor = `
# Allow communicating with pipewire service

owner /{,var/}run/user/[0-9]*/pipewire-0 rwk,
owner /{,var/}run/user/[0-9]*/pipewire-0.lock rwk,
owner /{,var/}run/user/[0-9]*/pulse/pid rwk,
`

const pipewireServerPermanentSlotAppArmor = `
owner /{,var/}run/user/[0-9]*/ r,
owner /{,var/}run/user/[0-9]*/pipewire-0 rwk,
owner /{,var/}run/user/[0-9]*/pipewire-0.lock rwk,
owner /{,var/}run/user/[0-9]*/pulse/pid rwk,
`

type pipewireServerInterface struct{}

func (iface *pipewireServerInterface) Name() string {
	return "pipewire-server"
}

func (iface *pipewireServerInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              pipewireServerSummary,
		ImplicitOnClassic:    true,
		ImplicitOnCore:       true,
		BaseDeclarationSlots: pipewireServerBaseDeclarationSlots,
	}
}

func (iface *pipewireServerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(pipewireServerConnectedPlugAppArmor)
	return nil
}

func (iface *pipewireServerInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TagDevice(`KERNEL=="timer"`)
	return nil
}

func (iface *pipewireServerInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pipewireServerPermanentSlotAppArmor)
	return nil
}

func (iface *pipewireServerInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&pipewireServerInterface{})
}
