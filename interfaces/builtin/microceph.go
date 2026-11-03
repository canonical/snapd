// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const microcephSummary = `allows access to the MicroCeph socket`

const microcephBaseDeclarationSlots = `
  microceph:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const microcephConnectedPlugAppArmor = `
# Description: allow access to the MicroCeph control socket.

/var/snap/###SLOT_INSTANCE_NAME###/common/state/control.socket rw,
`

const microcephConnectedPlugSecComp = `
# Description: allow access to the MicroCeph control socket.

socket AF_NETLINK - NETLINK_GENERIC
`

type microcephInterface struct {
	commonInterface
}

// AppArmorConnectedPlug uses the connected slot's instance name.
func (iface *microcephInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_INSTANCE_NAME###"
	new := slot.Snap().InstanceName().String()
	snippet := strings.ReplaceAll(microcephConnectedPlugAppArmor, old, new)
	spec.AddSnippet(snippet)
	return nil
}

func init() {
	registerIface(&microcephInterface{commonInterface{
		name:                 "microceph",
		summary:              microcephSummary,
		baseDeclarationSlots: microcephBaseDeclarationSlots,
		connectedPlugSecComp: microcephConnectedPlugSecComp,
	}})
}
