// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2023 Canonical Ltd
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

// Only allow raw disk devices; not ram, CDROM, generic SCSI, network,
// tape, raid, etc devices or disk partitions. For some devices, allow controller
// character devices since they are used to configure the corresponding block
// device.
const blockDevicesSummary = `allows access to disk block devices`

const blockDevicesBaseDeclarationPlugs = `
  block-devices:
    allow-installation: false
    deny-auto-connection: true
`

const blockDevicesBaseDeclarationSlots = `
  block-devices:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// https://www.kernel.org/doc/Documentation/admin-guide/devices.txt
// For now, only list common devices and skip the following:
// /dev/mfm{a,b} rw,                        # Acorn MFM
// /dev/ad[a-p] rw,                         # ACSI
// /dev/pd[a-d] rw,                         # Parallel port IDE
// /dev/pf[0-3] rw,                         # Parallel port ATAPI
// /dev/ub[a-z] rw,                         # USB block device
const blockDevicesConnectedPlugAppArmor = `
# Description: Allow write access to raw disk block devices.

@{PROC}/devices r,
/run/udev/data/b[0-9]*:[0-9]* r,
/sys/block/ r,
/sys/devices/**/block/** r,
/sys/dev/block/ r,
/sys/devices/platform/soc/**/mmc_host/** r,
# Allow reading major and minor numbers for block special files of NVMe namespaces.
/sys/devices/**/nvme/**/dev r,

# Access to raw devices, not individual partitions
/dev/hd[a-t] rwk,                                          # IDE, MFM, RLL
/dev/sd{,[a-h]}[a-z] rwk,                                  # SCSI
/dev/sdi[a-v] rwk,                                         # SCSI continued
/dev/i2o/hd{,[a-c]}[a-z] rwk,                              # I2O hard disk
/dev/i2o/hdd[a-x] rwk,                                     # I2O hard disk continued
/dev/mmcblk[0-9]{,[0-9],[0-9][0-9]} rwk,                   # MMC (up to 1000 devices)
/dev/vd[a-z] rwk,                                          # virtio
/dev/loop[0-9]{,[0-9],[0-9][0-9]} rwk,                     # loopback (up to 1000 devices)
/dev/loop-control rw,                                      # loopback control

# Allow /dev/nvmeXnY namespace block devices. Please note this grants access to all
# NVMe namespace block devices and that the numeric suffix on the character device
# does not necessarily correspond to a namespace block device with the same suffix
# From 'man nvme-format' : 
#   Note, the numeric suffix on the character device, for example the 0 in
#   /dev/nvme0, does NOT indicate this device handle is the parent controller
#   of any namespaces with the same suffix. The namespace handle's numeral may
#   be coming from the subsystem identifier, which is independent of the
#   controller's identifier. Do not assume any particular device relationship
#   based on their names. If you do, you may irrevocably erase data on an
#   unintended device.
/dev/nvme{[0-9],[1-9][0-9]}n{[1-9],[1-5][0-9],6[0-3]} rwk, # NVMe (up to 100 devices, with 1-63 namespaces)

# Allow /dev/nvmeX controller character devices. These character devices allow
# manipulation of the block devices that we also allow above, so grouping this
# access here makes sense, whereas access to individual partitions is delegated
# to the raw-volume interface.
/dev/nvme{[0-9],[1-9][0-9]} rwk,                           # NVMe (up to 100 devices)

# SCSI device commands, et al
capability sys_rawio,

# Perform various privileged block-device ioctl operations
capability sys_admin,

# Devices for various controllers used with ioctl()
/dev/mpt2ctl{,_wd} rw,
/dev/megaraid_sas_ioctl_node rw,

# Allow /sys/block/sdX/device/state to be accessible to accept or reject the request from given the path.
# To take the path offline will cause any subsequent access to fail immediately, vice versa.
/sys/devices/**/host*/**/state rw,

# Allow to use blkid to export key=value pairs such as UUID to get block device attributes
/{,usr/}sbin/blkid ixr,

# Allow to use mkfs utils to format partitions
/{,usr/}sbin/mke2fs ixr,
/{,usr/}sbin/mkfs.fat ixr,
`

var blockDevicesConnectedPlugUDev = []string{
	`SUBSYSTEM=="block"`,
	// these additional subsystems may not directly be block devices but they
	// allow for manipulation of the block devices and so are grouped here as
	// well
	`SUBSYSTEM=="nvme"`,
	`KERNEL=="mpt2ctl*"`,
	`KERNEL=="megaraid_sas_ioctl_node"`,
}

type blockDevicesInterface struct {
	commonInterface
}

func init() {
	registerIface(&blockDevicesInterface{commonInterface{
		name:                  "block-devices",
		summary:               blockDevicesSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  blockDevicesBaseDeclarationPlugs,
		baseDeclarationSlots:  blockDevicesBaseDeclarationSlots,
		connectedPlugAppArmor: blockDevicesConnectedPlugAppArmor,
		connectedPlugUDev:     blockDevicesConnectedPlugUDev,
	}})
}
