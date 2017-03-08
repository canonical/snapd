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
	"github.com/snapcore/snapd/interfaces/seccomp"
)

const lxdSupportConnectedPlugAppArmor = `
# Description: Can change to any apparmor profile (including unconfined) thus
# giving access to all resources of the system so LXD may manage what to give
# to its containers. This gives device ownership to connected snaps.
@{PROC}/**/attr/current r,
/usr/sbin/aa-exec ux,
`

const lxdSupportConnectedPlugSecComp = `
# Description: Can access all syscalls of the system so LXD may manage what to
# give to its containers, giving device ownership to connected snaps.
@unrestricted
`

type LxdSupportInterface struct{}

func (iface *LxdSupportInterface) Name() string {
	return "lxd-support"
}

func (iface *LxdSupportInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdSupportInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	if securitySystem == interfaces.SecurityAppArmor {
		return []byte(lxdSupportConnectedPlugAppArmor), nil
	}
	return nil, nil
}

func (iface *LxdSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	return spec.AddSnippet(lxdSupportConnectedPlugSecComp)
}

func (iface *LxdSupportInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdSupportInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdSupportInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *LxdSupportInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LxdSupportInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
