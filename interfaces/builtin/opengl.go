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

const openglSummary = `allows access to OpenGL stack`

const openglBaseDeclarationSlots = `
  opengl:
    allow-installation:
      slot-snap-type:
        - core
`

const openglConnectedPlugAppArmor = `
# Description: Can access opengl.

# specific gl libs
/var/lib/snapd/lib/gl{,32}/ r,
/var/lib/snapd/lib/gl{,32}/** rm,

# Bi-arch distribution nvidia support
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcuda*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvidia*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}tls/libnvidia*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvcuvid.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}lib{GL,GLESv1_CM,GLESv2,EGL}*nvidia.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libGLdispatch.so{,.*} rm,

# Support reading the Vulkan ICD files
/var/lib/snapd/lib/vulkan/ r,
/var/lib/snapd/lib/vulkan/** r,
/var/lib/snapd/hostfs/usr/share/vulkan/icd.d/*nvidia*.json r,

# Support reading the GLVND EGL vendor files
/var/lib/snapd/lib/glvnd/ r,
/var/lib/snapd/lib/glvnd/** r,
/var/lib/snapd/hostfs/usr/share/glvnd/egl_vendor.d/*nvidia*.json r,

# Main bi-arch GL libraries
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}{,nvidia*/}lib{GL,GLU,GLESv1_CM,GLESv2,EGL,GLX}.so{,.*} rm,

/dev/dri/ r,
/dev/dri/card0 rw,

# nvidia
/etc/vdpau_wrapper.cfg r,
@{PROC}/driver/nvidia/params r,
@{PROC}/modules r,
/dev/nvidia* rw,
unix (send, receive) type=dgram peer=(addr="@nvidia[0-9a-f]*"),

# eglfs
/dev/vchiq rw,

# cuda
@{PROC}/sys/vm/mmap_min_addr r,
@{PROC}/devices r,
/sys/devices/system/memory/block_size_bytes r,
unix (bind,listen) type=seqpacket addr="@cuda-uvmfd-[0-9a-f]*",
/{dev,run}/shm/cuda.* rw,


# Parallels guest tools 3D acceleration (video toolgate)
@{PROC}/driver/prl_vtg rw,

# /sys/devices
/sys/devices/pci[0-9a-f]*/**/config r,
/sys/devices/pci[0-9a-f]*/**/revision r,
/sys/devices/pci[0-9a-f]*/**/{,subsystem_}device r,
/sys/devices/pci[0-9a-f]*/**/{,subsystem_}vendor r,
/sys/devices/**/drm{,_dp_aux_dev}/** r,

# FIXME: this is an information leak and snapd should instead query udev for
# the specific accesses associated with the above devices.
/sys/bus/pci/devices/ r,
/sys/bus/platform/devices/soc:gpu/ r,
/run/udev/data/+drm:card* r,
/run/udev/data/+pci:[0-9a-f]* r,
/run/udev/data/+platform:soc:gpu* r,

# FIXME: for each device in /dev that this policy references, lookup the
# device type, major and minor and create rules of this form:
# /run/udev/data/<type><major>:<minor> r,
# For now, allow 'c'haracter devices and 'b'lock devices based on
# https://www.kernel.org/doc/Documentation/devices.txt
/run/udev/data/c226:[0-9]* r,  # 226 drm
`

// The nvidia modules don't use sysfs (therefore they can't be udev tagged) and
// will be added by snap-confine.
var openglConnectedPlugUDev = []string{
	`SUBSYSTEM=="drm", KERNEL=="card[0-9]*"`,
	`KERNEL=="vchiq"`,
}

const openglConnectedPlugSecComp = `
# Description: allow running operations on GPU devices
# necessary as cuda tries to create /dev/nvidia-uvm-tools with mknod
mknod - |S_IFCHR
`

func init() {
	registerIface(&commonInterface{
		name:                  "opengl",
		summary:               openglSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  openglBaseDeclarationSlots,
		connectedPlugAppArmor: openglConnectedPlugAppArmor,
		connectedPlugSecComp:  openglConnectedPlugSecComp,
		connectedPlugUDev:     openglConnectedPlugUDev,
		reservedForOS:         true,
	})
}
