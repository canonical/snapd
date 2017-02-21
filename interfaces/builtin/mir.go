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
	"bytes"

	"github.com/snapcore/snapd/interfaces"
)

var mirPermanentSlotAppArmor = []byte(`
# Description: Allow operating as the Mir server. This gives privileged access
# to the system.

# needed since Mir is the display server, to configure tty devices
capability sys_tty_config,
/{dev,run}/shm/\#* rw,
/dev/tty[0-9]* rw,
network netlink raw,
/run/mir_socket rw,
#NOTE: this allows reading and inserting all input events
/dev/input/* rw,
/run/udev/data/c13:[0-9]* r,
/run/udev/data/+input:input[0-9]* r,
`)

var mirPermanentSlotSecComp = []byte(`
# Description: Allow operating as the mir server. This gives privileged access
# to the system.
# Needed for server launch
bind
listen
# Needed by server upon client connect
accept
accept4
shmctl
`)

var mirConnectedSlotAppArmor = []byte(`
# Description: Permit clients to use Mir
unix (receive, send) type=seqpacket addr=none peer=(label=###PLUG_SECURITY_TAGS###),
`)

var mirConnectedPlugAppArmor = []byte(`
# Description: Permit clients to use Mir
unix (receive, send) type=seqpacket addr=none peer=(label=###SLOT_SECURITY_TAGS###),
/run/mir_socket rw,
/run/user/[0-9]*/mir_socket rw,
`)

type MirInterface struct{}

func (iface *MirInterface) Name() string {
	return "mir"
}

func (iface *MirInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *MirInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(mirConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *MirInterface) PermanentSlotSnippet(
	slot *interfaces.Slot,
	securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return mirPermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return mirPermanentSlotSecComp, nil
	}
	return nil, nil
}

func (iface *MirInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet := bytes.Replace(mirConnectedSlotAppArmor, old, new, -1)
		return snippet, nil
	}
	return nil, nil
}

func (iface *MirInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *MirInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *MirInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}
