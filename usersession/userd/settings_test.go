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

package userd_test

import (
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
	"github.com/snapcore/snapd/usersession/userd/ui"
)

type settingsSuite struct {
	testutil.BaseTest

	settings        *userd.Settings
	mockXdgSettings *testutil.MockCmd
}

var _ = Suite(&settingsSuite{})

func (s *settingsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.AddCleanup(release.MockOnClassic(true))

	s.AddCleanup(userd.MockSnapFromSender(func(*dbus.Conn, dbus.Sender) (string, error) {
		return "some-snap", nil
	}))

	s.settings = &userd.Settings{}
	s.mockXdgSettings = testutil.MockCommand(c, "xdg-settings", `
case "$1" in
    get)
        case "$2" in
            default-web-browser)
                echo "some-snap_foo.desktop"
                ;;
            default-url-scheme-handler)
                echo "some-snap_ircclient.desktop"
                ;;
            *)
                echo "mock called with unsupported arguments: $*"
                exit 1
                ;;
        esac
        ;;
    set)
        case "$2" in
            default-web-browser)
                # nothing to do
                ;;
            default-url-scheme-handler)
                if [ "$3" = "irc2" ]; then
                    echo "fail"
                    exit 1
                fi
                # nothing to do
                ;;
            *)
                echo "mock called with unsupported arguments: $*"
                exit 1
                ;;
        esac
        ;;
    check)
        case "$2" in
            default-web-browser)
                if [ "$3" = "some-snap_foo.desktop" ]; then
                    echo "yes"
                else
                    echo "no"
                fi
                ;;
            default-url-scheme-handler)
                if [ "$3" = "irc" ] && [ "$4" = "some-snap_ircclient.desktop" ]; then
                    echo "yes"
                else
                    echo "no"
                fi
                ;;
        esac
        ;;
    *)
        echo "mock called with unsupported argument: $1"
        exit 1
        ;;
esac
`)
	s.AddCleanup(s.mockXdgSettings.Restore)
}

func mockUIcommands(c *C, script string) func() {
	mockZenity := testutil.MockCommand(c, "zenity", script)
	mockKDialog := testutil.MockCommand(c, "kdialog", script)
	return func() {
		mockZenity.Restore()
		mockKDialog.Restore()
	}
}

func (s *settingsSuite) TestGetUnhappy(c *C) {
	for _, t := range []struct {
		setting    string
		errMatcher string
	}{
		{"random-setting", `invalid setting "random-setting"`},
		{"invälid", `invalid setting "invälid"`},
		{"", `invalid setting ""`},
	} {
		_ := mylog.Check2(s.settings.Get(t.setting, ":some-dbus-sender"))
		c.Assert(err, ErrorMatches, t.errMatcher)
		c.Assert(s.mockXdgSettings.Calls(), IsNil)
	}
}

func (s *settingsSuite) TestGetHappy(c *C) {
	defaultBrowser := mylog.Check2(s.settings.Get("default-web-browser", ":some-dbus-sender"))

	c.Check(defaultBrowser, Equals, "foo.desktop")
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "get", "default-web-browser"},
	})
}

func (s *settingsSuite) TestCheckInvalidSetting(c *C) {
	_ := mylog.Check2(s.settings.Check("random-setting", "foo.desktop", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `invalid setting "random-setting"`)
	c.Assert(s.mockXdgSettings.Calls(), IsNil)
}

func (s *settingsSuite) TestCheckIsDefault(c *C) {
	isDefault := mylog.Check2(s.settings.Check("default-web-browser", "foo.desktop", ":some-dbus-sender"))

	c.Check(isDefault, Equals, "yes")
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "check", "default-web-browser", "some-snap_foo.desktop"},
	})
}

func (s *settingsSuite) TestCheckIsDefaultUrlScheme(c *C) {
	isDefault := mylog.Check2(s.settings.CheckSub("default-url-scheme-handler", "irc", "ircclient.desktop", ":some-dbus-sender"))

	c.Check(isDefault, Equals, "yes")
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "check", "default-url-scheme-handler", "irc", "some-snap_ircclient.desktop"},
	})
}

func (s *settingsSuite) TestCheckNoDefault(c *C) {
	isDefault := mylog.Check2(s.settings.Check("default-web-browser", "bar.desktop", ":some-dbus-sender"))

	c.Check(isDefault, Equals, "no")
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "check", "default-web-browser", "some-snap_bar.desktop"},
	})
}

