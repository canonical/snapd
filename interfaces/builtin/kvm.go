// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const kvmSummary = `allows access to the kvm device`

const kvmBaseDeclarationSlots = `
  kvm:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kvmConnectedPlugAppArmor = `
# Description: Allow write access to kvm.
# See 'man kvm' for details.

/dev/kvm rw,
`

var kvmConnectedPlugUDev = []string{`KERNEL=="kvm"`}

func init() {
	registerIface(&commonInterface{
		name:                  "kvm",
		summary:               kvmSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  kvmBaseDeclarationSlots,
		connectedPlugAppArmor: kvmConnectedPlugAppArmor,
		connectedPlugUDev:     kvmConnectedPlugUDev,
		reservedForOS:         true,
	})
}
