// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

const microcephSummary = `allows access to the MicroCeph socket`

const microcephBaseDeclarationSlots = `
  microceph:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const microcephConnectedPlugAppArmor = `
# Description: allow access to the MicroCeph control socket.

/var/snap/microceph/common/state/control.socket rw,

# Allow bcache devices to be accessed since DM devices may be set up on top of those.
/dev/bcache[0-9]{,[0-9],[0-9][0-9]} rwk,                   # bcache (up to 1000 devices)
# Access to individual partitions
/dev/hd[a-t][0-9]{,[0-9],[0-9][0-9]} rwk,                  # IDE, MFM, RLL
/dev/sd{,[a-z]}[a-z][0-9]{,[0-9],[0-9][0-9]} rwk,          # SCSI
/dev/vd{,[a-z]}[a-z][0-9]{,[0-9],[0-9][0-9]} rwk,          # virtio
/dev/nvme{[0-9],[1-9][0-9]}n{[1-9],[1-5][0-9],6[0-3]}p[0-9]{,[0-9],[0-9][0-9]} rwk, # NVMe
# Allow managing of rbd-backed block devices
/sys/bus/rbd/add rwk,                                      # add block dev
/sys/bus/rbd/remove rwk,                                   # remove block dev
/sys/bus/rbd/add_single_major rwk,                         # add single major dev
/sys/bus/rbd/remove_single_major rwk,                      # remove single major dev
/sys/bus/rbd/supported_features r,                         # display enabled features
/sys/bus/rbd/devices/** rwk,                               # manage individual block devs
`

const microcephConnectedPlugSecComp = `
# Description: allow access to the MicroCeph control socket.

socket AF_NETLINK - NETLINK_GENERIC
`

func init() {
	registerIface(&commonInterface{
		name:                  "microceph",
		summary:               microcephSummary,
		baseDeclarationSlots:  microcephBaseDeclarationSlots,
		connectedPlugAppArmor: microcephConnectedPlugAppArmor,
		connectedPlugSecComp:  microcephConnectedPlugSecComp,
	})
}
