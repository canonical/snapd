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
)

const lxdClientConnectedPlugAppArmor = `
# Description: Can access commands and socket from the 'lxd' snap.
/var/snap/lxd/common/lxd/unix.socket rw,
`

type LxdClientInterface struct{}

func (iface *LxdClientInterface) Name() string {
	return "lxd-client"
}

func (iface *LxdClientInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdClientInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(lxdClientConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return nil, nil
	}
	return nil, nil
}

func (iface *LxdClientInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(lxdClientConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return nil, nil
	}
	return nil, nil
}

func (iface *LxdClientInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *LxdClientInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *LxdClientInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *LxdClientInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *LxdClientInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return false
}
