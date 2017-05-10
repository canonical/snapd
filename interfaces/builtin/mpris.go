// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/release"
)

const mprisPermanentSlotAppArmor = `
# Description: Allow operating as an MPRIS player.

# DBus accesses
#include <abstractions/dbus-session-strict>

# https://specifications.freedesktop.org/mpris-spec/latest/
# allow binding to the well-known DBus mpris interface based on the snap's name
dbus (bind)
    bus=session
    name="org.mpris.MediaPlayer2.###MPRIS_NAME###{,.*}",

# register as a player
dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=system
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    interface=org.freedesktop.DBus.Properties
    member="{GetAll,PropertiesChanged}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    interface="org.mpris.MediaPlayer2{,.Player}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

# we can always connect to ourselves
dbus (receive)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=@{profile_name}),
`

const mprisConnectedSlotAppArmor = `
# Allow connected clients to interact with the player
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Properties
    path=/org/mpris/MediaPlayer2
    peer=(label=###PLUG_SECURITY_TAGS###),
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=###PLUG_SECURITY_TAGS###),

dbus (receive)
    bus=session
    interface="org.mpris.MediaPlayer2{,.*}"
    path=/org/mpris/MediaPlayer2
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const mprisConnectedSlotAppArmorClassic = `
# Allow unconfined clients to interact with the player on classic
dbus (receive)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=unconfined),
dbus (receive)
    bus=session
    interface=org.freedesktop.DBus.Introspectable
    peer=(label=unconfined),
`

const mprisConnectedPlugAppArmor = `
# Description: Allow connecting to an MPRIS player.

#include <abstractions/dbus-session-strict>

# Find the mpris player
dbus (send)
    bus=session
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus.Introspectable
    peer=(name="org.freedesktop.DBus", label="unconfined"),
dbus (send)
    bus=session
    path=/{,org,org/mpris,org/mpris/MediaPlayer2}
    interface=org.freedesktop.DBus.Introspectable
    peer=(name="org.freedesktop.DBus", label="unconfined"),
# This reveals all names on the session bus
dbus (send)
    bus=session
    path=/
    interface=org.freedesktop.DBus
    member=ListNames
    peer=(name="org.freedesktop.DBus", label="unconfined"),

# Communicate with the mpris player
dbus (send)
    bus=session
    path=/org/mpris/MediaPlayer2
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type MprisInterface struct{}

func (iface *MprisInterface) Name() string {
	return "mpris"
}

func (iface *MprisInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###SLOT_SECURITY_TAGS###"
	new := slotAppLabelExpr(slot)
	spec.AddSnippet(strings.Replace(mprisConnectedPlugAppArmor, old, new, -1))
	return nil
}

func (iface *MprisInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.Slot) error {
	name, err := iface.getName(slot.Attrs)
	if err != nil {
		return err
	}

	old := "###MPRIS_NAME###"
	new := name
	spec.AddSnippet(strings.Replace(mprisPermanentSlotAppArmor, old, new, -1))
	// on classic, allow unconfined remotes to control the player
	// (eg, indicator-sound)
	if release.OnClassic {
		spec.AddSnippet(mprisConnectedSlotAppArmorClassic)
	}
	return nil
}

func (iface *MprisInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###PLUG_SECURITY_TAGS###"
	new := plugAppLabelExpr(plug)
	spec.AddSnippet(strings.Replace(mprisConnectedSlotAppArmor, old, new, -1))
	return nil
}

func (iface *MprisInterface) getName(attribs map[string]interface{}) (string, error) {
	// default to snap name if 'name' attribute not set
	mprisName := "@{SNAP_NAME}"
	for attr := range attribs {
		if attr != "name" {
			return "", fmt.Errorf("unknown attribute '%s'", attr)
		}
		raw, ok := attribs[attr]
		if !ok {
			return "", fmt.Errorf("cannot find attribute %q", attr)
		}
		name, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("name element %v is not a string", raw)
		}

		validDBusElement := regexp.MustCompile("^[a-zA-Z0-9_-]*$")
		if !validDBusElement.MatchString(name) {
			return "", fmt.Errorf("invalid name element: %q", name)
		}
		mprisName = name
	}
	return mprisName, nil
}

func (iface *MprisInterface) SanitizePlug(slot *interfaces.Plug) error {
	return nil
}

func (iface *MprisInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	_, err := iface.getName(slot.Attrs)
	return err
}

func (iface *MprisInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func (iface *MprisInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *MprisInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