func (s *settingsSuite) TestCheckNoDefaultUrlScheme(c *C) {
	isDefault := mylog.Check2(s.settings.CheckSub("default-url-scheme-handler", "irc", "bar.desktop", ":some-dbus-sender"))

	c.Check(isDefault, Equals, "no")
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "check", "default-url-scheme-handler", "irc", "some-snap_bar.desktop"},
	})
}

func (s *settingsSuite) TestNotThisSnap(c *C) {
	mockXdgSettings := testutil.MockCommand(c, "xdg-settings", `
if [ "$1" = "get" ] && [ "$2" = "default-web-browser" ]; then
    echo "other-snap_foo.desktop"
    exit 0
fi
if [ "$1" = "get" ] && [ "$2" = "default-url-scheme-handler" ] && [ "$3" = "irc" ]; then
    echo "other-snap_foo-irc.desktop"
    exit 0
fi

echo "mock called with unsupported argument: $1"
exit 1
`)
	defer mockXdgSettings.Restore()

	defaultBrowser := mylog.Check2(s.settings.Get("default-web-browser", ":some-dbus-sender"))

	c.Check(defaultBrowser, Equals, "NOT-THIS-SNAP.desktop")
	c.Check(mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "get", "default-web-browser"},
	})

	mockXdgSettings.ForgetCalls()

	defaultSchemeHandler := mylog.Check2(s.settings.GetSub("default-url-scheme-handler", "irc", ":some-dbus-sender"))

	c.Check(defaultSchemeHandler, Equals, "NOT-THIS-SNAP.desktop")
	c.Check(mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "get", "default-url-scheme-handler", "irc"},
	})
}

func (s *settingsSuite) TestSetInvalidSetting(c *C) {
	mylog.Check(s.settings.Set("random-setting", "foo.desktop", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `invalid setting "random-setting"`)
	c.Assert(s.mockXdgSettings.Calls(), IsNil)
}

func (s *settingsSuite) TestSetInvalidValue(c *C) {
	mylog.Check(s.settings.Set("default-web-browser", "foo", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `cannot set "default-web-browser" setting to invalid value "foo"`)
	c.Assert(s.mockXdgSettings.Calls(), IsNil)
}

func (s *settingsSuite) TestSetSubInvalidSetting(c *C) {
	mylog.Check(s.settings.SetSub("random-setting", "subprop", "foo.desktop", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `invalid setting "random-setting"`)
	c.Assert(s.mockXdgSettings.Calls(), IsNil)
}

func (s *settingsSuite) TestSetSubInvalidValue(c *C) {
	mylog.Check(s.settings.SetSub("default-url-scheme-handler", "irc", "foo", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `cannot set "default-url-scheme-handler" subproperty "irc" setting to invalid value "foo"`)
	c.Assert(s.mockXdgSettings.Calls(), IsNil)
}

func (s *settingsSuite) testSetUserDeclined(c *C) {
	df := filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_bar.desktop")
	mylog.Check(os.MkdirAll(filepath.Dir(df), 0755))

	mylog.Check(os.WriteFile(df, nil, 0644))

	mylog.Check(s.settings.Set("default-web-browser", "bar.desktop", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `cannot change configuration: user declined change`)
	c.Check(s.mockXdgSettings.Calls(), IsNil)
	// FIXME: this needs PR#4342
	/*
		c.Check(mockZenity.Calls(), DeepEquals, [][]string{
			{"zenity", "--question", "--text=Allow changing setting \"default-web-browser\" to \"bar.desktop\" ?"},
		})
	*/
}

func (s *settingsSuite) TestSetUserDeclinedKDialog(c *C) {
	// force zenity exec missing
	restoreZenity := ui.MockHasZenityExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "false")
	defer func() {
		restoreZenity()
		restoreCmds()
	}()

	s.testSetUserDeclined(c)
}

func (s *settingsSuite) TestSetUserDeclinedZenity(c *C) {
	// force kdialog exec missing
	restoreKDialog := ui.MockHasKDialogExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "false")
	defer func() {
		restoreKDialog()
		restoreCmds()
	}()

	s.testSetUserDeclined(c)
}

func (s *settingsSuite) testSetUserAccepts(c *C) {
	df := filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_foo.desktop")
	mylog.Check(os.MkdirAll(filepath.Dir(df), 0755))

	mylog.Check(os.WriteFile(df, nil, 0644))

	mylog.Check(s.settings.Set("default-web-browser", "foo.desktop", ":some-dbus-sender"))

	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "set", "default-web-browser", "some-snap_foo.desktop"},
	})
	// FIXME: this needs PR#4342
	/*
			c.Check(mockZenity.Calls(), DeepEquals, [][]string{
				{"zenity", "--question", "--text=Allow changing setting \"default-web-browser\" to \"foo.desktop\" ?"},
		})
	*/
}

