// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

/*
 * The dm-multipath interface allows snaps to manage device-mapper
 * multipath maps by communicating with the multipathd daemon. It is intended
 * for storage orchestration software that needs to list, create, reload and
 * remove multipath devices and react to path state changes.
 *
 * Direct unrestricted access to arbitrary raw block devices is not granted;
 * normal snap device cgroup mediation still applies.
 */

const dmMultipathSummary = `allows managing device-mapper multipath maps via multipathd`

const dmMultipathBaseDeclarationPlugs = `
  dm-multipath:
    allow-installation: false
    deny-auto-connection: true
`

const dmMultipathBaseDeclarationSlots = `
  dm-multipath:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const dmMultipathConnectedPlugAppArmor = `
# Global multipath configuration and persistent WWID to device name mappings
/etc/multipath.conf r,
/etc/multipath/bindings rwk,
/etc/multipath/wwids rwk,

# Device-mapper control interface for multipath map creation, modification and removal
/dev/mapper/control rw,
# Access to multipath device nodes and their symlinks
/dev/mapper/{,**} rw,
# Direct access to underlying device-mapper block devices
/dev/dm-[0-9]* rwk,
# Access to bcache devices that may be used as paths in multipath configurations
/dev/bcache[0-9]{,[0-9],[0-9][0-9]} rw,                   # bcache (up to 1000 devices)

# Communication with multipathd daemon for managing multipath devices
unix (send, receive, connect) type=stream peer=(addr="@/org/kernel/linux/storage/multipathd"),
`

var dmMultipathConnectedPlugUDev = []string{
	`KERNEL=="device-mapper"`,
	`KERNEL=="dm-[0-9]*"`,
}

type dmMultipathInterface struct {
	commonInterface
}

var dmMultipathConnectedPlugKmod = []string{
	`dm-mod`, // Device mapper.
}

func init() {
	registerIface(&dmMultipathInterface{commonInterface{
		name:                     "dm-multipath",
		summary:                  dmMultipathSummary,
		implicitOnClassic:        true,
		baseDeclarationSlots:     dmMultipathBaseDeclarationSlots,
		baseDeclarationPlugs:     dmMultipathBaseDeclarationPlugs,
		connectedPlugAppArmor:    dmMultipathConnectedPlugAppArmor,
		connectedPlugKModModules: dmMultipathConnectedPlugKmod,
		connectedPlugUDev:        dmMultipathConnectedPlugUDev,
	}})
}
