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

const classicDimensionPlugAppArmor = `
# Description: Extra permissions to use the classic dimension on Ubuntu Core

mount fstype=devpts options=(rw) devpts -> /dev/pts/,
/bin/mountpoint ixr,
@{PROC}/[0-9]*/mountinfo r,
/var/lib/extrausers/{,*} r,
capability fsetid,
capability dac_override,
/etc/sudoers.d/{,*} r,
/usr/bin/systemd-run Uxr,
/bin/systemctl Uxr,
`

func NewClassicDimensionInterface() interfaces.Interface {
	return &commonInterface{
		name: "classic-dimension",
		connectedPlugAppArmor: classicDimensionPlugAppArmor,
	}
}
