// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

// Allow control of LVM configuration. The consuming snap is expected
// to also add plugs for `device-mapper-control`, `block-devices` and
// any other interfaces required to actually access devices for use
// with LVM. The consuming snap must also ship the LVM2 tools.
const lvmControlSummary = `allows control of LVM configuration`

const lvmControlBaseDeclarationPlugs = `
  lvm-control:
    allow-installation: false
    deny-auto-connection: true
`

const lvmControlBaseDeclarationSlots = `
  lvm-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const lvmControlConnectedPlugAppArmor = `
# Description: Allow control of LVM configuration.

/etc/lvm/** rwkl,
/run/lock/lvm/** rwk,
/run/lvm/** rwk,
`

type lvmControlInterface struct {
	commonInterface
}

func init() {
	registerIface(&lvmControlInterface{commonInterface{
		name:                  "lvm-control",
		summary:               lvmControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  lvmControlBaseDeclarationPlugs,
		baseDeclarationSlots:  lvmControlBaseDeclarationSlots,
		connectedPlugAppArmor: lvmControlConnectedPlugAppArmor,
	}})
}
