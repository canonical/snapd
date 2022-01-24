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
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvoptix*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}tls/libnvidia*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvcuvid.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}lib{GL,GLESv1_CM,GLESv2,EGL}*nvidia.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libGLdispatch.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}vdpau/libvdpau_nvidia.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnv{rm,dc,imp,os}*.so{,.*} rm,
# CUDA libs
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnpp{c,ig,ial,icc,idei,ist,if,im,itc}*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcublas{,Lt}*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcufft.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcusolver.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcuparse.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcurand.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libcudnn{,_adv_infer,_adv_train,_cnn_infer,_cnn_train,_ops_infer,_ops_train}*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvrtc{,-builtins}*.so{,.*} rm,
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libnvToolsExt.so{,.*} rm,

# Support reading the Vulkan ICD files
/var/lib/snapd/lib/vulkan/ r,
/var/lib/snapd/lib/vulkan/** r,
/var/lib/snapd/hostfs/usr/share/vulkan/icd.d/*nvidia*.json r,

# Support reading the GLVND EGL vendor files
/var/lib/snapd/lib/glvnd/ r,
/var/lib/snapd/lib/glvnd/** r,
/var/lib/snapd/hostfs/usr/share/glvnd/egl_vendor.d/ r,
/var/lib/snapd/hostfs/usr/share/glvnd/egl_vendor.d/*nvidia*.json r,

# Support Nvidia EGL external platform
/var/lib/snapd/hostfs/usr/share/egl/egl_external_platform.d/ r,
/var/lib/snapd/hostfs/usr/share/egl/egl_external_platform.d/*nvidia*.json r,

# Main bi-arch GL libraries
/var/lib/snapd/hostfs/{,usr/}lib{,32,64,x32}/{,@{multiarch}/}{,nvidia*/}lib{OpenGL,GL,GLU,GLESv1_CM,GLESv2,EGL,GLX}.so{,.*} rm,

# Allow access to all cards since a) this is common on hybrid systems, b) ARM
# devices commonly have two devices (such as on the Raspberry Pi 4, one for KMS
# and another that does not) and c) there is nothing saying that /dev/dri/card0
# is the default card or the application is currently using.
/dev/dri/ r,
/dev/dri/card[0-9]* rw,

# nvidia
/etc/vdpau_wrapper.cfg r,
@{PROC}/driver/nvidia/params r,
@{PROC}/modules r,
/dev/nvidia* rw,
unix (send, receive) type=dgram peer=(addr="@nvidia[0-9a-f]*"),

# VideoCore/EGL (shared device with VideoCore camera)
/dev/vchiq rw,
# VideoCore Video decoding (required for accelerated MMAL video playback)
/dev/vcsm-cma rw,

# va-api
/dev/dri/renderD[0-9]* rw,

# cuda
@{PROC}/sys/vm/mmap_min_addr r,
@{PROC}/devices r,
/sys/devices/system/memory/block_size_bytes r,
/sys/module/tegra_fuse/parameters/tegra_* r,
unix (bind,listen) type=seqpacket addr="@cuda-uvmfd-[0-9a-f]*",
/{dev,run}/shm/cuda.* rw,
/dev/nvhost-* rw,
/dev/nvmap rw,

# Tegra display driver
/dev/tegra_dc_ctrl rw,
/dev/tegra_dc_[0-9]* rw,

# Xilinx zocl DRM driver
# https://github.com/Xilinx/XRT/tree/master/src/runtime_src/core/edge/drm
/sys/devices/platform/amba{,_pl@[0-9]*}/amba{,_pl@[0-9]*}:zyxclmm_drm/* r,

# Imagination PowerVR driver
/dev/pvr_sync rw,

# OpenCL ICD files
/etc/OpenCL/vendors/ r,
/etc/OpenCL/vendors/** r,

# Parallels guest tools 3D acceleration (video toolgate)
@{PROC}/driver/prl_vtg rw,

# /sys/devices
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/config r,
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/revision r,
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/boot_vga r,
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/{,subsystem_}class r,
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/{,subsystem_}device r,
/sys/devices/{,*pcie-controller/}pci[0-9a-f]*/**/{,subsystem_}vendor r,
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

# From https://bugs.launchpad.net/snapd/+bug/1862832
/run/nvidia-xdriver-* rw,
unix (send, receive) type=dgram peer=(addr="@var/run/nvidia-xdriver-*"),
`

// Some nvidia modules don't use sysfs (therefore they can't be udev tagged) and
// will be added by snap-confine.
var openglConnectedPlugUDev = []string{
	`SUBSYSTEM=="drm", KERNEL=="card[0-9]*"`,
	`KERNEL=="vchiq"`,
	`KERNEL=="vcsm-cma"`,
	`KERNEL=="renderD[0-9]*"`,
	`KERNEL=="nvhost-*"`,
	`KERNEL=="nvmap"`,
	`KERNEL=="tegra_dc_ctrl"`,
	`KERNEL=="tegra_dc_[0-9]*"`,
	`KERNEL=="pvr_sync"`,
}

func init() {
	registerIface(&commonInterface{
		name:                  "opengl",
		summary:               openglSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  openglBaseDeclarationSlots,
		connectedPlugAppArmor: openglConnectedPlugAppArmor,
		connectedPlugUDev:     openglConnectedPlugUDev,
	})
}
