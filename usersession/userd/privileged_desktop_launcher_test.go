// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userd"
)

type privilegedDesktopLauncherSuite struct {
	launcher *userd.PrivilegedDesktopLauncher
}

var _ = Suite(&privilegedDesktopLauncherSuite{})

func (s *privilegedDesktopLauncherSuite) SetUpTest(c *C) {
	s.launcher = &userd.PrivilegedDesktopLauncher{}
}

func (s *privilegedDesktopLauncherSuite) TearDownTest(c *C) {
}

var mircadeDesktop = `[Desktop Entry]
X-SnapInstanceName=mircade
Name=mircade
Exec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/mircade_mircade.desktop /snap/bin/mircade
Icon=/snap/mircade/143/meta/gui/mircade.png
Comment=Sample confined desktop
Type=Application
Categories=Game
`

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntrySucceedsWithGoodDesktopId(c *C) {
	dirs.SetRootDir(c.MkDir())
	cmd := testutil.MockCommand(c, "systemd-run", "true")
	defer cmd.Restore()

	deskTopFile := filepath.Join(dirs.SnapDesktopFilesDir, "mircade_mircade.desktop")
	err := os.MkdirAll(filepath.Dir(deskTopFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(deskTopFile, []byte(strings.Replace(mircadeDesktop, "/var/lib/snapd/desktop/applications", dirs.SnapDesktopFilesDir, -1)), 0644)
	c.Assert(err, IsNil)

	err = s.launcher.OpenDesktopEntry("mircade_mircade.desktop", ":some-dbus-sender")
	c.Assert(err, IsNil)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntryFailsWithBadDesktopId(c *C) {
	dirs.SetRootDir(c.MkDir())
	cmd := testutil.MockCommand(c, "systemd-run", "true")
	defer cmd.Restore()

	deskTopFile := filepath.Join(dirs.SnapDesktopFilesDir, "mircade_mircade.desktop")
	err := os.MkdirAll(filepath.Dir(deskTopFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(deskTopFile, []byte(strings.Replace(mircadeDesktop, "/var/lib/snapd/desktop/applications", dirs.SnapDesktopFilesDir, -1)), 0644)
	c.Assert(err, IsNil)

	err = s.launcher.OpenDesktopEntry("not-mircade_mircade.desktop", ":some-dbus-sender")
	c.Assert(err, NotNil)
}

func (s *privilegedDesktopLauncherSuite) TestOpenDesktopEntryFailsWithBadExecutable(c *C) {
	dirs.SetRootDir(c.MkDir())
	cmd := testutil.MockCommand(c, "systemd-run", "false")
	defer cmd.Restore()

	deskTopFile := filepath.Join(dirs.SnapDesktopFilesDir, "mircade_mircade.desktop")
	err := os.MkdirAll(filepath.Dir(deskTopFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(deskTopFile, []byte(strings.Replace(mircadeDesktop, "/var/lib/snapd/desktop/applications", dirs.SnapDesktopFilesDir, -1)), 0644)
	c.Assert(err, IsNil)

	err = s.launcher.OpenDesktopEntry("mircade_mircade.desktop", ":some-dbus-sender")
	c.Assert(err, NotNil)
}
