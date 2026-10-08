// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// snap-refresh-control is an empty interface with no actual apparmor/seccomp
// rules, but it's allowing snaps (via explicit check for snap-refresh-control
// connection done by hookstate) to execute "snapctl refresh --proceed" that
// triggers own refreshes, so its use should be limited.
const snapRefreshControlSummary = `allows extended control via snapctl over refreshes involving the snap`

const snapRefreshControlBaseDeclarationPlugs = `
  snap-refresh-control:
    allow-installation: false
    deny-auto-connection: true
`

const snapRefreshControlBaseDeclarationSlots = `
  snap-refresh-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

func init() {
	registerIface(&commonInterface{
		name:                 "snap-refresh-control",
		summary:              snapRefreshControlSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: snapRefreshControlBaseDeclarationPlugs,
		baseDeclarationSlots: snapRefreshControlBaseDeclarationSlots,
	})
}
