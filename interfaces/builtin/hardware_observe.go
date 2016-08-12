// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

import (
	"github.com/snapcore/snapd/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const hardwareObserveConnectedPlugAppArmor = `
# Description: This interface allows for getting hardware information
# from the system.  this is reserved because it allows reading potentially sensitive information.
# Usage: reserved

# files in /sys pertaining to hardware
/sys/{block,bus,class,devices}/{,**} r,

# DMI tables
/sys/firmware/dmi/tables/DMI r,
/sys/firmware/dmi/tables/smbios_entry_point r,
`

// NewHardwareObserveInterface returns a new "hardware-observe" interface.
func NewHardwareObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "hardware-observe",
		connectedPlugAppArmor: hardwareObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
