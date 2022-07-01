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

const zfsSummary = `allows manipulating ZFS volumes and pools`

const zfsBaseDeclarationPlugs = `
  zfs:
    allow-installation: false
    deny-auto-connection: true
`

const zfsBaseDeclarationSlots = `
  zfs:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
 `

const zfsConnectedPlugAppArmor = `# Description: Allow access to the ZFS device control interface
  /dev/zfs rw,
`

var zfsConnectedPlugUDev = []string{
	`KERNEL=="zfs"`,
}

type zfsInterface struct {
	commonInterface
}

func init() {
	registerIface(&zfsInterface{commonInterface{
		name:                  "zfs",
		summary:               zfsSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  zfsBaseDeclarationSlots,
		baseDeclarationPlugs:  zfsBaseDeclarationPlugs,
		connectedPlugAppArmor: zfsConnectedPlugAppArmor,
		connectedPlugUDev:     zfsConnectedPlugUDev,
	}})
}
