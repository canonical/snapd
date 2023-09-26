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
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
)

type privilegedDesktopLauncherSuite struct {
	testutil.BaseTest

	launcher *userd.PrivilegedDesktopLauncher
}

var _ = Suite(&privilegedDesktopLauncherSuite{})

func (s *privilegedDesktopLauncherSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.launcher = &userd.PrivilegedDesktopLauncher{}

	c.Assert(os.MkdirAll(dirs.SnapDesktopFilesDir, 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/usr/share/applications"), 0755), IsNil)

	var rawMircadeDesktop = `[Desktop Entry]
  X-SnapInstanceName=mircade
  Name=mircade
  Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mircade_mircade.desktop /snap/bin/mircade
  Icon=/snap/mircade/143/meta/gui/mircade.png
  Comment=Sample confined desktop
  Type=Application
  Categories=Game
  `
	tmpMircadeDesktop := strings.Replace(rawMircadeDesktop, "/var/lib/snapd/desktop/applications", dirs.SnapDesktopFilesDir, -1)
	desktopContent := strings.Replace(tmpMircadeDesktop, "/snap/bin/", dirs.SnapBinariesDir, -1)

	deskTopFile := filepath.Join(dirs.SnapDesktopFilesDir, "mircade_mircade.desktop")
	c.Assert(os.WriteFile(deskTopFile, []byte(desktopContent), 0644), IsNil)

	// Create a shadowed desktop file ID
	c.Assert(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/usr/share/applications/shadow-test.desktop"), []byte("[Desktop Entry]"), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDesktopFilesDir, "shadow-test.desktop"), []byte("[Desktop Entry]"), 0644), IsNil)

	s.mockEnv("HOME", filepath.Join(dirs.GlobalRootDir, "/home/user"))
	s.mockEnv("XDG_DATA_HOME", "")
	s.mockEnv("XDG_DATA_DIRS", strings.Join([]string{
		filepath.Join(dirs.GlobalRootDir, "/usr/share"),
		filepath.Dir(dirs.SnapDesktopFilesDir),
	}, ":"))

	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("systemd 246\n+PAM and more"), nil
	})
	s.AddCleanup(restore)
}

func (s *privilegedDesktopLauncherSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *privilegedDesktopLauncherSuite) mockEnv(key, value string) {
	old := os.Getenv(key)
	os.Setenv(key, value)
	s.AddCleanup(func() {
		os.Setenv(key, old)
	})
}

func (s *privilegedDesktopLauncherSuite) TestDesktopFileLookup(c *C) {
	// We have more extensive tests for this API in
	// privileged_desktop_launcher_internal_test.go: here we just
	// test it without mocking the stat calls.
	filename, err := userd.DesktopFileIDToFilename("mircade_mircade.desktop")
	c.Assert(err, IsNil)
	err = userd.VerifyDesktopFileLocation(filename)
	c.Check(err, IsNil)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntrySucceedsWithGoodDesktopId(c *C) {
	cmd := testutil.MockCommand(c, "systemd-run", "true")
	defer cmd.Restore()

	err := s.launcher.OpenDesktopEntry("mircade_mircade.desktop", ":some-dbus-sender")
	c.Check(err, IsNil)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntryFailsWithBadDesktopId(c *C) {
	cmd := testutil.MockCommand(c, "systemd-run", "false")
	defer cmd.Restore()

	err := s.launcher.OpenDesktopEntry("not-mircade_mircade.desktop", ":some-dbus-sender")
	c.Assert(err, ErrorMatches, `cannot find desktop file for "not-mircade_mircade.desktop"`)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntryFailsWithBadExecutable(c *C) {
	cmd := testutil.MockCommand(c, "systemd-run", "false")
	defer cmd.Restore()

	err := s.launcher.OpenDesktopEntry("mircade_mircade.desktop", ":some-dbus-sender")
	c.Check(err, ErrorMatches, `cannot run \[.*\]: exit status 1`)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntryFailsForNonSnap(c *C) {
	cmd := testutil.MockCommand(c, "systemd-run", "false")
	defer cmd.Restore()

	err := s.launcher.OpenDesktopEntry("shadow-test.desktop", ":some-dbus-sender")
	c.Check(err, ErrorMatches, `only launching snap applications from .* is supported`)
}
