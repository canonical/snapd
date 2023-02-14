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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

// The pipewire-server interface is the companion interface to the audio-record
// interface. The design of this interface is based on the idea that the slot
// implementation (eg pulseaudio) is expected to query snapd on if the
// audio-record slot is connected or not and the audio service will mediate
// recording (ie, the rules below allow connecting to the audio service, but do
// not implement enforcement rules; it is up to the audio service to provide
// enforcement). If other audio recording servers require different security
// policy for record and playback (eg, a different socket path), then those
// accesses will be added to this interface.

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
# Allow communicating with pulseaudio service

owner /{,var/}run/user/[0-9]*/ r,
owner /{,var/}run/user/[0-9]*/pipewire-0 rwk,
owner /{,var/}run/user/[0-9]*/pipewire-0.lock rwk,
`

const pipewireServerConnectedPlugSecComp = `
shmctl
`

const pipewireServerPermanentSlotAppArmor = `
# When running Pipewire in system mode it will switch to the at
# build time configured user/group on startup.
capability setuid,
capability setgid,

capability sys_nice,
capability sys_resource,

# For udev
network netlink raw,
/sys/devices/virtual/dmi/id/sys_vendor r,
/sys/devices/virtual/dmi/id/bios_vendor r,
/sys/**/sound/** r,


# Shared memory based communication with clients

owner /{,var/}run/user/[0-9]*/ r,
owner /{,var/}run/user/[0-9]*/pipewire-0 rwk,
owner /{,var/}run/user/[0-9]*/pipewire-0.lock rwk,
`

const pipewireServerPermanentSlotSecComp = `
# The following are needed for UNIX sockets
personality
setpriority
bind
listen
accept
accept4
shmctl
setgroups
setgroups32
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
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

func (iface *pipewireServerInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(pipewireServerConnectedPlugSecComp)
	return nil
}

func (iface *pipewireServerInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pipewireServerPermanentSlotSecComp)
	return nil
}

func (iface *pipewireServerInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&pipewireServerInterface{})
}
