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

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/systemd"
)

const plymouthReplaceSummary = `allows communicating with snapd`

const plymouthReplaceBaseDeclarationPlugs = `
  plymouth-replace:
    allow-installation: false
    deny-auto-connection: true
`

const plymouthReplaceBaseDeclarationSlots = `
  plymouth-replace:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const plymouthReplaceConnectedPlugAppArmor = `
# Description: can communicate with plymouthd

unix (connect, receive, send)
      type=stream
      peer=(addr="@/org/freedesktop/plymouthd"),
/usr/bin/plymouth ixr,
`

type plymouthReplaceInterface struct {
	commonInterface
}

func (iface *plymouthReplaceInterface) SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return spec.AddDropIn(plug.Name(), `
[Unit]
Conflicts=plymouth-quit.service
After=plymouth-quit.service
OnFailure=plymouth-quit.service
Conflicts=getty@tty1.service
After=getty@tty1.service
`)
}

func init() {
	registerIface(&plymouthReplaceInterface{commonInterface{
		name:                  "plymouth-replace",
		summary:               plymouthReplaceSummary,
		implicitOnCore:        true,
		implicitOnClassic:     false,
		baseDeclarationPlugs:  plymouthReplaceBaseDeclarationPlugs,
		baseDeclarationSlots:  plymouthReplaceBaseDeclarationSlots,
		connectedPlugAppArmor: plymouthReplaceConnectedPlugAppArmor,
	}})
}
