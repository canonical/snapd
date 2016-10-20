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

const lxdConnectedPlugAppArmor = `
# Description: Can change to any apparmor profile (including unconfined) thus
# giving access to all resources of the system so LXD may manage what to give
# to its containers. This gives device ownership to connected snaps.
@{PROC}/**/attr/current r,
/usr/sbin/aa-exec ux,
# Allow access to all of $HOME (user, not snap)
owner @{HOME}/** rwk,
# Run lxd commands
/snap/bin/lxd ux,
/snap/bin/lxd.lxc ux,
# LXD socket
/var/snap/lxd/common/lxd/unix.socket rw,
`

const lxdConnectedPlugSecComp = `
# Description: Can access all syscalls of the system so LXD may manage what to
# give to its containers, giving device ownership to connected snaps.
@unrestricted
setsockopt
bind
`

type LxdInterface struct{}

func (iface *LxdInterface) Name() string {
	return "lxd"
}

func (iface *LxdInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(lxdConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(lxdConnectedPlugSecComp), nil
	}
	return nil, nil
}

func (iface *LxdInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(lxdConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(lxdConnectedPlugSecComp), nil
	}
	return nil, nil
}

func (iface *LxdInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdInterface) SanitizePlug(plug *interfaces.Plug) error {
	snapName := plug.Snap.Name()
	// devName := plug.Snap.Developer
	if snapName == "lxd" {
		return fmt.Errorf("Use lxd-support in the snap providing lxd")
	} /* else if devName != "canonical" {
		return fmt.Errorf("lxd interface is reserved for the upstream LXD project")
	} */
	return nil
}

func (iface *LxdInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LxdInterface) LegacyAutoConnect() bool {
	// since limited to lxd.canonical, we can auto-connect
	return true
}

func (iface *LxdInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
