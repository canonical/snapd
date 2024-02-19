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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const dbusSummary = `allows owning a specific name on DBus`

const dbusBaseDeclarationSlots = `
  dbus:
    allow-installation:
      slot-snap-type:
        - app
    deny-connection:
      slot-attributes:
        name: .+
    deny-auto-connection: true
`

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

# For KDE applications and some other cases, also support alternation for:
# - using org.kde.foo-PID as the 'well-known' name
# - using org.foo.cmd_<num>_<num> as the 'well-known' name
# Note, snapd does not allow declaring a 'well-known' name that ends with
# '-[0-9]+' or that contains '_'. Parallel installs of DBus services aren't
# supported at this time, but if they were, this could allow a parallel
# install'swell-known name to overlap with the normal install.
dbus (bind)
    bus=###DBUS_BUS###
    name=###DBUS_NAME###{_,-}[1-9]{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9]}{,_[1-9]{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9_]}{,[0-9]}},

# For Firefox, support using org.mozilla.firefox.<id> as the 'well-known' name
# where <id> is the base64-encoded profile name.
# See https://bugzilla.mozilla.org/1441894 for a discussion and details.
dbus (bind)
    bus=###DBUS_BUS###
    name="###DBUS_NAME###.*",

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

# Allow us to introspect org.freedesktop.DBus (needed by pydbus)
dbus (send)
    bus=###DBUS_BUS###
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
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

const dbusPermanentSlotDBus = `
<policy user="root">
    <allow own="###DBUS_NAME###"/>
    <allow send_destination="###DBUS_NAME###"/>
</policy>
<policy context="default">
    <allow send_destination="###DBUS_NAME###"/>
</policy>
`

const dbusPermanentSlotSecComp = `
# Description: Allow owning a name and listening on DBus public bus
listen
accept
accept4
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

type dbusInterface struct{}

func (iface *dbusInterface) Name() string {
	return "dbus"
}

func (iface *dbusInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              dbusSummary,
		BaseDeclarationSlots: dbusBaseDeclarationSlots,
	}
}

// snapd has AppArmor rules (see above) allowing binds to busName-PID
// so to avoid overlap with different snaps (eg, busName running as PID
// 123 and busName-123), don't allow busName to end with -PID. If that
// rule is removed, this limitation can be lifted.
var isInvalidSnappyBusName = regexp.MustCompile("-[0-9]+$").MatchString

// Obtain yaml-specified bus well-known name
func (iface *dbusInterface) getAttribs(attribs interfaces.Attrer) (string, string, error) {
	// bus attribute
	var bus string
	if err := attribs.Attr("bus", &bus); err != nil {
		return "", "", fmt.Errorf("cannot find attribute 'bus'")
	}

	if bus != "session" && bus != "system" {
		return "", "", fmt.Errorf("bus '%s' must be one of 'session' or 'system'", bus)
	}

	// name attribute
	var name string
	if err := attribs.Attr("name", &name); err != nil {
		return "", "", fmt.Errorf("cannot find attribute 'name'")
	}

	err := interfaces.ValidateDBusBusName(name)
	if err != nil {
		return "", "", err
	}

	if isInvalidSnappyBusName(name) {
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
func getAppArmorSnippet(policy string, bus string, name string) string {
	old := "###DBUS_BUS###"
	new := bus
	snippet := strings.Replace(policy, old, new, -1)

	old = "###DBUS_NAME###"
	new = name
	snippet = strings.Replace(snippet, old, new, -1)

	// convert name to AppArmor dbus path (eg 'org.foo' to '/org/foo{,/**}')
	var pathBuf bytes.Buffer
	pathBuf.WriteString(`"/`)
	pathBuf.WriteString(strings.Replace(name, ".", "/", -1))
	pathBuf.WriteString(`{,/**}"`)

	old = "###DBUS_PATH###"
	new = pathBuf.String()
	snippet = strings.Replace(snippet, old, new, -1)

	// convert name to AppArmor dbus interface (eg, 'org.foo' to 'org.foo{,.*}')
	var ifaceBuf bytes.Buffer
	ifaceBuf.WriteString(`"`)
	ifaceBuf.WriteString(name)
	ifaceBuf.WriteString(`{,.*}"`)

	old = "###DBUS_INTERFACE###"
	new = ifaceBuf.String()
	snippet = strings.Replace(snippet, old, new, -1)

	return snippet
}

