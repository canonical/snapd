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

const coreSupportConnectedPlugAppArmor = `
# Description: Can control all aspects of systemd via the systemctl command. It
# allows execution of the systemctl binary unconfined. As such, this gives device
# ownership to the snap.

/bin/systemctl Uxr,
`

// NewShutdownInterface returns a new "shutdown" interface.
func NewCoreSupportInterface() interfaces.Interface {
	return &commonInterface{
		name: "core-support",
		connectedPlugAppArmor: coreSupportConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
