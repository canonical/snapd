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

// Only allow raw disk devices; not loop, ram, CDROM, generic SCSI, network,
// tape, raid, etc devices or disk partitions
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

# Access to raw devices, not individual partitions
/dev/hd[a-t] rw,                         # IDE, MFM, RLL
/dev/sd{,[a-h]}[a-z] rw,                 # SCSI
/dev/sdi[a-v] rw,                        # SCSI continued
/dev/i2o/hd{,[a-c]}[a-z] rw,             # I2O hard disk
/dev/i2o/hdd[a-x] rw,                    # I2O hard disk continued
/dev/mmcblk[0-9]{,[0-9],[0-9][0-9]} rw,  # MMC (up to 1000 devices)
/dev/nvme[0-9]{,[0-9]} rw,               # NVMe (up to 100 devices)
/dev/vd[a-z] rw,                         # virtio

# SCSI device commands, et al
capability sys_rawio,

# Perform various privileged block-device ioctl operations
capability sys_admin,

# Devices for various controllers used with ioctl()
/dev/mpt2ctl{,_wd} rw,
/dev/megaraid_sas_ioctl_node rw,
`

var blockDevicesConnectedPlugUDev = []string{
	`SUBSYSTEM=="block"`,
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
