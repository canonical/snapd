// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

const lxdSupportSummary = `allows operating as the LXD service`

const lxdSupportBaseDeclarationPlugs = `
  lxd-support:
    allow-installation: false
    deny-auto-connection: true
`

const lxdSupportBaseDeclarationSlots = `
  lxd-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const lxdSupportConnectedPlugAppArmor = `
# Description: Can change to any apparmor profile (including unconfined) thus
# giving access to all resources of the system so LXD may manage what to give
# to its containers. This gives device ownership to connected snaps.
@{PROC}/**/attr/current r,
/usr/sbin/aa-exec ux,

# Allow discovering the os-release of the host
/var/lib/snapd/hostfs/{etc,usr/lib}/os-release r,
`

const lxdSupportConnectedPlugSecComp = `
# Description: Can access all syscalls of the system so LXD may manage what to
# give to its containers, giving device ownership to connected snaps.
@unrestricted
`

func init() {
	registerIface(&commonInterface{
		name:                  "lxd-support",
		summary:               lxdSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  lxdSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  lxdSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: lxdSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  lxdSupportConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
