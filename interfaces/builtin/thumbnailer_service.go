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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/snap"
)

const thumbnailerServiceSummary = `allows operating as or interacting with the Thumbnailer service`

const thumbnailerServiceBaseDeclarationSlots = `
  thumbnailer-service:
    allow-installation:
      slot-snap-type:
        - app
    deny-auto-connection: true
    deny-connection: true
`

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

# Allow clients to introspect the service
dbus (send)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    path=/com/canonical/Thumbnailer
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type thumbnailerServiceInterface struct{}

func (iface *thumbnailerServiceInterface) Name() string {
	return "thumbnailer-service"
}

func (iface *thumbnailerServiceInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              thumbnailerServiceSummary,
		BaseDeclarationSlots: thumbnailerServiceBaseDeclarationSlots,
	}
}

func (iface *thumbnailerServiceInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := thumbnailerServiceConnectedPlugAppArmor
	old := "###SLOT_SECURITY_TAGS###"
	new := slot.LabelExpression()
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *thumbnailerServiceInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(thumbnailerServicePermanentSlotAppArmor)
	return nil
}

func (iface *thumbnailerServiceInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := thumbnailerServiceConnectedSlotAppArmor
	old := "###PLUG_SNAP_NAME###"
	// parallel-installs: PLUG_SNAP_NAME is used in the context of dbus
	// mediation rules, need to use the actual instance name
	new := plug.Snap().InstanceName()
	snippet = strings.Replace(snippet, old, new, -1)

	old = "###PLUG_SECURITY_TAGS###"
	new = plug.LabelExpression()
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *thumbnailerServiceInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&thumbnailerServiceInterface{})
}
