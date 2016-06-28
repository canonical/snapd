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
	//"reflect"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
)

var dbusBindPermanentSlotAppArmor = []byte(`
# Description: Allow owning a name on DBus public bus
# Usage: common

# DBus accesses
# FIXME
#include <abstractions/dbus-session-strict>

# register and bind to a well-known DBus name
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

# TODO: allow unconfined clients talk to us
`)

var dbusBindPermanentSlotSecComp = []byte(`
# Description: Allow owning a name on DBus public bus
# Usage: common

getsockname
recvmsg
sendmsg
sendto
`)

var dbusBindConnectedSlotAppArmor = []byte(`
# Description: Allow DBus consumer to connect to us
# FIXME
#include <abstractions/dbus-session-strict>

# Communicate with the well-known named DBus service
dbus (receive, send)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    peer=(label=###PLUG_SECURITY_TAGS###),
`)


var dbusBindConnectedPlugAppArmor = []byte(`
# Description: Allow connecting to DBus service on well-known name
# Usage: common

# DBus accesses
#include <abstractions/dbus-session-strict>

# Communicate with the well-known named DBus service
dbus (receive, send)
    bus=###DBUS_BIND_BUS###
    path=###DBUS_BIND_PATH###
    peer=(label=###SLOT_SECURITY_TAGS###),
`)

var dbusBindConnectedPlugSecComp = []byte(`
getsockname
recvmsg
sendmsg
sendto
`)


type DbusBindInterface struct{}

func (iface *DbusBindInterface) Name() string {
	return "dbus-bind"
}

func (iface *DbusBindInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusBindInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###DBUS_BIND_BUS###")
		new := []byte(slot.Attrs["bus"].(string))
		snippet := bytes.Replace(dbusBindConnectedPlugAppArmor, old, new, -1)

		old = []byte("###DBUS_BIND_NAME###")
		new = []byte(slot.Attrs["name"].(string))
		snippet = bytes.Replace(snippet, old, new, -1)

		// convert name to AppArmor dbus path
		dot_re := regexp.MustCompile("\\.")
		var path_buf bytes.Buffer
		path_buf.WriteString(`"/`)
		path_buf.WriteString(dot_re.ReplaceAllString(slot.Attrs["name"].(string), "/"))
		path_buf.WriteString(`{,/**}"`)

		old = []byte("###DBUS_BIND_PATH###")
		new = path_buf.Bytes()
		snippet = bytes.Replace(snippet, old, new, -1)

		old = []byte("###SLOT_SECURITY_TAGS###")
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)

		//fmt.Printf("CONNECTED PLUG:\n %s\n", snippet)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return dbusBindPermanentSlotSecComp, nil
	case interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusBindInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###DBUS_BIND_BUS###")
		new := []byte(slot.Attrs["bus"].(string))
		snippet := bytes.Replace(dbusBindPermanentSlotAppArmor, old, new, -1)

		old = []byte("###DBUS_BIND_NAME###")
		new = []byte(slot.Attrs["name"].(string))
		snippet = bytes.Replace(snippet, old, new, -1)

		// convert name to AppArmor dbus path
		dot_re := regexp.MustCompile("\\.")
		var path_buf bytes.Buffer
		path_buf.WriteString(`"/`)
		path_buf.WriteString(dot_re.ReplaceAllString(slot.Attrs["name"].(string), "/"))
		path_buf.WriteString(`{,/**}"`)

		old = []byte("###DBUS_BIND_PATH###")
		new = path_buf.Bytes()
		snippet = bytes.Replace(snippet, old, new, -1)

		//fmt.Printf("PERMANENT SLOT:\n %s\n", snippet)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return dbusBindPermanentSlotSecComp, nil
	case interfaces.SecurityDBus, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusBindInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###DBUS_BIND_BUS###")
		new := []byte(slot.Attrs["bus"].(string))
		snippet := bytes.Replace(dbusBindConnectedSlotAppArmor, old, new, -1)

		// TODO: break this out
		// convert name to AppArmor dbus path
		dot_re := regexp.MustCompile("\\.")
		var path_buf bytes.Buffer
		path_buf.WriteString(`"/`)
		path_buf.WriteString(dot_re.ReplaceAllString(slot.Attrs["name"].(string), "/"))
		path_buf.WriteString(`{,/**}"`)

		old = []byte("###DBUS_BIND_PATH###")
		new = path_buf.Bytes()
		snippet = bytes.Replace(snippet, old, new, -1)

		old = []byte("###PLUG_SECURITY_TAGS###")
		new = plugAppLabelExpr(plug)
		snippet = bytes.Replace(snippet, old, new, -1)

		//fmt.Printf("CONNECTED SLOT:\n %s\n", snippet)
		return snippet, nil
	case interfaces.SecurityDBus, interfaces.SecuritySecComp, interfaces.SecurityUDev, interfaces.SecurityMount:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DbusBindInterface) SanitizePlug(slot *interfaces.Plug) error {
	return nil
}

