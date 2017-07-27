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

const openvswitchSupportSummary = `allows operating as the openvswitch service`

const openvswitchSupportBaseDeclarationSlots = `
  openvswitch-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const openvswitchSupportConnectedPlugAppArmor = `
/run/uuidd/request rw,
`

var openvswitchSupportConnectedPlugKmod = []string{`openvswitch`}

func init() {
	registerIface(&commonInterface{
		name:                     "openvswitch-support",
		summary:                  openvswitchSupportSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     openvswitchSupportBaseDeclarationSlots,
		connectedPlugKModModules: openvswitchSupportConnectedPlugKmod,
		connectedPlugAppArmor:    openvswitchSupportConnectedPlugAppArmor,
		reservedForOS:            true,
	})
}
