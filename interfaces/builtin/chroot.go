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

func init() {
	allInterfaces = append(allInterfaces, NewChrootInterface())
}

const chrootConnectedPlugAppArmor = `
# Description: Can chroot into a directory.

capability sys_chroot,

/{,usr/}sbin/chroot ixr,
`

const chrootConnectedPlugSecComp = `
# Description: Can use the chroot syscall
chroot
`

// NewChrootInterface returns a new "chroot" interface.
func NewChrootInterface() interfaces.Interface {
	return &commonInterface{
		name: "chroot",
		connectedPlugAppArmor: chrootConnectedPlugAppArmor,
		connectedPlugSecComp:  chrootConnectedPlugSecComp,
		reservedForOS:         true,
	}
}
