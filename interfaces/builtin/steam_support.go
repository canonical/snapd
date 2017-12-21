// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

const steamSupportSummary = `allows access to various permissions required to use Steam`

const steamSupportBaseDeclarationSlots = `
  steam-support:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const steamSupportConnectedPlugSecComp = `
# Description: Allow Steam games to work correctly.
# This extends the browser-support base seccomp. They cannot be used
# together as browser-support will deny ptrace capabilities.

# This is required for Feral Interactive ports
ptrace

# Required by titles such as Dying Light
sched_setaffinity
`

const steamSupportConnectedPlugAppArmor = `
# Description: Provide support for the Steam Client and Steam Games to run
# correctly under full AppArmor confinement. Primary use case is to leverage
# solus-runtime-gaming and linux-steam-integration to provide a sane and safe
# Steam experience across all platforms, with appropriate mediation.
# This AppArmor definition is used in conjunction with the browser-support
# base AppArmor definition. They cannot both be used together as defined
# connections however, as browser-support will deny the ptrace capabilities
# that are required by Steam.

########################
# Steam Client / API   #
########################

# Allow Valve IPC to work for the multiprocess client architecture.
# "Server" component will write/create/lock, clients (i.e. running games in
# the Steam API) will mmap the object.
owner /{dev,run}/shm/u*-ValveIPCSharedObj* mrwk,

# Linux Steam Integration config mask system
owner /{dev,run}/shm/u*-LinuxSteamIntegration.unity3d.* rw,

# i.e. /dev/shm/#539043
owner /{dev,run}/shm/#* mrw,

# Further required pipe devices (i.e. /dev/shm/u1000-Shm_125376fc)
owner /{dev,run}/shm/u*-Shm_* mrw,

# Steam Web Store IPC (i.e. /dev/shm/ValveIPCSHM_1000)
owner /{dev,run}/shm/ValveIPCSHM_* rw,

# For old Source games like L4D2
owner /{dev,run}/shm/{,.}org.chromium.Chromium.shmem.libcef_* mrwk,


########################
#    Audio Support     #
########################

# Base runtime can provide HRTF/OpenAL implementation for games
/usr/share/openal/hrtf/ r,
/usr/share/openal/hrtf/*.mhr mr,
/usr/share/openal/presets/ r,
/usr/share/openal/hrtf/*.{ambdec,txt} mr,

# solus-runtime-gaming uses a stateless alsa configuration which will
# always use PulseAudio if possible.
/usr/share/alsa/alsa.conf r,
/usr/share/defaults/alsa/asound.conf r,
/usr/share/alsa/alsa.conf.d/ r,
/usr/share/alsa/alsa.conf.d/** r,
/usr/share/alsa/cards/** r,
/usr/share/alsa/pcm/** r,

# PCM Access
/sys/devices/pci**/sound/**/pcm**/pcm_class r,

########################
#  Mono Game Support   #
########################

# i.e. /dev/shm/mono-shared-1000-shared_data-ironhide-Linux-x86_64-328-12-0
owner /{dev,run}/shm/mono-shared-*-shared_{data,fileshare}-* mrw,

# i.e. /dev/shm/mono.16965
owner /{dev,run}/shm/mono.@{pid} mrw,


########################
#     GPU Access       #
########################

# Games can and will attempt to map /dev/nvidia* (specifically Unity)
/dev/nvidiactl mr,
/dev/nvidia[0-9] mr,

########################
#    Vulkan Support    #
########################

# Base runtime can contain the vulkan loader itself
/usr/share/vulkan/explicit_layer.d/ r,
/usr/share/vulkan/explicit_layer.d/** r,

# Base runtime can contain mesalib definitions for Vulkan ICD files
/usr/share/vulkan/icd.d/ r,
/usr/share/vulkan/icd.d/** r,

########################
#      Disks/Media     #
########################

# Steam requires +x permissions on the partitions and will perform
# such a test:
# sh: /run/media/bigdisk/games//steamapps/.steam_exec_test.sh: /bin/sh: bad interpreter: Permission denied
/run/media/**/.steam_exec_test.sh ixmrw,

# Libraries and executables on other partitions require map + execute permissions
/run/media/**/steamapps/common/** ixm,

########################
# Udev / PCI Support   #
########################

# Steam Controller
/run/udev/data/c247:[0-9]* r,
/run/udev/data/c248:[0-9]* r,

# Allow thread-synchronisation to work in the Steam client
/sys/devices/pci[0-9]*/**/{class,enable,irq,resource*} r,

# Steam (and many games) will attempt to use lspci to learn about GPUs
# and potentially apply workarounds on start if they detect NVIDIA.
/{,usr}/bin/lspci ixr,
/etc/udev/hwdb.bin r,  # Need udev data to identify GPUs

/usr/share/hwdata/{pci,usb}.ids r, # Need hwdata

# USB device access
/sys/devices/pci[0-9]*/**/iad_bFunction{Sub,}Class r,
/sys/devices/pci[0-9]*/**/bInterface{Sub,}Class r,
/sys/devices/pci[0-9]*/**/bDevice{Sub,}Class r,

# General device information access
/sys/devices/pci[0-9]*/**/{idVendor,idProduct,manufacturer,product,serial} r,

########################
#    Feral Titles      #
########################

# Feral uses ptrace (one assumes for crash report service)
capability sys_ptrace,

# Allow confined ptrace now.
ptrace (trace, read) peer=snap.@{SNAP_NAME}.**,

# The Feral launcher script tries using curl-config, let it.
/usr/bin/curl-config ixr,

########################
# AppArmor Limitations #
########################

# Games Dev Tycoon reads /proc/$ppid/environ but unfortunately AppArmor
# doesn't have any such qualifier.
@{PROC}/[0-9]*/environ r,
`

func init() {
	registerIface(&commonInterface{
		name:                  "steam-support",
		summary:               steamSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  steamSupportBaseDeclarationSlots,
		connectedPlugAppArmor: browserSupportConnectedPlugAppArmor + steamSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  browserSupportConnectedPlugSecComp + steamSupportConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
