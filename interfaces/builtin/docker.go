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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
)

const dockerSummary = `allows access to Docker socket`

const dockerBaseDeclarationSlots = `
  docker:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const dockerConnectedPlugAppArmor = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.

# Allow talking to the docker daemon
/{,var/}run/docker.sock rw,
`

const dockerConnectedPlugSecComp = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.

bind
socket AF_NETLINK - NETLINK_GENERIC
`

type dockerInterface struct{}

func (iface *dockerInterface) Name() string {
	return "docker"
}

func (iface *dockerInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              dockerSummary,
		BaseDeclarationSlots: dockerBaseDeclarationSlots,
	}
}

func (iface *dockerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(dockerConnectedPlugAppArmor)
	return nil
}

func (iface *dockerInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(dockerConnectedPlugSecComp)
	return nil
}

func (iface *dockerInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&dockerInterface{})
}
