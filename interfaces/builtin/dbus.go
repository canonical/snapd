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
	"regexp"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/release"
)

// Split this out since we only need these rules once per app
const dbusPermanentSlotAppArmorShared = `
# Description: Allow owning a name on DBus public bus

#include <abstractions/###DBUS_BIND_ABSTRACTION###>

# register on DBus
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
`

// These rules are needed for each well-known name for the app
const dbusPermanentSlotAppArmorIndividual = `
# bind to a well-known DBus name: ###DBUS_BIND_NAME###
dbus (bind)
    bus=###DBUS_BIND_BUS###
    name=###DBUS_BIND_NAME###,

# Allow us to talk to dbus-daemon
dbus (receive)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    interface=org.freedesktop.DBus.Properties
    peer=(name=org.freedesktop.DBus, label=unconfined),
`

const dbusPermanentSlotAppArmorIndividualClassic = `
# allow unconfined clients talk to ###DBUS_BIND_NAME### on classic
dbus (receive)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    peer=(label=unconfined),
`

const dbusPermanentSlotSecComp = `
# Description: Allow owning a name on DBus public bus
getsockname
recvmsg
sendmsg
sendto
`

type DbusInterface struct{}

func (iface *DbusInterface) Name() string {
	return "dbus"
}

func (iface *DbusInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *DbusInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *DbusInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	bus, name, err := iface.getAttribs(slot.Attrs)
	if err != nil {
		return nil, err
	}
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		snippets := bytes.NewBufferString("")

		// common permanent slot policy
		abstraction, err := getAppArmorAbstraction(bus)
		if err != nil {
			return nil, err
		}
		old := []byte("###DBUS_BIND_ABSTRACTION###")
		new := []byte(abstraction)
		snippet := bytes.Replace([]byte(dbusPermanentSlotAppArmorShared), old, new, -1)
		snippets.Write(snippet)

		// well-known DBus name-specific permanent slot policy
		snippet = getAppArmorIndividualSnippet([]byte(dbusPermanentSlotAppArmorIndividual), bus, name)
		snippets.Write(snippet)

		if release.OnClassic {
			// classic-only policy
			snippet = getAppArmorIndividualSnippet([]byte(dbusPermanentSlotAppArmorIndividualClassic), bus, name)
			snippets.Write(snippet)
		}
		//fmt.Printf("DEBUG - PERMANENT SLOT:\n %s\n", snippets.Bytes())
		return snippets.Bytes(), nil
	case interfaces.SecuritySecComp:
		return []byte(dbusPermanentSlotSecComp), nil
	}
	return nil, nil
}

func (iface *DbusInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// Obtain yaml-specified bus well-known name
func (iface *DbusInterface) getAttribs(attribs map[string]interface{}) (string, string, error) {
	bus := ""
	name := ""
	for attr := range attribs {
		if attr != "bus" && attr != "name" {
			return "", "", fmt.Errorf("unknown attribute '%s'", attr)
		}

		raw, ok := attribs[attr]
		if !ok {
			return "", "", fmt.Errorf("cannot find attribute %q", attr)
		}
		val, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("element %v for '%s' is not a string", raw, attr)
		}

		if attr == "bus" {
			bus = val
			if bus != "session" && bus != "system" {
				return "", "", fmt.Errorf("bus '%s' must be one of 'session' or 'system'", bus)
			}
		} else if attr == "name" {
			name = val
			err := interfaces.ValidateDBusBusName(name)
			if err != nil {
				return "", "", err
			}
		}
	}

	if bus == "" {
		return "", "", fmt.Errorf("required attribute 'bus' not specified")
	} else if name == "" {
		return "", "", fmt.Errorf("required attribute 'name' not specified")
	}

	return bus, name, nil
}

// Determine AppArmor dbus abstraction to use based on bus
func getAppArmorAbstraction(bus string) (string, error) {
	var abstraction string
	if bus == "system" {
		abstraction = "dbus-strict"
	} else if bus == "session" {
		abstraction = "dbus-session-strict"
	} else {
		return "", fmt.Errorf("unknown abstraction for specified bus '%q'", bus)
	}
	return abstraction, nil
}

// Calculate individual snippet policy based on bus and name
func getAppArmorIndividualSnippet(policy []byte, bus string, name string) []byte {
	old := []byte("###DBUS_BIND_BUS###")
	new := []byte(bus)
	snippet := bytes.Replace(policy, old, new, -1)

	old = []byte("###DBUS_BIND_NAME###")
	new = []byte(name)
	snippet = bytes.Replace(snippet, old, new, -1)

	// convert name to AppArmor dbus path (eg org.foo' to '/org/foo{,/**}')
	dot_re := regexp.MustCompile("\\.")
	var path_buf bytes.Buffer
	path_buf.WriteString(`"/`)
	path_buf.WriteString(dot_re.ReplaceAllString(name, "/"))
	path_buf.WriteString(`{,/**}"`)

	old = []byte("###DBUS_BIND_PATH###")
	new = path_buf.Bytes()
	snippet = bytes.Replace(snippet, old, new, -1)

	return snippet
}

func (iface *DbusInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	_, _, err := iface.getAttribs(plug.Attrs)
	return err
}

func (iface *DbusInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	_, _, err := iface.getAttribs(slot.Attrs)
	return err
}

// Since we only implement the permanent slot side, this is meaningless but
// we have to supply the method, so set it to something safe.
func (iface *DbusInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