func (iface *dbusInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	bus, name, err := iface.getAttribs(plug)
	if err != nil {
		return err
	}

	busSlot, nameSlot, err := iface.getAttribs(slot)
	if err != nil {
		return err
	}

	// ensure that we only connect to slot with matching attributes
	if bus != busSlot || name != nameSlot {
		return nil
	}

	// well-known DBus name-specific connected plug policy
	snippet := getAppArmorSnippet(dbusConnectedPlugAppArmor, bus, name)

	// abstraction policy
	abstraction, err := getAppArmorAbstraction(bus)
	if err != nil {
		return err
	}

	old := "###DBUS_ABSTRACTION###"
	new := abstraction
	snippet = strings.Replace(snippet, old, new, -1)

	old = "###SLOT_SECURITY_TAGS###"
	new = spec.SnapAppSet().SlotLabelExpression(slot)
	snippet = strings.Replace(snippet, old, new, -1)

	spec.AddSnippet(snippet)
	return nil
}

func (iface *dbusInterface) DBusPermanentSlot(spec *dbus.Specification, slot *snap.SlotInfo) error {
	bus, name, err := iface.getAttribs(slot)
	if err != nil {
		return err
	}

	// only system services need bus policy
	if bus != "system" {
		return nil
	}

	old := "###DBUS_NAME###"
	new := name
	spec.AddSnippet(strings.Replace(dbusPermanentSlotDBus, old, new, -1))
	return nil
}

func (iface *dbusInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	bus, name, err := iface.getAttribs(slot)
	if err != nil {
		return err
	}

	// well-known DBus name-specific permanent slot policy
	snippet := getAppArmorSnippet(dbusPermanentSlotAppArmor, bus, name)

	// abstraction policy
	abstraction, err := getAppArmorAbstraction(bus)
	if err != nil {
		return err
	}

	old := "###DBUS_ABSTRACTION###"
	new := abstraction
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)

	if release.OnClassic {
		// classic-only policy
		spec.AddSnippet(getAppArmorSnippet(dbusPermanentSlotAppArmorClassic, bus, name))
	}
	return nil
}

func (iface *dbusInterface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(dbusPermanentSlotSecComp)
	return nil
}

func (iface *dbusInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	bus, name, err := iface.getAttribs(slot)
	if err != nil {
		return err
	}

	busPlug, namePlug, err := iface.getAttribs(plug)
	if err != nil {
		return err
	}

	// ensure that we only connect to slot with matching attributes. This
	// makes sure that the security policy is correct, but does not ensure
	// that 'snap interfaces' is correct.
	// TODO: we can fix the 'snap interfaces' issue when interface/policy
	// checkers when they are available
	if bus != busPlug || name != namePlug {
		return nil
	}

	// well-known DBus name-specific connected slot policy
	snippet := getAppArmorSnippet(dbusConnectedSlotAppArmor, bus, name)

	old := "###PLUG_SECURITY_TAGS###"
	new := spec.SnapAppSet().PlugLabelExpression(plug)
	snippet = strings.Replace(snippet, old, new, -1)
	spec.AddSnippet(snippet)
	return nil
}

func (iface *dbusInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	_, _, err := iface.getAttribs(plug)
	return err
}

func (iface *dbusInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_, _, err := iface.getAttribs(slot)
	return err
}

func (iface *dbusInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&dbusInterface{})
}
