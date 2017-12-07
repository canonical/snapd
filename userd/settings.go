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
	"regexp"
	"strings"
	"time"

	"github.com/godbus/dbus"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

// Timeout when the confirmation dialog for an xdg-setging
// automatically closes. Keep in sync with the core snaps
// xdg-settings wrapper which also sets this value to 300.
var defaultConfirmDialogTimeout = 300 * time.Second

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
		<arg type='s' name='check' direction='in'/>
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

// FIXME: allow setting default-url-scheme-handler ?
var settingsWhitelist = []string{
	"default-web-browser",
}

// FIXME: too restrictive?
var allowedSettingsValues = regexp.MustCompile(`^[a-zA-Z0-9._]+$`)

func settingWhitelisted(setting string) *dbus.Error {
	for _, whitelisted := range settingsWhitelist {
		if setting == whitelisted {
			return nil
		}
	}
	return dbus.MakeFailedError(fmt.Errorf("cannot use setting %q: not allowed", setting))
}

// Settings implements the 'io.snapcraft.Settings' DBus interface.
type Settings struct {
	conn *dbus.Conn
}

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

// some notes:
// - we only set/get desktop files
// - all desktop files of snaps are prefixed with: ${snap}_
// - on get/check/set we need to add/strip this prefix

// Check implements the 'Check' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Check string:'default-web-browser' string:'firefox.desktop'
func (s *Settings) Check(setting, check string, sender dbus.Sender) (string, *dbus.Error) {
	// avoid information leak: see https://github.com/snapcore/snapd/pull/4073#discussion_r146682758
	snap, err := snapFromSender(s.conn, sender)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	if err := settingWhitelisted(setting); err != nil {
		return "", err
	}
	if !allowedSettingsValues.MatchString(check) {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot check setting %q to value %q: value not allowed", setting, check))
	}

	// FIXME: this works only for desktop files
	desktopFile := fmt.Sprintf("%s_%s", snap, check)

	cmd := exec.Command("xdg-settings", "check", setting, desktopFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot check setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	return strings.TrimSpace(string(output)), nil
}

// Get implements the 'Get' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Get string:'default-web-browser'
func (s *Settings) Get(setting string, sender dbus.Sender) (string, *dbus.Error) {
	if err := settingWhitelisted(setting); err != nil {
		return "", err
	}

	cmd := exec.Command("xdg-settings", "get", setting)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot get setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	// avoid information leak: see https://github.com/snapcore/snapd/pull/4073#discussion_r146682758
	snap, err := snapFromSender(s.conn, sender)
	if err != nil {
		return "", dbus.MakeFailedError(err)
	}
	if !strings.HasPrefix(string(output), snap+"_") {
		return "NOT_THIS_SNAP.snap.desktop", nil
	}

	desktopFile := strings.SplitN(string(output), "_", 2)[1]
	return strings.TrimSpace(desktopFile), nil
}

// Set implements the 'Set' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Set string:'default-web-browser' string:'chromium-browser.desktop'
func (s *Settings) Set(setting, new string, sender dbus.Sender) *dbus.Error {
	if err := settingWhitelisted(setting); err != nil {
		return err
	}
	if !allowedSettingsValues.MatchString(new) {
		return dbus.MakeFailedError(fmt.Errorf("cannot set setting %q to value %q: value not allowed", setting, new))
	}

	// see https://github.com/snapcore/snapd/pull/4073#discussion_r146682758
	snap, err := snapFromSender(s.conn, sender)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	new = fmt.Sprintf("%s_%s", snap, new)

	// FIXME: what GUI toolkit to use?
	// FIXME2: we could support kdialog here as well
	if !osutil.ExecutableExists("zenity") {
		return dbus.MakeFailedError(fmt.Errorf("cannot find zenity"))
	}
	// FIXME: we need to know the parent PID or our dialog may pop under
	//        the existing windows
	txt := fmt.Sprintf(i18n.G(`
<big><b>Allow settings change?</b></big>

Allow snap %q to change %q to %q ?

<span size="x-small">This dialog will close automatically after 5 minutes of inactivity.</span>
`), snap, setting, new)
	cmd := exec.Command("zenity", "--question", "--modal", "--text="+txt)
	if err := cmd.Start(); err != nil {
		return dbus.MakeFailedError(err)
	}
	done := make(chan error)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		// normal exit
	case <-time.After(defaultConfirmDialogTimeout * time.Second):
		// timeout do nothing, the other side will have timed
		// out as well, no need to send a reply.
		cmd.Process.Kill()
		return nil
	}
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot set setting: user declined"))
	}

	cmd = exec.Command("xdg-settings", "set", setting, new)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot set setting %s: %s", setting, osutil.OutputErr(output, err)))
	}

	return nil
}
