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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/release"
)

const dbusPermanentSlotAppArmor = `
# Description: Allow owning a name on DBus public bus

#include <abstractions/###DBUS_ABSTRACTION###>

# register on DBus
dbus (send)
    bus=###DBUS_BUS###
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="{Request,Release}Name"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=###DBUS_BUS###
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionUnix{ProcessID,User}"
    peer=(name=org.freedesktop.DBus, label=unconfined),

dbus (send)
    bus=###DBUS_BUS###
    path=/org/freedesktop/DBus
    interface=org.freedesktop.DBus
    member="GetConnectionCredentials"
    peer=(name=org.freedesktop.DBus, label=unconfined),

# bind to a well-known DBus name: ###DBUS_NAME###
dbus (bind)
    bus=###DBUS_BUS###
    name=###DBUS_NAME###,

# For KDE applications, also support alternation since they use org.kde.foo-PID
# as their 'well-known' name. snapd does not allow declaring a 'well-known'
# name that ends with '-[0-9]+', so this is ok.
dbus (bind)
    bus=###DBUS_BUS###
    name=###DBUS_NAME###-[1-9]{,[0-9]}{,[0-9]}{,[0-9]}{,[0-9]}{,[0-9]},

# Allow us to talk to dbus-daemon
dbus (receive)
    bus=###DBUS_BUS###
    path=###DBUS_PATH###
    peer=(name=org.freedesktop.DBus, label=unconfined),
dbus (send)
    bus=###DBUS_BUS###
    path=###DBUS_PATH###
    interface=org.freedesktop.DBus.Properties
    peer=(name=org.freedesktop.DBus, label=unconfined),
`

const dbusPermanentSlotAppArmorClassic = `
# allow unconfined clients to introspect us on classic
dbus (receive)
    bus=###DBUS_BUS###
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),

# allow us to respond to unconfined clients via ###DBUS_INTERFACE###
# on classic (send should be handled via another snappy interface).
dbus (receive)
    bus=###DBUS_BUS###
    interface=###DBUS_INTERFACE###
    peer=(label=unconfined),

# allow us to respond to unconfined clients via ###DBUS_PATH### (eg,
# org.freedesktop.*, org.gtk.Application, etc) on classic (send should be
# handled via another snappy interface).
dbus (receive)
    bus=###DBUS_BUS###
    path=###DBUS_PATH###
    peer=(label=unconfined),
`

const dbusPermanentSlotSecComp = `
# Description: Allow owning a name on DBus public bus
getsockname
recvmsg
sendmsg
sendto
`

const dbusPermanentSlotDBus = `
<policy user="root">
    <allow own="###DBUS_NAME###"/>
    <allow send_destination="###DBUS_NAME###"/>
</policy>
<policy context="default">
    <allow send_destination="###DBUS_NAME###"/>
</policy>
`

const dbusConnectedSlotAppArmor = `
# allow snaps to introspect us. This allows clients to introspect all
# DBus interfaces of this service (but not use them).
dbus (receive)
    bus=###DBUS_BUS###
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###PLUG_SECURITY_TAGS###),

# allow connected snaps to all paths via ###DBUS_INTERFACE###
dbus (receive, send)
    bus=###DBUS_BUS###
    interface=###DBUS_INTERFACE###
    peer=(label=###PLUG_SECURITY_TAGS###),

# allow connected snaps to all interfaces via ###DBUS_PATH### (eg,
# org.freedesktop.*, org.gtk.Application, etc) to allow full integration with
# connected snaps.
dbus (receive, send)
    bus=###DBUS_BUS###
    path=###DBUS_PATH###
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const dbusConnectedPlugAppArmor = `
#include <abstractions/###DBUS_ABSTRACTION###>

# allow snaps to introspect the slot servive. This allows us to introspect
# all DBus interfaces of the service (but not use them).
dbus (send)
    bus=###DBUS_BUS###
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=###SLOT_SECURITY_TAGS###),

