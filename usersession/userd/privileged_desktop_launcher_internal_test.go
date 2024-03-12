// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
)

type privilegedDesktopLauncherInternalSuite struct {
	testutil.BaseTest
}

var _ = Suite(&privilegedDesktopLauncherInternalSuite{})

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

	// A desktop file ID provided by a snap may be shadowed by the
	// host system.
	"/usr/share/applications/shadow-test.desktop",
	"/var/lib/snapd/desktop/applications/shadow-test.desktop",
}

func existsOnMockFileSystem(desktop_file string) (bool, bool, error) {
	existsOnMockFileSystem := strutil.ListContains(mockFileSystem, desktop_file)
	return existsOnMockFileSystem, existsOnMockFileSystem, nil
}

func (s *privilegedDesktopLauncherInternalSuite) mockEnv(key, value string) {
	old := os.Getenv(key)
	os.Setenv(key, value)
	s.AddCleanup(func() {
		os.Setenv(key, old)
	})
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileSearchPath(c *C) {
	s.mockEnv("HOME", "/home/user")
	s.mockEnv("XDG_DATA_HOME", "")
	s.mockEnv("XDG_DATA_DIRS", "")

	// Default search path
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/.local/share/applications",
		"/usr/local/share/applications",
		"/usr/share/applications",
	})

	// XDG_DATA_HOME will override the first path
	s.mockEnv("XDG_DATA_HOME", "/home/user/share")
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/share/applications",
		"/usr/local/share/applications",
		"/usr/share/applications",
	})

	// XDG_DATA_DIRS changes the remaining paths
	s.mockEnv("XDG_DATA_DIRS", "/usr/share:/var/lib/snapd/desktop")
	c.Check(userd.DesktopFileSearchPath(), DeepEquals, []string{
		"/home/user/share/applications",
		"/usr/share/applications",
		"/var/lib/snapd/desktop/applications",
	})
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileIDToFilenameSucceedsWithValidId(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	var desktopIdTests = []struct {
		id     string
		expect string
	}{
		{"mir-kiosk-scummvm_mir-kiosk-scummvm.desktop", "/var/lib/snapd/desktop/applications/mir-kiosk-scummvm_mir-kiosk-scummvm.desktop"},
		{"foo-bar-baz.desktop", "/var/lib/snapd/desktop/applications/foo-bar/baz.desktop"},
		{"baz-foo-bar.desktop", "/var/lib/snapd/desktop/applications/baz/foo-bar.desktop"},
		{"shadow-test.desktop", "/usr/share/applications/shadow-test.desktop"},
	}

	for _, test := range desktopIdTests {
		actual, err := userd.DesktopFileIDToFilename(test.id)
		c.Assert(err, IsNil)
		c.Assert(actual, Equals, test.expect)
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestDesktopFileIDToFilenameFailsWithInvalidId(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	var desktopIdTests = []string{
		"mir-kiosk-scummvm-mir-kiosk-scummvm.desktop",
		"bar-foo-baz.desktop",
		"bar-baz-foo.desktop",
		"foo-bar_foo-bar.desktop",
		// special path segments cannot be smuggled inside desktop IDs
		"bar-..-vlc_vlc.desktop",
		"foo-bar/baz.desktop",
		"bar/../vlc_vlc.desktop",
		"../applications/vlc_vlc.desktop",
		// Other invalid desktop IDs
		"---------foo-bar-baz.desktop",
		"foo-bar-baz.desktop-foo-bar",
		"not-a-dot-desktop",
		"以临时配置文件打开新-non-ascii-here-too.desktop",
	}

	for _, id := range desktopIdTests {
		_, err := userd.DesktopFileIDToFilename(id)
		c.Check(err, ErrorMatches, `cannot find desktop file for ".*"`, Commentf(id))
	}
}

func (s *privilegedDesktopLauncherInternalSuite) TestVerifyDesktopFileLocation(c *C) {
	restore := userd.MockRegularFileExists(existsOnMockFileSystem)
	defer restore()
	s.mockEnv("XDG_DATA_DIRS", "/usr/local/share:/usr/share:/var/lib/snapd/desktop")

	// Resolved desktop files belonging to snaps will pass verification:
	filename, err := userd.DesktopFileIDToFilename("mir-kiosk-scummvm_mir-kiosk-scummvm.desktop")
	c.Assert(err, IsNil)
	err = userd.VerifyDesktopFileLocation(filename)
	c.Check(err, IsNil)

	// Desktop IDs belonging to host system apps fail:
	filename, err = userd.DesktopFileIDToFilename("shadow-test.desktop")
	c.Assert(err, IsNil)
	err = userd.VerifyDesktopFileLocation(filename)
	c.Check(err, ErrorMatches, "only launching snap applications from /var/lib/snapd/desktop/applications is supported")
}
