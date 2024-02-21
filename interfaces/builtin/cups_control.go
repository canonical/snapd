// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// On classic systems where the slot is implicitly provided, the interface
// allows access to the cups control socket.
//
// On systems where the slot is provided by an app snap, the cups-control
// interface is the companion interface to the cups interface. The design of
// these interfaces is based on the idea that the slot implementation (eg
// cupsd) is expected to query snapd to determine if the cups-control interface
// is connected or not for the peer client process and the print service will
// mediate admin functionality (ie, the rules in these interfaces allow
// connecting to the print service, but do not implement enforcement rules; it
// is up to the print service to provide enforcement).
const cupsControlSummary = `allows access to the CUPS control socket`

// cups-control is implicit on classic but may also be provided by an app snap
// on core or classic (the current design allows the snap provider to slots
// both cups-control and cups or just cups-control (like with implicit classic
// or any slot provider without mediation patches), but not just cups).
const cupsControlBaseDeclarationSlots = `
  cups-control:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-auto-connection: true
    deny-connection:
      on-classic: false
`

const cupsControlPermanentSlotAppArmor = `
# Allow daemon access to create the CUPS socket
/{,var/}run/cups/ rw,
/{,var/}run/cups/** rwk,

# Allow cups to verify passwords directly
/etc/shadow r,
/var/lib/extrausers/shadow r,

# Some versions of CUPS will verify the connecting pid's
# security label
@{PROC}/[0-9]*/attr/{,apparmor/}current r,

# Allow daemon access to the color manager on the system
dbus (receive, send)
    bus=system
    path=/org/freedesktop/ColorManager
    interface=org.freedesktop.ColorManager
    peer=(name=org.freedesktop.ColorManager),

# Allow daemon to send notifications
dbus (send)
    bus=system
    path=/org/cups/cupsd/Notifier
    interface=org.cups.cupsd.Notifier
    peer=(label=unconfined),

# Allow daemon to send signals to its snap_daemon processes
capability kill,

# Allow daemon to manage snap_daemon files and directories
capability fsetid,
`

const cupsControlConnectedSlotAppArmor = `
# Allow daemon to send notifications to connected snaps
dbus (send)
    bus=system
    path=/org/cups/cupsd/Notifier
    interface=org.cups.cupsd.Notifier
    peer=(label=###PLUG_SECURITY_TAGS###),
`

const cupsControlConnectedPlugAppArmor = `
# Allow communicating with the cups server for printing and configuration.

#include <abstractions/cups-client>
/{,var/}run/cups/printcap r,

# Allow receiving all DBus signal notifications from the daemon (see
# notifier/dbus.c in cups sources)
dbus (receive)
    bus=system
    path=/org/cups/cupsd/Notifier
    interface=org.cups.cupsd.Notifier
    peer=(label=###SLOT_SECURITY_TAGS###),
`

type cupsControlInterface struct {
	commonInterface
}

func (iface *cupsControlInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	// On classic, only apply slot snippet when running as application snap
	// on classic since the slot side may be from the classic OS or snap.
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(cupsControlPermanentSlotAppArmor)
	}
	return nil
}

func (iface *cupsControlInterface) AppArmorConnectedSlot(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// On classic, only apply slot snippet when running as application snap
	// on classic since the slot side may be from the classic OS or snap.
	if !implicitSystemConnectedSlot(slot) {
		old := "###PLUG_SECURITY_TAGS###"
		new := spec.SnapAppSet().PlugLabelExpression(plug)
		snippet := strings.Replace(cupsControlConnectedSlotAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *cupsControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	old := "###SLOT_SECURITY_TAGS###"
	var new string
	// If we're running on classic, cups may be installed either as a snap
	// or as part of the classic OS. If it is part of the classic OS, it
	// will not have a security label like it would when installed as a
	// snap.
	if implicitSystemConnectedSlot(slot) {
		// cupsd from the OS may be confined or unconfined. Newer
		// releases may use the 'cupsd' label instead of the old
		// path-based label.
		new = "\"{unconfined,/usr/sbin/cupsd,cupsd}\""
	} else {
		new = spec.SnapAppSet().SlotLabelExpression(slot)
	}

	// implement 'implicitOnCore: false/implicitOnClassic: true' by only
	// applying the snippet if the slot is an app or we are on classic
	if slot.Snap().Type() == snap.TypeApp || release.OnClassic {
		snippet := strings.Replace(cupsControlConnectedPlugAppArmor, old, new, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *cupsControlInterface) AutoConnect(plug *snap.PlugInfo, slot *snap.SlotInfo) bool {
	cupsdConf := filepath.Join(dirs.GlobalRootDir, "/etc/cups/cupsd.conf")
	_, hostSystemHasCupsd, _ := osutil.RegularFileExists(cupsdConf)
	if hostSystemHasCupsd {
		// If the host system has cupsd installed, we want to
		// direct connections to the implicit
		// system:cups-control slot
		return implicitSystemPermanentSlot(slot)
	} else {
		// If host system does not have cupsd, block
		// auto-connect to system:cups-control slot
		return !implicitSystemPermanentSlot(slot)
	}
}

func init() {
	registerIface(&cupsControlInterface{
		commonInterface: commonInterface{
			name:                 "cups-control",
			summary:              cupsControlSummary,
			baseDeclarationSlots: cupsControlBaseDeclarationSlots,
			implicitOnCore:       false,
			implicitOnClassic:    true,
		},
	})
}
