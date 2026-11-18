// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"path"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const displayControlSummary = `allows configuring display parameters`

const displayControlBaseDeclarationSlots = `
  display-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// gnome-settings-daemon also provides an API via setting the Brightness
// property via a Set() method on the org.gnome.SettingsDaemon.Power.Screen
// interface, but we can't mediate member data. This could instead be supported
// via userd...
const displayControlConnectedPlugAppArmor = `
# Description: This interface allows getting information about a connected
# display and setting parameters like backlight brightness.

# keyboard backlight key
/sys/class/leds/ r,
/sys/devices/**/leds/**kbd_backlight/{,**} r,
/sys/devices/**/leds/**kbd_backlight/brightness w,

# upower
#include <abstractions/dbus-strict>
dbus (send)
    bus=system
    path=/org/freedesktop/UPower/KbdBacklight
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),
dbus (send)
    bus=system
    path=/org/freedesktop/UPower/KbdBacklight
    interface=org.freedesktop.UPower.KbdBacklight
    member={GetBrightness,GetMaxBrightness,SetBrightness}
    peer=(label=unconfined),

# gnome-settings-daemon
#include <abstractions/dbus-session-strict>
dbus (send)
    bus=session
    path=/org/gnome/SettingsDaemon/Power
    interface=org.freedesktop.DBus.Introspectable
    member=Introspect
    peer=(label=unconfined),
dbus (send)
    bus=session
    path=/org/gnome/SettingsDaemon/Power
    interface=org.gnome.SettingsDaemon.Power.Screen
    member=Step{Down,Up}
    peer=(label=unconfined),

/sys/class/backlight/ r,

# Allow changing backlight
/sys/devices/**/**/drm/card[0-9]/card[0-9]*/*_backlight/brightness w,
/sys/devices/platform/lvds_backlight/backlight/lvds_backlight/brightness rw,
/sys/devices/platform/lvds_backlight/backlight/lvds_backlight/bl_power rw,
`

type displayControlInterface struct {
	commonInterface
}

func (iface *displayControlInterface) dereferencedBacklightPaths() []string {
	var paths []string
	sysClass := "/sys/class/backlight"
	dirs, err := readDir(sysClass)
	if err != nil {
		return paths
	}

	for _, s := range dirs {
		p, err := evalSymlinks(filepath.Join(sysClass, s.Name()))
		if err != nil {
			continue
		}
		paths = append(paths, filepath.Clean(p))
	}
	return paths
}

func (iface *displayControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// add the static rules
	spec.AddSnippet(displayControlConnectedPlugAppArmor)

	// add the detected rules
	for _, p := range iface.dereferencedBacklightPaths() {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "# autodetected backlight: %s\n", path.Base(p))
		fmt.Fprintf(&buf, "%s/{,**} r,\n", p)
		fmt.Fprintf(&buf, "%s/bl_power w,\n", p)
		fmt.Fprintf(&buf, "%s/brightness w,\n", p)
		spec.AddSnippet(buf.String())
	}

	return nil
}

func init() {
	registerIface(&displayControlInterface{commonInterface{
		name:                  "display-control",
		summary:               displayControlSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  displayControlBaseDeclarationSlots,
		connectedPlugAppArmor: displayControlConnectedPlugAppArmor,
	}})
}
