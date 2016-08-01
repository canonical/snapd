// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (c) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more dtails.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.
 *
 */

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

var mirPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the Mir server. Reserved because this
# gives privileged access to the system.
# Usage: reserved

capability dac_override,
capability sys_tty_config,
capability sys_admin,

unix (receive, send) type=seqpacket addr=none,
/dev/dri/card0 rw,
/dev/shm/\#* rw,
/dev/tty* wr,
/run/udev/data/* r,
network netlink raw,
/run/mir_socket rw,
#NOTE: this allows reading and inserting all input events
/dev/input/* rw,

`)

var mirPermanentSlotSecComp = []byte(`
# Description: Allow operating as the mir service. Reserved because this
# gives privileged access to the system.

# Needed for server launch
bind
listen
setsockopt
getsockname

# Needed by server upon client connect
sendto
accept
shmctl
open
getsockopt
recvmsg
sendmsg

`)

var mirConnectedSlotAppArmor = []byte(`
# Description: Permit clients to use Mir
# Usage: reserver

/run/user/*/mir_socket rw,
unix (send, receive) peer=(label=###PLUG_SECURITY_TAGS###)

`)

var mirConnectedPlugAppArmor = []byte(`
# Description: Permit clients to use Mir
# Usage: reserver

/run/user/*/mir_socket rw,
unix (send, receive) peer=(label=###SLOT_SECURITY_TAGS###)

`)

var mirConnectedPlugSecComp = []byte(`
# Description: Permit clients to use Mir
# Usage: reserver
getsockname
recvmsg
sendmsg
sendto
`)

type MirInterface struct{}

func (iface *MirInterface) Name() string {
	return "mir"
}

func (iface *MirInterface) PermanentPlugSnippet(
	plug *interfaces.Plug,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
					case interfaces.SecurityAppArmor, interfaces.SecuritySecComp,
				interfaces.SecurityUDev, interfaces.SecurityDBus,
		                interfaces.SecurityMount:
				return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) ConnectedPlugSnippet(
	plug *interfaces.Plug,
	slot *interfaces.Slot,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(mirConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp,
		interfaces.SecurityUDev, interfaces.SecurityDBus,
                interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) PermanentSlotSnippet(
	slot *interfaces.Slot,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return mirPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return mirPermanentSlotSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(mirConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp,
		interfaces.SecurityUDev, interfaces.SecurityDBus,
                interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *MirInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *MirInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MirInterface) AutoConnect() bool {
	return true
}
