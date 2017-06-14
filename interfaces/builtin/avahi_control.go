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
    "strings"

    "github.com/snapcore/snapd/interfaces"
    "github.com/snapcore/snapd/interfaces/apparmor"
    "github.com/snapcore/snapd/interfaces/dbus"
    "github.com/snapcore/snapd/release"
)

const avahiControlBaseDeclarationSlots = `
  avahi-control:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const avahiControlSummary = `allows control over local domains, hostnames and services`

const avahiControlConnectedSlotAppArmor = `
# EntryGroup
dbus (receive)
    bus=system
    path=/Client*/EntryGroup*
    interface=org.freedesktop.Avahi.EntryGroup
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (send)
    bus=system
    interface=org.freedesktop.Avahi.EntryGroup
    member=StateChanged
    peer=(name=org.freedesktop.Avahi, label=###PLUG_SECURITY_TAGS###),
`

const avahiControlConnectedPlugAppArmor = `
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=Set*
    peer=(name=org.freedesktop.Avahi,label=###SLOT_SECURITY_TAGS###),

# EntryGroup
dbus (send)
    bus=system
    path=/
    interface=org.freedesktop.Avahi.Server
    member=EntryGroupNew
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/EntryGroup*
    interface=org.freedesktop.Avahi.EntryGroup
    member={Free,Commit,Reset}
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/EntryGroup*
    interface=org.freedesktop.Avahi.EntryGroup
    member={GetState,IsEmpty,UpdateServiceTxt}
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (send)
    bus=system
    path=/Client*/EntryGroup*
    interface=org.freedesktop.Avahi.EntryGroup
    member=Add{Service,ServiceSubtype,Address,Record}
    peer=(name=org.freedesktop.Avahi, label=###SLOT_SECURITY_TAGS###),

dbus (receive)
    bus=system
    path=/Client*/EntryGroup*
    interface=org.freedesktop.Avahi.EntryGroup
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type avahiControlInterface struct{}

func (iface *avahiControlInterface) Name() string {
   return "avahi-control"
}

func (iface *avahiControlInterface) MetaData() interfaces.MetaData {
   return interfaces.MetaData{
       Summary: avahiControlSummary,
       ImplicitOnClassic: true,
       BaseDeclarationSlots: avahiControlBaseDeclarationSlots,
   }
}

func (iface *avahiControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
    old := "###SLOT_SECURITY_TAGS###"
    var new string
    if release.OnClassic {
        // If we're running on classic Avahi will be part
        // of the OS snap and will run unconfined.
        new = "unconfined"
    } else {
        new = slotAppLabelExpr(slot)
    }
    snippet := strings.Replace(avahiObserveConnectedPlugAppArmor + avahiControlConnectedPlugAppArmor, old, new, -1)
    spec.AddSnippet(snippet)
    return nil
}

func (iface *avahiControlInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
    spec.AddSnippet(avahiObservePermanentSlotAppArmor)
    return nil
}

func (iface *avahiControlInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
    old := "###PLUG_SECURITY_TAGS###"
    new := plugAppLabelExpr(plug)
    snippet := strings.Replace(avahiObserveConnectedSlotAppArmor + avahiControlConnectedSlotAppArmor, old, new, -1)
    spec.AddSnippet(snippet)
    return nil
}

func (iface *avahiControlInterface) DBusPermanentSlot(spec *dbus.Specification, slot *interfaces.Slot) error {
    spec.AddSnippet(avahiObservePermanentSlotDBus)
    return nil
}

func (iface *avahiControlInterface) SanitizePlug(plug *interfaces.Plug) error {
    return nil
}

func (iface *avahiControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
    return nil
}

func (iface *avahiControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
    // allow what declarations allowed
    return true
}

func init() {
    registerIface(&avahiControlInterface{})
}
