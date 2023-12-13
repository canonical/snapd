// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

const microcephSupportSummary = `allows operating as the MicroCeph service`

const microcephSupportBaseDeclarationPlugs = `
  microceph-support:
    allow-installation: false
    deny-auto-connection: true
`

const microcephSupportBaseDeclarationSlots = `
  microceph-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const microcephSupportConnectedPlugAppArmor = `

# Allow bcache devices to be accessed since DM devices may be set up on top of those.
/dev/bcache[0-9]{,[0-9],[0-9][0-9]} rwk,                   # bcache (up to 1000 devices)
# Access to individual partitions
/dev/hd[a-t][0-9]{,[0-9],[0-9][0-9]} rwk,                  # IDE, MFM, RLL
/dev/sd{,[a-z]}[a-z][0-9]{,[0-9],[0-9][0-9]} rwk,          # SCSI
/dev/vd{,[a-z]}[a-z][0-9]{,[0-9],[0-9][0-9]} rwk,          # virtio
/dev/nvme{[0-9],[1-9][0-9]}n{[1-9],[1-5][0-9],6[0-3]}p[0-9]{,[0-9],[0-9][0-9]} rwk, # NVMe
# Access device mapper blockdevs
/dev/dm-[0-9]{,[0-9],[0-9][0-9]} rwk,                      # device mapper (up to 1000 devices)
# Allow managing of rbd-backed block devices
/sys/bus/rbd/add rwk,                                      # add block dev
/sys/bus/rbd/remove rwk,                                   # remove block dev
/sys/bus/rbd/add_single_major rwk,                         # add single major dev
/sys/bus/rbd/remove_single_major rwk,                      # remove single major dev
/sys/bus/rbd/supported_features r,                         # display enabled features
/sys/bus/rbd/devices/** rwk,                               # manage individual block devs

`

func init() {
	registerIface(&commonInterface{
		name:                  "microceph-support",
		summary:               microcephSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  microcephSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  microcephSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: microcephSupportConnectedPlugAppArmor,
	})
}
