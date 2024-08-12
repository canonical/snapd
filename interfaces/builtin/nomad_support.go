// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/apparmor"
)

// The nomad-support interface enables running Hashicorp Nomad within
// a strictly confined snap
// https://www.nomadproject.io/

const nomadSupportSummary = `allows operating as the Hashicorp Nomad service`

const nomadSupportBaseDeclarationPlugs = `
  nomad-support:
    allow-installation: false
    deny-auto-connection: true
`

const nomadSupportBaseDeclarationSlots = `
  nomad-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const nomadSupportConnectedPlugAppArmor = `
# Requirements for Nomad service #

# Discovering cgroup info
/sys/fs/cgroup/cgroup.controllers r,
/sys/fs/cgroup/cgroup.subtree_control rw,

# cpuset management top level
/sys/fs/cgroup/nomad.slice/ rw,
/sys/fs/cgroup/nomad.slice/cpuset.cpus.effective r,
/sys/fs/cgroup/nomad.slice/cgroup.type r,

# Required to allow mkdir /sys/fs/cgroup/nomad.slice/
capability dac_override,

# managing our own cgroup config
/sys/fs/cgroup/nomad.slice/cgroup.subtree_control rw,
/sys/fs/cgroup/nomad.slice/*.scope/{,**} rw,

# NVIDIA device plugin
# https://developer.hashicorp.com/nomad/plugins/devices/nvidia
@{PROC}/driver/nvidia/capabilities/mig/config r,
@{PROC}/driver/nvidia/capabilities/mig/monitor r,
`

type nomadSupportInterface struct {
	commonInterface
}

func (iface *nomadSupportInterface) Name() string {
	return "nomad-support"
}

func (iface *nomadSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(nomadSupportConnectedPlugAppArmor)
	return nil
}

func init() {
	registerIface(&nomadSupportInterface{commonInterface{
		name:                 "nomad-support",
		summary:              nomadSupportSummary,
		implicitOnClassic:    true,
		implicitOnCore:       true,
		baseDeclarationPlugs: nomadSupportBaseDeclarationPlugs,
		baseDeclarationSlots: nomadSupportBaseDeclarationSlots,
	}})
}

