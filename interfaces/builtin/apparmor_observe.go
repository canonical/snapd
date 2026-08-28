// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

const apparmorObserveSummary = `allows querying AppArmor access decisions`

const apparmorObserveBaseDeclarationSlots = `
  apparmor-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const apparmorObserveConnectedPlugAppArmor = `
# Description: Allow use of AppArmor's access query interface.
# The query interface is writable because callers write a query to .access
# and then read the kernel's response back from the same file.
/sys/kernel/security/apparmor/.access rw,
`

func init() {
	registerIface(&commonInterface{
		name:                  "apparmor-observe",
		summary:               apparmorObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  apparmorObserveBaseDeclarationSlots,
		connectedPlugAppArmor: apparmorObserveConnectedPlugAppArmor,
	})
}
