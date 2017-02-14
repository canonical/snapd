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

import (
	"github.com/snapcore/snapd/interfaces"
)

const bootControlConnectedPlugAppArmor = `
# Description: Can access and modify boot config files. This gives device
# ownership to the snap.

# Allow read/write access to the pi2 boot config.txt. WARNING: improperly
# editing this file may render the system unbootable.
owner /boot/uboot/config.txt rwk,
`

func NewBootControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "boot-control",
		connectedPlugAppArmor: bootControlConnectedPlugAppArmor,
	}
}
