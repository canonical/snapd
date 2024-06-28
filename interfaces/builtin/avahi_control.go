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
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/snap"
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

const avahiControlSummary = `allows control over service discovery on a local network via the mDNS/DNS-SD protocol suite`

const avahiControlConnectedSlotAppArmor = `
# Description: allows configuration of service discovery via mDNS/DNS-SD
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

func (iface *avahiControlInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              avahiControlSummary,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: avahiControlBaseDeclarationSlots,
	}
}

func (iface *avahiControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	// If we're running on classic, Avahi may be installed either as a snap of
	// as part of the OS. If it is part of the OS, it will not have a security
	// label like it would when installed as a snap.
	if implicitSystemConnectedSlot(slot) {
		// avahi from the OS is typically unconfined but known to sometimes be confined
		// with stock apparmor 2.13.2+ profiles the label is avahi-daemon
		new = "\"{unconfined,/usr/sbin/avahi-daemon,avahi-daemon}\""
	} else {
		new = slot.LabelExpression()
	}
	// avahi-control implies avahi-observe, so add snippets for both here
	snippet := strings.Replace(avahiObserveConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	snippet = strings.Replace(avahiControlConnectedPlugAppArmor, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *avahiControlInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemPermanentSlot(slot) {
		// NOTE: this is using avahi-observe permanent slot as it contains
		// base declarations for running as the avahi service.
		spec.AddSnippet(avahiObservePermanentSlotAppArmor)
	}
	return nil
}

func (iface *avahiControlInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := plug.LabelExpression()
		// avahi-control implies avahi-observe, so add snippets for both here
		snippet := strings.Replace(avahiObserveConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
		snippet = strings.Replace(avahiControlConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *avahiControlInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemPermanentSlot(slot) {
		// NOTE: this is using avahi-observe permanent slot as it contains
		// base declarations for running as the avahi service.
		spec.AddSnippet(avahiObservePermanentSlotDBus)
	}
	return nil
}

func (iface *avahiControlInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&avahiControlInterface{})
}
