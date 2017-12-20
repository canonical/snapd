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
	"fmt"

	"github.com/snapcore/snapd/snap"
)

const snapdControlSummary = `allows communicating with snapd`

const snapdControlBaseDeclarationPlugs = `
  snapd-control:
    allow-installation: false
    deny-auto-connection: true
`

const snapdControlBaseDeclarationSlots = `
  snapd-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const snapdControlConnectedPlugAppArmor = `
# Description: Can manage snaps via snapd.

/run/snapd.socket rw,
`

type snapControlInterface struct {
	commonInterface
}

func (iface *snapControlInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if refreshSchedule, ok := plug.Attrs["refresh-schedule"].(string); ok {
		if refreshSchedule != "managed" {
			return fmt.Errorf("unsupported refresh-schedule value: %q", refreshSchedule)
		}
	}

	return nil
}

func init() {
	registerIface(&snapControlInterface{commonInterface{
		name:                  "snapd-control",
		summary:               snapdControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationPlugs:  snapdControlBaseDeclarationPlugs,
		baseDeclarationSlots:  snapdControlBaseDeclarationSlots,
		connectedPlugAppArmor: snapdControlConnectedPlugAppArmor,
		reservedForOS:         true,
	}})
}
