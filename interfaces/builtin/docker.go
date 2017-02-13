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

	"github.com/snapcore/snapd/interfaces"
)

const dockerConnectedPlugAppArmor = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.
# Usage: reserved

# Allow talking to the docker daemon
/{,var/}run/docker.sock rw,
`

const dockerConnectedPlugSecComp = `
# Description: allow access to the Docker daemon socket. This gives privileged
# access to the system via Docker's socket API.
# Usage: reserved

bind
`

type DockerInterface struct{}

func (iface *DockerInterface) Name() string {
	return "docker"
}

func (iface *DockerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *DockerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(dockerConnectedPlugAppArmor)
		return snippet, nil
	case interfaces.SecuritySecComp:
		snippet := []byte(dockerConnectedPlugSecComp)
		return snippet, nil
	}
	return nil, nil
}

func (iface *DockerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *DockerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	// The docker socket is a named socket and therefore mediated by
	// AppArmor file rules. As such, there is no additional ConnectedSlot
	// policy to add.
	return nil, nil
}

func (iface *DockerInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *DockerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *DockerInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
