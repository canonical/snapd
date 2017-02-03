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
	"bytes"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
)

const thumbnailerPermanentSlotAppArmor = `
# Description: Allow use of aa_query_label API

/sys/module/apparmor/parameters/enabled r,
@{PROC}/@{pid}/mounts                   r,
/sys/kernel/security/apparmor/.access   rw,
`

const thumbnailerConnectedSlotAppArmor = `
# Description: Allow access to plug's data directory

@{INSTALL_DIR}/###PLUG_SNAP_NAME###/**     r,
owner @{HOME}/snap/###PLUG_SNAP_NAME###/** r,
/var/snap/###PLUG_SNAP_NAME###/**          r,
`

type ThumbnailerInterface struct{}

func (iface *ThumbnailerInterface) Name() string {
	return "thumbnailer"
}

func (iface *ThumbnailerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ThumbnailerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ThumbnailerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(thumbnailerPermanentSlotAppArmor), nil
	}
	return nil, nil
}

func (iface *ThumbnailerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippet := []byte(thumbnailerConnectedSlotAppArmor)
		old := []byte("###PLUG_SNAP_NAME###")
		new := []byte(plug.Snap.Name())
		snippet = bytes.Replace(snippet, old, new, -1)

		return snippet, nil
	}
	return nil, nil
}

func (iface *ThumbnailerInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ThumbnailerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ThumbnailerInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return true
}
