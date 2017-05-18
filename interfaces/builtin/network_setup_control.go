// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

import "github.com/snapcore/snapd/release"

const networkSetupControlConnectedPlugAppArmor = `
# Description: Can read/write netplan configuration files

/etc/netplan/{,**} rw,
/etc/network/{,**} rw,
`

const networkSetupControlConnectedPlugAppArmorClassic = `
# Description: Can read/write NetworkManager's dispatcher (nm-dispatcher) scripts

/etc/NetworkManager/dispatcher.d/{,**} rw,
`

func init() {

	var classicAppArmor string
	if release.OnClassic {
		classicAppArmor = networkSetupControlConnectedPlugAppArmorClassic
	}

	registerIface(&commonInterface{
		name: "network-setup-control",
		connectedPlugAppArmor: networkSetupControlConnectedPlugAppArmor + classicAppArmor,
		reservedForOS:         true,
	})
}
