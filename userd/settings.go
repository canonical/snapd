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

package userd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

const settingsIntrospectionXML = `
<interface name="org.freedesktop.DBus.Peer">
	<method name='Ping'>
	</method>
	<method name='GetMachineId'>
               <arg type='s' name='machine_uuid' direction='out'/>
	</method>
</interface>
<interface name='io.snapcraft.Settings'>
	<method name='Check'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' dbusInterfacename='check' direction='in'/>
                <arg type='s' name='result' direction='out'/>
	</method>
	<method name='Get'>
		<arg type='s' name='setting' direction='in'/>
                <arg type='s' name='result' direction='out'/>
	</method>
	<method name='Set'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='value' direction='in'/>
	</method>
</interface>`

// Settings implements the 'io.snapcraft.Settings' DBus interface.
type Settings struct{}

// Name returns the name of the interface this object implements
func (s *Settings) Name() string {
	return "io.snapcraft.Settings"
}

// BasePath returns the base path of the object
func (s *Settings) BasePath() dbus.ObjectPath {
	return "/io/snapcraft/Settings"
}

// IntrospectionData gives the XML formatted introspection description
// of the DBus service.
func (s *Settings) IntrospectionData() string {
	return settingsIntrospectionXML
}

// Check implements the 'Check' method of the 'com.canonical.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Check string:'default-web-browser' string:'firefox.desktop'
func (s *Settings) Check(setting, check string) (string, *dbus.Error) {
	cmd := exec.Command("xdg-settings", "check", setting, check)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot check setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	return strings.TrimSpace(string(output)), nil
}

// Get implements the 'Get' method of the 'com.canonical.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Get string:'default-web-browser'
func (s *Settings) Get(setting string) (string, *dbus.Error) {
	cmd := exec.Command("xdg-settings", "get", setting)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot get setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	return strings.TrimSpace(string(output)), nil
}

// Set implements the 'Set' method of the 'com.canonical.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Set string:'default-web-browser' string:'chromium-browser.desktop'
func (s *Settings) Set(setting, new string) *dbus.Error {
	// FIXME: what GUI toolkit to use?
	// FIXME2: we could support kdialog here as well
	if !osutil.ExecutableExists("zenity") {
		return dbus.MakeFailedError(fmt.Errorf("cannot find zenity"))
	}
	cmd := exec.Command("zenity", "--question", "--text="+fmt.Sprintf(i18n.G("Allow changing setting %q to %q ?"), setting, new))
	if err := cmd.Run(); err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot set setting: user declined"))
	}

	cmd = exec.Command("xdg-settings", "set", setting, new)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot set setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	return nil
}