func (s *settingsSuite) testSetUserAcceptsURLScheme(c *C) {
	df := filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_ircclient.desktop")
	mylog.Check(os.MkdirAll(filepath.Dir(df), 0755))

	mylog.Check(os.WriteFile(df, nil, 0644))

	mylog.Check(s.settings.SetSub("default-url-scheme-handler", "irc", "ircclient.desktop", ":some-dbus-sender"))

	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "set", "default-url-scheme-handler", "irc", "some-snap_ircclient.desktop"},
	})
}

func (s *settingsSuite) TestSetUserAcceptsZenity(c *C) {
	// force kdialog exec missing
	restoreKDialog := ui.MockHasKDialogExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "true")
	defer func() {
		restoreKDialog()
		restoreCmds()
	}()

	s.testSetUserAccepts(c)
}

func (s *settingsSuite) TestSetUserAcceptsZenityUrlScheme(c *C) {
	// force kdialog exec missing
	restoreKDialog := ui.MockHasKDialogExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "true")
	defer func() {
		restoreKDialog()
		restoreCmds()
	}()

	s.testSetUserAcceptsURLScheme(c)
}

func (s *settingsSuite) TestSetUserAcceptsKDialog(c *C) {
	// force zenity exec missing
	restoreZenity := ui.MockHasZenityExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "true")
	defer func() {
		restoreZenity()
		restoreCmds()
	}()

	s.testSetUserAccepts(c)
}

func (s *settingsSuite) TestSetUserAcceptsKDialogUrlScheme(c *C) {
	// force zenity exec missing
	restoreZenity := ui.MockHasZenityExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "true")
	defer func() {
		restoreZenity()
		restoreCmds()
	}()

	s.testSetUserAcceptsURLScheme(c)
}

func (s *settingsSuite) TestSetUserAcceptsZenityUrlSchemeXdgSettingsError(c *C) {
	// force kdialog exec missing
	restoreKDialog := ui.MockHasKDialogExecutable(func() bool { return false })
	restoreCmds := mockUIcommands(c, "true")
	defer func() {
		restoreKDialog()
		restoreCmds()
	}()

	df := filepath.Join(dirs.SnapDesktopFilesDir, "some-snap_ircclient.desktop")
	mylog.Check(os.MkdirAll(filepath.Dir(df), 0755))

	mylog.Check(os.WriteFile(df, nil, 0644))

	mylog.Check(s.settings.SetSub("default-url-scheme-handler", "irc2", "ircclient.desktop", ":some-dbus-sender"))
	c.Assert(err, ErrorMatches, `cannot set "default-url-scheme-handler" subproperty "irc2" setting: fail`)
	c.Check(s.mockXdgSettings.Calls(), DeepEquals, [][]string{
		{"xdg-settings", "set", "default-url-scheme-handler", "irc2", "some-snap_ircclient.desktop"},
	})
}

func (s *settingsSuite) TestFailsOnUbuntuCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	_ := mylog.Check2(s.settings.Check("default-web-browser", "foo.desktop", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")

	_ = mylog.Check2(s.settings.CheckSub("default-url-scheme-handler", "irc", "bar.desktop", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")

	_ = mylog.Check2(s.settings.Get("default-web-browser", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")

	_ = mylog.Check2(s.settings.GetSub("default-url-scheme-handler", "irc", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")
	mylog.Check(s.settings.Set("default-web-browser", "foo.desktop", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")
	mylog.Check(s.settings.SetSub("default-url-scheme-handler", "irc", "ircclient.desktop", ":some-dbus-sender"))
	c.Check(err, ErrorMatches, "not supported on Ubuntu Core")

	c.Check(s.mockXdgSettings.Calls(), HasLen, 0)
}
