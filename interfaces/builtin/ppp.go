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

var pppConnectedPlugAppArmor = []byte(`
# Description: Allow operating ppp daemon. Reserved because this gives
#  privileged access to the ppp daemon.
# Usage: reserved

# Needed for modem connections using PPP
/usr/sbin/pppd ix,
/etc/ppp/** rwix,
/dev/ppp rw,
/dev/tty[^0-9]* rw,
/run/lock/*tty[^0-9]* rw,
/run/ppp* rw,
/var/run/ppp* rw,
/var/log/ppp* rw,
/bin/run-parts ix,
@{PROC}/@{pid}/loginuid r,
capability setgid,
capability setuid,
/sbin/resolvconf rix,
/run/resolvconf** rw,
/etc/resolvconf/** rw,
/etc/resolvconf/update.d/* ix,
/lib/resolvconf/* ix,
`)

type PppInterface struct{}

func (iface *PppInterface) Name() string {
	return "ppp"
}

func (iface *PppInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *PppInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecurityAppArmor:
		return pppConnectedPlugAppArmor, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *PppInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return nil, nil
	case interfaces.SecuritySecComp:
		return nil, nil
	case interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	case interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *PppInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *PppInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *PppInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *PppInterface) AutoConnect() bool {
	return false
}
