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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/timezone-control
const timezoneControlConnectedPlugAppArmor = `
# Description: Can manage timezones directly separate from 'config ubuntu-core'.
# Usage: reserved

/usr/share/zoneinfo/      r,
/usr/share/zoneinfo/**    r,
/etc/{,writable/}timezone rw,
`

// NewTimezoneControlInterface returns a new "timezone-control" interface.
func NewTimezoneControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "timezone-control",
		connectedPlugAppArmor: timezoneControlConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
