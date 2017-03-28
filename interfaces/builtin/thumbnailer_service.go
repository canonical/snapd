// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const thumbnailerServicePermanentSlotAppArmor = `
# Description: Allow use of aa_query_label API. This
# discloses the AppArmor policy for all processes.

/sys/module/apparmor/parameters/enabled r,
@{PROC}/@{pid}/mounts                   r,
/sys/kernel/security/apparmor/.access   rw,

# Description: Allow owning the Thumbnailer bus name on the session bus

#include <abstractions/dbus-session-strict>

dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member={RequestName,ReleaseName,GetConnectionCredentials}
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (bind)
    bus=session
    name=com.canonical.Thumbnailer,
`

const thumbnailerServiceConnectedSlotAppArmor = `
# Description: Allow access to plug's data directory.

@{INSTALL_DIR}/###PLUG_SNAP_NAME###/**     r,
owner @{HOME}/snap/###PLUG_SNAP_NAME###/** r,
/var/snap/###PLUG_SNAP_NAME###/**          r,

# Description: allow client snaps to access the thumbnailer service.
dbus (receive, send)
    bus=session
    interface=com.canonical.Thumbnailer
    path=/com/canonical/Thumbnailer
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const thumbnailerServiceConnectedPlugAppArmor = `
# Description: allow access to the thumbnailer D-Bus service.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    interface=com.canonical.Thumbnailer
    path=/com/canonical/Thumbnailer
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type ThumbnailerServiceInterface struct{}

func (iface *ThumbnailerServiceInterface) Name() string {
	return "thumbnailer-service"
}

func (iface *ThumbnailerServiceInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	snippet := thumbnailerServiceConnectedPlugAppArmor
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *ThumbnailerServiceInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	spec.AddSnippet(thumbnailerServicePermanentSlotAppArmor)
	return nil
}

func (iface *ThumbnailerServiceInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	snippet := thumbnailerServiceConnectedSlotAppArmor
	old := "###PLUG_SNAP_NAME###"
	new := plug.Snap.Name()
	snippet = strings.Replace(snippet, old, new, -1)

	old = "###PLUG_SECURITY_TAGS###"
	new = plugAppLabelExpr(plug)
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *ThumbnailerServiceInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ThumbnailerServiceInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	return nil
}

func (iface *ThumbnailerServiceInterface) AutoConnect(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	return true
}

func (iface *ThumbnailerServiceInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *ThumbnailerServiceInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
