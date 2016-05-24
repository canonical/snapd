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

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/timeserver-control
const timeserverControlConnectedPlugAppArmor = `
# Description: Can manage timeservers directly separate from config ubuntu-core.
# Usage: reserved

# Won't work until LP: #1504657 is fixed. Requires reboot until timesyncd
# notices the change or systemd restarts it.
/etc/systemd/timesyncd.conf rw,
`

// NewTimeserverControlInterface returns a new "timeserver-control" interface.
func NewTimeserverControlInterface() interfaces.Interface {
	return &commonInterface{
		name: "timeserver-control",
		connectedPlugAppArmor: timeserverControlConnectedPlugAppArmor,
		reservedForOS:         true,
	}
}
