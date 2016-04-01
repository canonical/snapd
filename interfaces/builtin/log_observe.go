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
	"github.com/ubuntu-core/snappy/interfaces"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const logObserveConnectedPlugAppArmor = `
# Description: Can read system logs.
# Usage: reserved

/var/log/ r,
/var/log/** r,

# Needed since we are root and the owner/group doesn't match :\
# So long as we have this, the cap must be reserved.
capability dac_override,
`

// NewLogObserveInterface returns a new "log-observe" interface.
func NewLogObserveInterface() interfaces.Interface {
	return &commonInterface{
		name: "log-observe",
		connectedPlugAppArmor: logObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