# allow connected snaps to ###DBUS_NAME###
dbus (receive, send)
    bus=###DBUS_BUS###
    peer=(name=###DBUS_NAME###, label=###SLOT_SECURITY_TAGS###),
# For KDE applications, also support alternation since they use org.kde.foo-PID
# as their 'well-known' name. snapd does not allow ###DBUS_NAME### to end with
# '-[0-9]+', so this is ok.
dbus (receive, send)
    bus=###DBUS_BUS###
    peer=(name="###DBUS_NAME###-[1-9]{,[0-9]}{,[0-9]}{,[0-9]}{,[0-9]}{,[0-9]}", label=###SLOT_SECURITY_TAGS###),

# allow connected snaps to all paths via ###DBUS_INTERFACE### to allow full
# integration with connected snaps.
dbus (receive, send)
    bus=###DBUS_BUS###
    interface=###DBUS_INTERFACE###
    peer=(label=###SLOT_SECURITY_TAGS###),

# allow connected snaps to all interfaces via ###DBUS_PATH### (eg,
# org.freedesktop.*, org.gtk.Application, etc) to allow full integration with
# connected snaps.
dbus (receive, send)
    bus=###DBUS_BUS###
    path=###DBUS_PATH###
    peer=(label=###SLOT_SECURITY_TAGS###),
`

const dbusConnectedPlugSecComp = `
getsockname
recvmsg
sendmsg
sendto
`

type DbusInterface struct{}

func (iface *DbusInterface) Name() string {
	return "dbus"
}

// Obtain yaml-specified bus well-known name
func (iface *DbusInterface) getAttribs(attribs map[string]interface{}) (string, string, error) {
	// bus attribute
	bus, ok := attribs["bus"].(string)
	if !ok {
		return "", "", fmt.Errorf("cannot find attribute 'bus'")
	}

	if bus != "session" && bus != "system" {
		return "", "", fmt.Errorf("bus '%s' must be one of 'session' or 'system'", bus)
	}

	// name attribute
	name, ok := attribs["name"].(string)
	if !ok {
		return "", "", fmt.Errorf("cannot find attribute 'name'")
	}

	err := interfaces.ValidateDBusBusName(name)
	if err != nil {
		return "", "", err
	}

	// snapd has AppArmor rules (see above) allowing binds to busName-PID
	// so to avoid overlap with different snaps (eg, busName running as PID
	// 123 and busName-123), don't allow busName to end with -PID. If that
	// rule is removed, this limitation can be lifted.
	invalidSnappyBusName := regexp.MustCompile("-[0-9]+$")
	if invalidSnappyBusName.MatchString(name) {
		return "", "", fmt.Errorf("DBus bus name must not end with -NUMBER")
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
func getAppArmorSnippet(policy []byte, bus string, name string) []byte {
	old := []byte("###DBUS_BUS###")
	new := []byte(bus)
	snippet := bytes.Replace(policy, old, new, -1)

	old = []byte("###DBUS_NAME###")
	new = []byte(name)
	snippet = bytes.Replace(snippet, old, new, -1)

	// convert name to AppArmor dbus path (eg 'org.foo' to '/org/foo{,/**}')
	var pathBuf bytes.Buffer
	pathBuf.WriteString(`"/`)
	pathBuf.WriteString(strings.Replace(name, ".", "/", -1))
	pathBuf.WriteString(`{,/**}"`)

	old = []byte("###DBUS_PATH###")
	new = pathBuf.Bytes()
	snippet = bytes.Replace(snippet, old, new, -1)

	// convert name to AppArmor dbus interface (eg, 'org.foo' to 'org.foo{,.*}')
	var ifaceBuf bytes.Buffer
	ifaceBuf.WriteString(`"`)
	ifaceBuf.WriteString(name)
	ifaceBuf.WriteString(`{,.*}"`)

	old = []byte("###DBUS_INTERFACE###")
	new = ifaceBuf.Bytes()
	snippet = bytes.Replace(snippet, old, new, -1)

	return snippet
}

func (iface *DbusInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *DbusInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	bus, name, err := iface.getAttribs(plug.Attrs)
	if err != nil {
		return nil, err
	}

	busSlot, nameSlot, err := iface.getAttribs(slot.Attrs)
	if err != nil {
		return nil, err
	}

	// ensure that we only connect to slot with matching attributes
	if bus != busSlot || name != nameSlot {
		return nil, nil
	}

	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// well-known DBus name-specific connected plug policy
		snippet := getAppArmorSnippet([]byte(dbusConnectedPlugAppArmor), bus, name)

		// abstraction policy
		abstraction, err := getAppArmorAbstraction(bus)
		if err != nil {
			return nil, err
		}

		old := []byte("###DBUS_ABSTRACTION###")
		new := []byte(abstraction)
		snippet = bytes.Replace(snippet, old, new, -1)

		old = []byte("###SLOT_SECURITY_TAGS###")
		new = slotAppLabelExpr(slot)
		snippet = bytes.Replace(snippet, old, new, -1)

		return snippet, nil
	case interfaces.SecuritySecComp:
		return []byte(dbusConnectedPlugSecComp), nil
	}
	return nil, nil
}

func (iface *DbusInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		bus, name, err := iface.getAttribs(slot.Attrs)
		if err != nil {
			return nil, err
		}

		snippets := bytes.NewBufferString("")

		// well-known DBus name-specific permanent slot policy
		snippet := getAppArmorSnippet([]byte(dbusPermanentSlotAppArmor), bus, name)

		// abstraction policy
		abstraction, err := getAppArmorAbstraction(bus)
		if err != nil {
			return nil, err
		}

		old := []byte("###DBUS_ABSTRACTION###")
		new := []byte(abstraction)
		snippet = bytes.Replace(snippet, old, new, -1)

		snippets.Write(snippet)

		if release.OnClassic {
			// classic-only policy
			snippets.Write(getAppArmorSnippet([]byte(dbusPermanentSlotAppArmorClassic), bus, name))
		}

		return snippets.Bytes(), nil
	case interfaces.SecuritySecComp:
		return []byte(dbusPermanentSlotSecComp), nil
	case interfaces.SecurityDBus:
		bus, name, err := iface.getAttribs(slot.Attrs)
		if err != nil {
			return nil, err

		}

		// only system services need bus policy
		if bus != "system" {
			return nil, nil
		}

		old := []byte("###DBUS_NAME###")
		new := []byte(name)
		snippet := bytes.Replace([]byte(dbusPermanentSlotDBus), old, new, -1)

		return []byte(snippet), nil
	}
	return nil, nil
}

func (iface *DbusInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	bus, name, err := iface.getAttribs(slot.Attrs)
	if err != nil {
		return nil, err
	}

	busPlug, namePlug, err := iface.getAttribs(plug.Attrs)
	if err != nil {
		return nil, err
	}

	// ensure that we only connect to slot with matching attributes. This
	// makes sure that the security policy is correct, but does not ensure
	// that 'snap interfaces' is correct.
	// TODO: we can fix the 'snap interfaces' issue when interface/policy
	// checkers when they are available
	if bus != busPlug || name != namePlug {
		return nil, nil
	}

	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// well-known DBus name-specific connected slot policy
		snippet := getAppArmorSnippet([]byte(dbusConnectedSlotAppArmor), bus, name)

		old := []byte("###PLUG_SECURITY_TAGS###")
		new := plugAppLabelExpr(plug)
		snippet = bytes.Replace(snippet, old, new, -1)

		return snippet, nil
	}
	return nil, nil
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

func (iface *DbusInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *DbusInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

func (iface *DbusInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
