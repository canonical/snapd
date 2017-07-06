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

const libvirtSummary = `allows access to libvirt service`

const libvirtBaseDeclarationSlots = `
  libvirt:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const libvirtConnectedPlugAppArmor = `
/run/libvirt/libvirt-sock rw,
/etc/libvirt/* r,
`

const libvirtConnectedPlugSecComp = `
listen
accept
accept4
`

func init() {
	registerIface(&commonInterface{
		name:                  "libvirt",
		summary:               libvirtSummary,
		implicitOnClassic:     true,
		baseDeclarationSlots:  libvirtBaseDeclarationSlots,
		connectedPlugAppArmor: libvirtConnectedPlugAppArmor,
		connectedPlugSecComp:  libvirtConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
