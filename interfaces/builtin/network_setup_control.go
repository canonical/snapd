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

import (
	"fmt"

	"github.com/snapcore/snapd/snap"
)

const networkSetupControlSummary = `allows access to netplan configuration`

const networkSetupControlBaseDeclarationSlots = `
  network-setup-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
    deny-connection:
      plug-attributes:
        netplan-apply: true
`

const networkSetupControlConnectedPlugAppArmor = `
# Description: Can read/write netplan configuration files

/etc/netplan/{,**} rw,
/etc/network/{,**} rw,
/etc/systemd/network/{,**} rw,

# netplan generate
/run/ r,
/run/systemd/network/{,**} r,
/run/systemd/network/*-netplan-* w,
/run/NetworkManager/conf.d/{,**} r,
/run/NetworkManager/conf.d/*netplan*.conf* w,

/run/udev/rules.d/ rw,                 # needed for cloud-init
/run/udev/rules.d/[0-9]*-netplan-* rw,
`

type networkSetupControlInterface struct {
	commonInterface
}

func init() {
	registerIface(&networkSetupControlInterface{
		commonInterface{
			name:                  "network-setup-control",
			summary:               networkSetupControlSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationSlots:  networkSetupControlBaseDeclarationSlots,
			connectedPlugAppArmor: networkSetupControlConnectedPlugAppArmor,
			reservedForOS:         true,
		},
	})
}

func (iface *networkSetupControlInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if netplan-apply isn't specified, but if it is,
	// it needs to be bool
	if v, ok := plug.Attrs["netplan-apply"]; ok {
		if _, ok = v.(bool); !ok {
			return fmt.Errorf("network-setup-control plug requires bool with 'netplan-apply'")
		}
	}

	return nil
}
