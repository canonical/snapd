// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const pulseaudioSummary = `allows operating as or interacting with the pulseaudio service`

const pulseaudioBaseDeclarationSlots = `
  pulseaudio:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-connection:
      on-classic: false
`

const pulseaudioConnectedPlugAppArmor = `
# Allow communicating with pulseaudio service for playback and, on some
# distributions, recording.
/{run,dev}/shm/pulse-shm-* mrwk,

owner /{,var/}run/pulse/ r,
owner /{,var/}run/pulse/native rwk,
owner /run/user/[0-9]*/ r,
owner /run/user/[0-9]*/pulse/ rw,

/run/udev/data/c116:[0-9]* r,
/run/udev/data/+sound:card[0-9]* r,
`

const pulseaudioConnectedPlugAppArmorDesktop = `
# Only on desktop do we need access to /etc/pulse for any PulseAudio client
# to read available client side configuration settings. On an Ubuntu Core
# device those things will be stored inside the snap directory.
/etc/pulse/ r,
/etc/pulse/* r,
owner @{HOME}/.pulse-cookie rk,
owner @{HOME}/.config/pulse/cookie rk,
owner /{,var/}run/user/*/pulse/ rwk,
owner /{,var/}run/user/*/pulse/native rwk,
`

const pulseaudioConnectedPlugSecComp = `
shmctl
`

const pulseaudioPermanentSlotAppArmor = `
# When running PulseAudio in system mode it will switch to the at
# build time configured user/group on startup.
capability setuid,
capability setgid,

capability sys_nice,
capability sys_resource,

owner @{PROC}/@{pid}/exe r,
/etc/machine-id r,

# Audio related
@{PROC}/asound/devices r,
@{PROC}/asound/card** r,

# Should use the alsa interface instead
/dev/snd/pcm* rw,
/dev/snd/control* rw,
/dev/snd/timer r,

/sys/**/sound/** r,

# For udev
network netlink raw,
/sys/devices/virtual/dmi/id/sys_vendor r,
/sys/devices/virtual/dmi/id/bios_vendor r,
# FIXME: use udev queries to make this more specific
/run/udev/data/** r,

owner /{,var/}run/pulse/ rw,
owner /{,var/}run/pulse/** rwk,

# Shared memory based communication with clients
/{run,dev}/shm/pulse-shm-* mrwk,

/usr/share/applications/ r,

owner /run/pulse/native/ rwk,
owner /run/user/[0-9]*/ r,
owner /run/user/[0-9]*/pulse/ rw,
`

const pulseaudioPermanentSlotSecComp = `
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

type pulseAudioInterface struct{}

func (iface *pulseAudioInterface) Name() string {
	return "pulseaudio"
}

func (iface *pulseAudioInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              pulseaudioSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: pulseaudioBaseDeclarationSlots,
	}
}

func (iface *pulseAudioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(pulseaudioConnectedPlugAppArmor)
	if release.OnClassic {
		spec.AddSnippet(pulseaudioConnectedPlugAppArmorDesktop)
	}
	return nil
}

func (iface *pulseAudioInterface) UDevPermanentSlot(spec *udev.Specification, slot *snap.SlotInfo) error {
	spec.TagDevice(`KERNEL=="controlC[0-9]*"`)
	spec.TagDevice(`KERNEL=="pcmC[0-9]*D[0-9]*[cp]"`)
	spec.TagDevice(`KERNEL=="timer"`)
	return nil
}

func (iface *pulseAudioInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pulseaudioPermanentSlotAppArmor)
	return nil
}

func (iface *pulseAudioInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(pulseaudioConnectedPlugSecComp)
	return nil
}

func (iface *pulseAudioInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pulseaudioPermanentSlotSecComp)
	return nil
}

func (iface *pulseAudioInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&pulseAudioInterface{})
}
