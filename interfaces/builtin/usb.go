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
        "github.com/snapcore/snapd/release"
)

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/home
const usbConnectedPlugAppArmor = `
# Description: Can access non-hidden files in user's usb drives.
# Usage: reserved

# Allow read access to media
/media/ rw,
/media/*/ rw,
/media/*/** rw,

/run/media/ rw,
/run/media/*/ rw,
/run/media/*/** rw,
`

// NewHomeInterface returns a new "home" interface.
func NewUsbInterface() interfaces.Interface {
	return &commonInterface{
		name: "usb",
		connectedPlugAppArmor: usbConnectedPlugAppArmor,
		reservedForOS:         true,
		autoConnect:           release.OnClassic,
	}
}
