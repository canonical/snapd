// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// The audio-playback interface is the companion interface to the audio-record
// interface. The design of this interface is based on the idea that the slot
// implementation (eg pulseaudio) is expected to query snapd on if the
// audio-record slot is connected or not and the audio service will mediate
// recording (ie, the rules below allow connecting to the audio service, but do
// not implement enforcement rules; it is up to the audio service to provide
// enforcement). If other audio recording servers require different security
// policy for record and playback (eg, a different socket path), then those
// accesses will be added to this interface.

const audioPlaybackSummary = `allows audio playback via supporting services`

const audioPlaybackBaseDeclarationSlots = `
  audio-playback:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
`

const audioPlaybackConnectedPlugAppArmor = `
# Allow communicating with pulseaudio service
/{run,dev}/shm/pulse-shm-* mrwk,

owner /{,var/}run/pulse/ r,
owner /{,var/}run/pulse/native rwk,
owner /{,var/}run/pulse/pid r,
owner /{,var/}run/user/[0-9]*/ r,
owner /{,var/}run/user/[0-9]*/pulse/ r,

/run/udev/data/c116:[0-9]* r,
/run/udev/data/+sound:card[0-9]* r,
`

const audioPlaybackConnectedPlugAppArmorDesktop = `
# Allow communicating with pulseaudio service on the desktop in classic distro.
# Only on desktop do we need access to /etc/pulse for any PulseAudio client
# to read available client side configuration settings. On an Ubuntu Core
# device those things will be stored inside the snap directory.
/etc/pulse/ r,
/etc/pulse/** r,
owner @{HOME}/.pulse-cookie rk,
owner @{HOME}/.config/pulse/cookie rk,
owner /{,var/}run/user/*/pulse/ r,
owner /{,var/}run/user/*/pulse/native rwk,
owner /{,var/}run/user/*/pulse/pid r,
`

const audioPlaybackConnectedPlugAppArmorCore = `
owner /run/user/[0-9]*/###SLOT_SECURITY_TAGS###/pulse/ r,
owner /run/user/[0-9]*/###SLOT_SECURITY_TAGS###/pulse/native rwk,
owner /run/user/[0-9]*/###SLOT_SECURITY_TAGS###/pulse/pid r,
`

const audioPlaybackConnectedPlugSecComp = `
shmctl
`

const audioPlaybackPermanentSlotAppArmor = `
# When running PulseAudio in system mode it will switch to the at
# build time configured user/group on startup.
capability setuid,
capability setgid,

capability sys_nice,
capability sys_resource,

owner @{PROC}/@{pid}/exe r,
/etc/machine-id r,

# For udev
network netlink raw,
/sys/devices/virtual/dmi/id/sys_vendor r,
/sys/devices/virtual/dmi/id/bios_vendor r,
/sys/**/sound/** r,

owner /{,var/}run/pulse/ rw,
owner /{,var/}run/pulse/** rwk,

# Shared memory based communication with clients
/{run,dev}/shm/pulse-shm-* mrwk,

owner /run/user/[0-9]*/ r,
owner /run/user/[0-9]*/pulse/ rw,

# This allows to share screen in Core Desktop
owner /run/user/[0-9]*/pipewire-[0-9] rwk,

# This allows wireplumber to read the pulseaudio
# configuration if pipewire runs inside a container
/etc/pulse/ r,
/etc/pulse/** r,
`

const audioPlaybackPermanentSlotSecComp = `
# The following are needed for UNIX sockets
personality
setpriority
bind
listen
accept
accept4
shmctl
# Needed to set root as group for different state dirs
# pulseaudio creates on startup.
setgroups
setgroups32
# libudev
socket AF_NETLINK - NETLINK_KOBJECT_UEVENT
`

type audioPlaybackInterface struct{}

func (iface *audioPlaybackInterface) Name() string {
	return "audio-playback"
}

func (iface *audioPlaybackInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              audioPlaybackSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: audioPlaybackBaseDeclarationSlots,
	}
}

func (iface *audioPlaybackInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(audioPlaybackConnectedPlugAppArmor)
	if release.OnClassic {
		spec.AddSnippet(audioPlaybackConnectedPlugAppArmorDesktop)
	}
	if !implicitSystemConnectedSlot(slot) {
		old := "###SLOT_SECURITY_TAGS###"
		new := "snap." + slot.Snap().InstanceName() // forms the snap-instance-specific subdirectory name of /run/user/*/ used for XDG_RUNTIME_DIR
		snippet := strings.Replace(audioPlaybackConnectedPlugAppArmorCore, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *audioPlaybackInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TagDevice(`KERNEL=="controlC[0-9]*"`)
	spec.TagDevice(`KERNEL=="pcmC[0-9]*D[0-9]*[cp]"`)
	spec.TagDevice(`KERNEL=="timer"`)
	return nil
}

func (iface *audioPlaybackInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(audioPlaybackPermanentSlotAppArmor)
	return nil
}

func (iface *audioPlaybackInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(audioPlaybackConnectedPlugSecComp)
	return nil
}

func (iface *audioPlaybackInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(audioPlaybackPermanentSlotSecComp)
	return nil
}

func (iface *audioPlaybackInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&audioPlaybackInterface{})
}
