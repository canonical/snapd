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

// Only allow disk device partitions; not loop, ram, CDROM, generic SCSI,
// network, tape, raid, etc devices
const rawVolumeSummary = `allows access to disk block device partitions`

const rawVolumeBaseDeclarationPlugs = `
  raw-volume:
    allow-installation: false
    deny-auto-connection: true
`

const rawVolumeBaseDeclarationSlots = `
  raw-volume:
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
const rawVolumeConnectedPlugAppArmor = `
# Description: Allow write access to disk block device partitions.

@{PROC}/devices r,
/run/udev/data/b[0-9]*:[0-9]* r,
/sys/block/ r,
/sys/devices/**/block/** r,

# Access to raw devices, not individual partitions. Note that partition '0' is
# not included since it refers to the whole disk.

# IDE, MFM, RLL. 1-63 partitions
/dev/hd[a-t]{[1-9],[1-5][0-9],6[0-3]} rw,

# SCSI. 1-15 partitions
/dev/sd{,[a-h]}[a-z]{[1-9],1[0-5]} rw,
/dev/sdi[a-v]{[1-9],1[0-5]} rw,

# I2O hard disk. 1-15 partitions
/dev/i2o/hd{,[a-c]}[a-z]{[1-9],1[0-5]} rw,
/dev/i2o/hdd[a-x]{[1-9],1[0-5]} rw,

# MMC (up to 1000 devices). 1-7 partitions
/dev/mmcblk[0-9]{,[0-9],[0-9][0-9]}p[1-7] rw,

# NVMe (up to 100 devices). 1-63 partitions and 1-63 namespaces with partitions
/dev/nvme[0-9]{,[0-9]}p{[1-9],[1-5][0-9],6[0-3]} rw,
/dev/nvme[0-9]{,[0-9]}n{[1-9],[1-5][0-9],6[0-3]}p{[1-9],[1-5][0-9],6[0-3]} rw,

# virtio. 1-63 partitions
/dev/vd[a-z]{[1-9],[1-5][0-9],6[0-3]} rw,

# needed for write access
capability sys_admin,
`

var rawVolumeConnectedPlugUDev = []string{
	`SUBSYSTEM=="block"`,
}

type rawVolumeInterface struct {
	commonInterface
}

func init() {
	registerIface(&rawVolumeInterface{commonInterface{
		name:                  "raw-volume",
		summary:               rawVolumeSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  rawVolumeBaseDeclarationPlugs,
		baseDeclarationSlots:  rawVolumeBaseDeclarationSlots,
		connectedPlugAppArmor: rawVolumeConnectedPlugAppArmor,
		connectedPlugUDev:     rawVolumeConnectedPlugUDev,
		reservedForOS:         true,
	}})
}
