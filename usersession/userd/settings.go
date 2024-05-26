// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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
	"path/filepath"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/usersession/userd/ui"
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
	<method name='CheckSub'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='subproperty' direction='in'/>
		<arg type='s' name='check' direction='in'/>
		<arg type='s' name='result' direction='out'/>
	</method>
	<method name='Get'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='result' direction='out'/>
	</method>
	<method name='GetSub'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='subproperty' direction='in'/>
		<arg type='s' name='result' direction='out'/>
	</method>
	<method name='Set'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='value' direction='in'/>
	</method>
	<method name='SetSub'>
		<arg type='s' name='setting' direction='in'/>
		<arg type='s' name='subproperty' direction='in'/>
		<arg type='s' name='value' direction='in'/>
	</method>
</interface>`

var validSettings = []string{
	"default-web-browser",
	"default-url-scheme-handler",
}

func allowedSetting(setting string) bool {
	if !strings.HasSuffix(setting, ".desktop") {
		return false
	}
	base := strings.TrimSuffix(setting, ".desktop")

	return snap.ValidAppName(base)
}

// settingSpec specifies a setting with an optional subproperty
type settingSpec struct {
	setting     string
	subproperty string
}

func (s *settingSpec) String() string {
	if s.subproperty != "" {
		return fmt.Sprintf("%q subproperty %q", s.setting, s.subproperty)
	} else {
		return fmt.Sprintf("%q", s.setting)
	}
}

func (s *settingSpec) validate() *dbus.Error {
	for _, valid := range validSettings {
		if s.setting == valid {
			return nil
		}
	}
	return dbus.MakeFailedError(fmt.Errorf("invalid setting %q", s.setting))
}

// Settings implements the 'io.snapcraft.Settings' DBus interface.
type Settings struct {
	conn *dbus.Conn
}

// Interface returns the name of the interface this object implements
func (s *Settings) Interface() string {
	return "io.snapcraft.Settings"
}

// ObjectPath returns the path that the object is exported as
func (s *Settings) ObjectPath() dbus.ObjectPath {
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

func safeSnapFromSender(s *Settings, sender dbus.Sender) (string, *dbus.Error) {
	// avoid information leak: see https://github.com/snapcore/snapd/pull/4073#discussion_r146682758
	snap := mylog.Check2(snapFromSender(s.conn, sender))

	return snap, nil
}

func desktopFileFromValueForSetting(s *Settings, command string, setspec *settingSpec, dotDesktopValue string, sender dbus.Sender) (string, *dbus.Error) {
	snap := mylog.Check2(safeSnapFromSender(s, sender))
	mylog.Check(setspec.validate())

	if !allowedSetting(dotDesktopValue) {
		return "", dbus.MakeFailedError(fmt.Errorf("cannot %s %s setting to invalid value %q", command, setspec, dotDesktopValue))
	}

	// FIXME: this works only for desktop files
	desktopFile := fmt.Sprintf("%s_%s", snap, dotDesktopValue)
	return desktopFile, nil
}

func desktopFileFromOutput(s *Settings, output string, sender dbus.Sender) (string, *dbus.Error) {
	snap := mylog.Check2(safeSnapFromSender(s, sender))

	if !strings.HasPrefix(output, snap+"_") {
		return "NOT-THIS-SNAP.desktop", nil
	}

	desktopFile := strings.SplitN(output, "_", 2)[1]
	return strings.TrimSpace(desktopFile), nil
}

func setDialog(s *Settings, setspec *settingSpec, desktopFile string, sender dbus.Sender) *dbus.Error {
	df := filepath.Join(dirs.SnapDesktopFilesDir, desktopFile)
	if !osutil.FileExists(df) {
		return dbus.MakeFailedError(fmt.Errorf("cannot find desktop file %q", df))
	}

	// FIXME: we need to know the parent PID or our dialog may pop under
	//        the existing windows. We might get it with the help of
	//        the xdg-settings tool inside the core snap. It would have
	//        to get the PID of the process asking for the settings
	//        then xdg-settings can sent this to us and we can intospect
	//        the X windows for _NET_WM_PID and use the windowID to
	//        attach to zenity - not sure how this translate to the
	//        wayland world though :/
	dialog, uiErr := ui.New()
	if uiErr != nil {
		return dbus.MakeFailedError(fmt.Errorf("cannot ask for settings change: %v", uiErr))
	}

	snap := mylog.Check2(safeSnapFromSender(s, sender))

	answeredYes := dialog.YesNo(
		i18n.G("Allow settings change?"),
		fmt.Sprintf(i18n.G("Allow snap %q to change %s to %q ?"), snap, setspec, desktopFile),
		&ui.DialogOptions{
			Timeout: defaultConfirmDialogTimeout,
			Footer:  i18n.G("This dialog will close automatically after 5 minutes of inactivity."),
		},
	)
	if !answeredYes {
		return dbus.MakeFailedError(fmt.Errorf("cannot change configuration: user declined change"))
	}
	return nil
}

func checkOutput(cmd *exec.Cmd, command string, setspec *settingSpec) (string, *dbus.Error) {
	output, stderr := mylog.Check3(osutil.RunCmd(cmd))

	return string(output), nil
}

// Check implements the 'Check' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Check string:'default-web-browser' string:'firefox.desktop'
func (s *Settings) Check(setting string, check string, sender dbus.Sender) (string, *dbus.Error) {
	mylog.Check(checkOnClassic())

	settingMain := &settingSpec{setting: setting}
	desktopFile := mylog.Check2(desktopFileFromValueForSetting(s, "check", settingMain, check, sender))

	cmd := exec.Command("xdg-settings", "check", setting, desktopFile)
	output := mylog.Check2(checkOutput(cmd, "check", settingMain))

	return strings.TrimSpace(output), nil
}

// CheckSub implements the 'CheckSub' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.CheckSub string:'default-url-scheme-handler' string:'irc' string:'ircclient.desktop'
func (s *Settings) CheckSub(setting string, subproperty string, check string, sender dbus.Sender) (string, *dbus.Error) {
	mylog.Check(checkOnClassic())

	settingSub := &settingSpec{setting: setting, subproperty: subproperty}
	desktopFile := mylog.Check2(desktopFileFromValueForSetting(s, "check", settingSub, check, sender))

	cmd := exec.Command("xdg-settings", "check", setting, subproperty, desktopFile)
	output := mylog.Check2(checkOutput(cmd, "check", settingSub))

	return strings.TrimSpace(output), nil
}

// Get implements the 'Get' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Get string:'default-web-browser'
func (s *Settings) Get(setting string, sender dbus.Sender) (string, *dbus.Error) {
	mylog.Check(checkOnClassic())

	settingMain := &settingSpec{setting: setting}
	mylog.Check(settingMain.validate())

	cmd := exec.Command("xdg-settings", "get", setting)
	output := mylog.Check2(checkOutput(cmd, "get", settingMain))

	return desktopFileFromOutput(s, output, sender)
}

// GetSub implements the 'GetSub' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.GetSub string:'default-url-scheme-handler' string:'irc'
func (s *Settings) GetSub(setting string, subproperty string, sender dbus.Sender) (string, *dbus.Error) {
	mylog.Check(checkOnClassic())

	settingSub := &settingSpec{setting: setting, subproperty: subproperty}
	mylog.Check(settingSub.validate())

	cmd := exec.Command("xdg-settings", "get", setting, subproperty)
	output := mylog.Check2(checkOutput(cmd, "get", settingSub))

	return desktopFileFromOutput(s, output, sender)
}

// Set implements the 'Set' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.Set string:'default-web-browser' string:'chromium-browser.desktop'
func (s *Settings) Set(setting string, new string, sender dbus.Sender) *dbus.Error {
	mylog.Check(checkOnClassic())

	settingMain := &settingSpec{setting: setting}
	desktopFile := mylog.Check2(desktopFileFromValueForSetting(s, "set", settingMain, new, sender))
	mylog.Check(setDialog(s, settingMain, desktopFile, sender))

	cmd := exec.Command("xdg-settings", "set", setting, desktopFile)
	mylog.Check2(checkOutput(cmd, "set", settingMain))

	return nil
}

// SetSub implements the 'SetSub' method of the 'io.snapcraft.Settings'
// DBus interface.
//
// Example usage: dbus-send --session --dest=io.snapcraft.Settings --type=method_call --print-reply /io/snapcraft/Settings io.snapcraft.Settings.SetSub string:'default-url-scheme-handler' string:'irc' string:'ircclient.desktop'
func (s *Settings) SetSub(setting string, subproperty string, new string, sender dbus.Sender) *dbus.Error {
	mylog.Check(checkOnClassic())

	settingSub := &settingSpec{setting: setting, subproperty: subproperty}
	desktopFile := mylog.Check2(desktopFileFromValueForSetting(s, "set", settingSub, new, sender))
	mylog.Check(setDialog(s, settingSub, desktopFile, sender))

	cmd := exec.Command("xdg-settings", "set", setting, subproperty, desktopFile)
	mylog.Check2(checkOutput(cmd, "set", settingSub))

	return nil
}
