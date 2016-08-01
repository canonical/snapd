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
var dbusAppPermanentSlotAppArmorShared = []byte(`
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
`)

// These rules are needed for each well-known name for the app
var dbusAppPermanentSlotAppArmorIndividual = []byte(`
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
`)

var dbusAppPermanentSlotAppArmorIndividualClassic = []byte(`
# allow unconfined clients talk to ###DBUS_BIND_NAME### on classic
dbus (receive)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    peer=(label=unconfined),
`)

var dbusAppPermanentSlotSecComp = []byte(`
# Description: Allow owning a name on DBus public bus

getsockname
recvmsg
sendmsg
sendto
`)

type DbusAppInterface struct{}

func (iface *DbusAppInterface) Name() string {
	return "dbus-app"
}

func (iface *DbusAppInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusAppInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusAppInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	dbusAppBusNames, err := iface.GetBusNames(slot.Attrs)
	if err != nil {
		return nil, err
	}
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		var snippets bytes.Buffer
		snippets.WriteString(``)

		for bus, names := range dbusAppBusNames {
			// common permanent slot policy
			abstraction, err := getAppArmorAbstraction(bus)
			if err != nil {
				return nil, err
			}
			old := []byte("###DBUS_BIND_ABSTRACTION###")
			new := []byte(abstraction)
			snippet := bytes.Replace(dbusAppPermanentSlotAppArmorShared, old, new, -1)
			snippets.Write(snippet)

			for _, name := range names {
				// well-known DBus name-specific permanent slot
				// policy
				snippet := getAppArmorIndividualSnippet(dbusAppPermanentSlotAppArmorIndividual, bus, name)
				snippets.Write(snippet)

				if release.OnClassic {
					// classic-only policy
					snippet := getAppArmorIndividualSnippet(dbusAppPermanentSlotAppArmorIndividualClassic, bus, name)
					snippets.Write(snippet)
				}
			}
		}
		//fmt.Printf("DEBUG - PERMANENT SLOT:\n %s\n", snippets.Bytes())
		return snippets.Bytes(), nil
	case interfaces.SecuritySecComp:
		return dbusAppPermanentSlotSecComp, nil
	case interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusAppInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// Obtain yaml-specified DBus well-known names by bus
func (iface *DbusAppInterface) GetBusNames(attribs map[string]interface{}) (map[string][]string, error) {
	busNames := make(map[string][]string)
	for attr := range attribs {
		bus := attr
		if bus != "session" && bus != "system" {
			return nil, fmt.Errorf("bus must be one of 'session' or 'system'")
		}
		busNamesList, ok := attribs[bus].([]interface{})
		if !ok {
			return nil, fmt.Errorf("bus attribute is not a list")
		}

		for _, item := range busNamesList {
			busName, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("session element is not a string")
			}

			// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
			if !ok || len(busName) == 0 {
				return nil, fmt.Errorf("bus name must be set")
			} else if len(busName) > 255 {
				return nil, fmt.Errorf("bus name is too long (must be <= 255)")
			}

			validBusName := regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9_-]*(\\.[a-zA-Z0-9_-]+)+$")
			if !validBusName.MatchString(busName) {
				return nil, fmt.Errorf("invalid bus name: %q", busName)
			}

			busNames[bus] = append(busNames[bus], busName)
		}
	}

	return busNames, nil
}

// verify that name for bus is in list
func (iface *DbusAppInterface) verifyNameInAttributes(bus string, name string, attribs map[string]interface{}) bool {
	otherBusNames, err := iface.GetBusNames(attribs)
	if err != nil {
		return false
	}

	if otherNames, ok := otherBusNames[bus]; ok {
		for _, otherName := range otherNames {
			if name == otherName {
				return true
			}
		}
	}

	return false
}

// Determine AppArmor dbus abstraction to use based on bus
func getAppArmorAbstraction(bus string) (string, error) {
	var abstraction string
	if bus == "system" {
		abstraction = "dbus-strict"
	} else if bus == "session" {
		abstraction = "dbus-session-strict"
	} else {
		return "", fmt.Errorf("unknown abstraction for specified bus '%s'", bus)
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

	// convert name to AppArmor dbus path
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

func (iface *DbusAppInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	_, err := iface.GetBusNames(plug.Attrs)
	return err
}

func (iface *DbusAppInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	_, err := iface.GetBusNames(slot.Attrs)
	return err
}

func (iface *DbusAppInterface) AutoConnect() bool {
	return false
}
