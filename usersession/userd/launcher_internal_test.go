// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-20 Canonical Ltd
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
	"github.com/snapcore/snapd/strutil"
	. "gopkg.in/check.v1"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type launcherInternalSuite struct {
}

var _ = Suite(&launcherInternalSuite{})

var mockFileSystem = []string{
	"/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop",
	"/var/lib/snapd/desktop/applications/multipass_multipass-gui.desktop",
	"/var/lib/snapd/desktop/applications/cevelop_cevelop.desktop",
	"/var/lib/snapd/desktop/applications/egmde-confined-desktop_egmde-confined-desktop.desktop",
	"/var/lib/snapd/desktop/applications/classic-snap-analyzer_classic-snap-analyzer.desktop",
	"/var/lib/snapd/desktop/applications/vlc_vlc.desktop",
	"/var/lib/snapd/desktop/applications/gnome-calculator_gnome-calculator.desktop",
	"/var/lib/snapd/desktop/applications/mir-kiosk-kodi_mir-kiosk-kodi.desktop",
	"/var/lib/snapd/desktop/applications/gnome-characters_gnome-characters.desktop",
	"/var/lib/snapd/desktop/applications/clion_clion.desktop",
	"/var/lib/snapd/desktop/applications/gnome-system-monitor_gnome-system-monitor.desktop",
	"/var/lib/snapd/desktop/applications/inkscape_inkscape.desktop",
	"/var/lib/snapd/desktop/applications/gnome-logs_gnome-logs.desktop",

	"/var/lib/snapd/desktop/applications/foo-bar/baz.desktop",
	"/var/lib/snapd/desktop/applications/baz/foo-bar.desktop",
}

func existsOnMockFileSystem(desktop_file string) bool {
	return strutil.ListContains(mockFileSystem, desktop_file)
}

func (s *launcherInternalSuite) TestDesktopFileIDToFilenameSucceedsWithValidId(c *C) {

	var desktopIdTests = []struct {
		id     string
		expect string
	}{
		{"mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop"},
		{"foo-bar-baz.desktop", "/var/lib/snapd/desktop/applications/foo-bar/baz.desktop"},
		{"baz-foo-bar.desktop", "/var/lib/snapd/desktop/applications/baz/foo-bar.desktop"},
	}

	for _, test := range desktopIdTests {
		actual, err := desktopFileIDToFilename(existsOnMockFileSystem, test.id)
		c.Assert(err, IsNil)
		c.Assert(actual, Equals, test.expect)
	}
}

func (s *launcherInternalSuite) TestDesktopFileIDToFilenameFailsWithInvalidId(c *C) {
	var desktopIdTests = []string{
		"mir-kiosk-scummvm-mir-kiosk-scummvm.desktop",
		"bar-foo-baz.desktop",
		"bar-baz-foo.desktop",
		"foo-bar_foo-bar.desktop",
	}

	for _, id := range desktopIdTests {
		_, err := desktopFileIDToFilename(existsOnMockFileSystem, id)
		c.Assert(err, NotNil)
	}
}

func (s *launcherInternalSuite) TestParseExecCommandSucceedsWithValidEntry(c *C) {
	var exec_command = []struct {
		exec_command string
		expect       []string
	}{
		{"env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop /snap/bin/mir-kiosk-scummvm %U",
			[]string{"env", "BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/snap/bin/mir-kiosk-scummvm"}},
		{"/snap/bin/foo -f %U %%bar", []string{"/snap/bin/foo", "-f", "%bar"}},
		{"/snap/bin/foo '-f %U %%bar'", []string{"/snap/bin/foo", "-f %U %%bar"}},
		{"/snap/bin/foo \"'-f bar'\"", []string{"/snap/bin/foo", "'-f bar'"}},
	}

	for _, test := range exec_command {
		actual, err := parseExecCommand(test.exec_command)
		c.Assert(err, IsNil)
		c.Assert(actual, DeepEquals, test.expect)
	}
}

func (s *launcherInternalSuite) TestParseExecCommandFailsWithInvalidEntry(c *C) {
	var exec_command = []string{
		"/snap/bin/foo \"unclosed double quote",
		"/snap/bin/foo 'unclosed single quote",
	}

	for _, test := range exec_command {
		_, err := parseExecCommand(test)
		c.Assert(err, NotNil)
	}
}
