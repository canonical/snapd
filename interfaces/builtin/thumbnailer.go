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

const thumbnailerPermanentSlotAppArmor = `
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

const thumbnailerConnectedSlotAppArmor = `
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

const thumbnailerConnectedPlugAppArmor = `
# Description: allow access to the thumbnailer D-Bus service.

#include <abstractions/dbus-session-strict>

dbus (receive, send)
    bus=session
    interface=com.canonical.Thumbnailer
    path=/com/canonical/Thumbnailer
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type ThumbnailerInterface struct{}

func (iface *ThumbnailerInterface) Name() string {
	return "thumbnailer"
}

func (iface *ThumbnailerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ThumbnailerInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	snippet := thumbnailerConnectedPlugAppArmor
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	snippet = strings.Replace(snippet, old, new, -1)
	return spec.AddSnippet(snippet)
}

func (iface *ThumbnailerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ThumbnailerInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	return spec.AddSnippet(thumbnailerPermanentSlotAppArmor)
}

func (iface *ThumbnailerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *ThumbnailerInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	snippet := thumbnailerConnectedSlotAppArmor
	old := "###PLUG_SNAP_NAME###"
	new := plug.Snap.Name()
	snippet = strings.Replace(snippet, old, new, -1)

	old = "###PLUG_SECURITY_TAGS###"
	new = plugAppLabelExpr(plug)
	snippet = strings.Replace(snippet, old, new, -1)
	return spec.AddSnippet(snippet)
}

func (iface *ThumbnailerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
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
