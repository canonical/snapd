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

import "github.com/snapcore/snapd/interfaces"

const tpmConnectedPlugAppArmor = `
# Description: for those who need to talk to the system TPM chip over /dev/tpm0

/dev/tpm0 rw,
`

func NewTpmInterface() interfaces.Interface {
	return &commonInterface{
		name: "tpm",
		connectedPlugAppArmor: tpmConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