func (iface *DbusBindInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	fmt.Printf("%#+v\n", slot.Attrs)
	fmt.Printf("%#+v\n", slot.Attrs["session"])
	//fmt.Printf("%d\n", len(slot.Attrs["session"].(string)))

	for p, ok := range slot.Attrs.(interface{}) {
		fmt.Printf("%#+v\n", p)

	}
	/*
	for i, n := range names {
		fmt.Printf("bus=%s\n", n)
		buses[i] = slot.Attrs[n].(string)
	}
	fmt.Printf("%#+v\n", buses)
	*/

	/*
	stringSlice, found := slot.Attrs["session"].([]string)
	if !found {
		fmt.Printf("couldn't find []string\n")
	}
	stringSlice, found := slot.Attrs["session"].(string)
	if !found {
		fmt.Printf("couldn't find []string\n")
	}
	stringSlice, found := slot.Attrs["session"].([]string)
	if !found {
		fmt.Printf("couldn't find []string\n")
	}

	if !found {
		fmt.Printf("slot.Attrs=%+v\n", slot.Attrs)
		fmt.Printf("slot.Attrs[session]=%+v\n", slot.Attrs["session"])
		fmt.Printf("slot.Attrs[session]=%s\n", (slot.Attrs["session"]).([]string))
		fmt.Printf("couldn't find []string\n")
		fmt.Printf("type=%s\n", reflect.TypeOf(slot.Attrs["session"]))
		return nil
	}
	fmt.Printf("stringSlice=%+v\n", stringSlice)

	names := []string{"session", "system"}
	vals := make([]string, len(names))
	for i, v := range names {
		fmt.Println(i)
		fmt.Println(v)
		vals[i] = slot.Attrs[v].(string)
		fmt.Println(vals[i])
	}
	*/
	/*
	for _, val := range vals {
		//fmt.Printf("  %s\n", val)
		fmt.Println(val)
	}

	return nil

	names := []string{"session", "system"}
	vals := make([]interface{}, len(names))
	for i, v := range names {
		vals[i] = v
		fmt.Println(v)
	}
	for _, val := range vals {
		//fmt.Printf("  %s\n", val)
		fmt.Println(val)
	}

	return nil

	for bus := range slot.Attrs {
		fmt.Printf("bus=%s\n", bus)
		fmt.Printf("bus_names=%s\n", slot.Attrs[bus])
		fmt.Printf("len=%d\n", len(slot.Attrs[bus]))
	}
	*/

	return nil
	for bus, bus_names := range slot.Attrs {
		fmt.Printf("bus=%s\n", bus)
		fmt.Printf("bus_names=%+v\n", bus_names)
		if bus != "session" && bus != "system" {
			return fmt.Errorf("bus must be one of 'session' or 'system'")
		}
		for _, dbus_name := range bus_names.([]string) {
			fmt.Printf("  %s\n", dbus_name)
		}
	}

	fmt.Printf("HERE\n")
	return nil

	// verify that we have both the bus and the name and they are formatted
	// properly
	bus, ok := slot.Attrs["bus"].(string)
	if !ok || len(bus) == 0 {
		return fmt.Errorf("bus must be set")
	}
	if strings.Compare(bus, "session") != 0 && strings.Compare(bus, "system") != 0 {
		return fmt.Errorf("bus must be one of 'session' or 'system'")
	}

	// https://dbus.freedesktop.org/doc/dbus-specification.html#message-protocol-names
	dbus_name, ok := slot.Attrs["name"].(string)
	if !ok || len(dbus_name) == 0 {
		return fmt.Errorf("bus name must be set")
	} else if len(dbus_name) > 255 {
		return fmt.Errorf("bus name is too long (must be <= 255)")
	}

	validBusName := regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9_-]*(\\.[a-zA-Z0-9_-]+)+$")
	if !validBusName.MatchString(dbus_name) {
		return fmt.Errorf("invalid bus name: %q", dbus_name)
	}

	return nil
}

func (iface *DbusBindInterface) AutoConnect() bool {
	return true
}
