// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package wrappers_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

func makeMockSnapdSnap(c *C) *snap.Info {
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapUserServicesDir, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnapWithFiles(c, snapdYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		// system services
		{"lib/systemd/system/snapd.service", "[Unit]\nExecStart=/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start"},
		{"lib/systemd/system/snapd.system-shutdown.service", "[Unit]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start"},
		{"lib/systemd/system/snapd.autoimport.service", "[Unit]\nExecStart=/usr/bin/snap auto-import"},
		{"lib/systemd/system/snapd.socket", "[Unit]\n[Socket]\nListenStream=/run/snapd.socket"},
		{"lib/systemd/system/snapd.snap-repair.timer", "[Unit]\n[Timer]\nOnCalendar=*-*-* 5,11,17,23:00"},
		// user services
		{"usr/lib/systemd/user/snapd.session-agent.service", "[Unit]\nExecStart=/usr/bin/snap session-agent"},
		{"usr/lib/systemd/user/snapd.session-agent.socket", "[Unit]\n[Socket]\nListenStream=%t/snap-session.socket"},
	})

	return info
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	// reset root dir
	dirs.SetRootDir(s.tempdir)

	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] == "show" && cmd[1] == "--property=Id,ActiveState,UnitFileState,Type" {
			s := fmt.Sprintf("Type=oneshot\nId=%s\nActiveState=inactive\nUnitFileState=enabled\n", cmd[2])
			return []byte(s), nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	mountUnit := fmt.Sprintf(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=%s/snap/snapd/1/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
WantedBy=snapd.service
`, dirs.GlobalRootDir)
	for _, entry := range [][]string{{
		// check that snapd.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start", dirs.SnapMountDir),
	}, {
		// check that snapd.autoimport.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/bin/snap auto-import", dirs.SnapMountDir),
	}, {
		// check that snapd.system-shutdown.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service"),
		// and paths *do not* get re-written
		"[Unit]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start",
	}, {
		// check that usr-lib-snapd.mount is created
		filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"),
		mountUnit,
	}, {
		// check that snapd.session-agent.service is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\nExecStart=%s/snapd/1/usr/bin/snap session-agent", dirs.SnapMountDir),
	}, {
		// check that snapd.session-agent.socket is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"),
		"[Unit]\n[Socket]\nListenStream=%t/snap-session.socket",
	}} {
		c.Check(entry[0], testutil.FileEquals, entry[1])
	}

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", dirs.GlobalRootDir, "enable", "usr-lib-snapd.mount"},
		{"stop", "usr-lib-snapd.mount"},
		{"show", "--property=ActiveState", "usr-lib-snapd.mount"},
		{"start", "usr-lib-snapd.mount"},
		{"daemon-reload"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.snap-repair.timer"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.snap-repair.timer"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.socket"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.socket"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.system-shutdown.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.system-shutdown.service"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.autoimport.service"},
		{"stop", "snapd.autoimport.service"},
		{"show", "--property=ActiveState", "snapd.autoimport.service"},
		{"start", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.snap-repair.timer"},
		{"stop", "snapd.snap-repair.timer"},
		{"show", "--property=ActiveState", "snapd.snap-repair.timer"},
		{"start", "snapd.snap-repair.timer"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.socket"},
		{"start", "snapd.service"},
		{"start", "--no-block", "snapd.seeded.service"},
		{"start", "--no-block", "snapd.autoimport.service"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "disable", "snapd.session-agent.service"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "enable", "snapd.session-agent.service"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "disable", "snapd.session-agent.socket"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "enable", "snapd.session-agent.socket"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	// check that snapd services were *not* created
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket")), Equals, false)

	// check that no systemctl calls happened
	c.Check(s.sysdLog, IsNil)
}

func (s *servicesTestSuite) TestRemoveSnapServicesForSnapdOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	// reset root dir
	dirs.SetRootDir(s.tempdir)

	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		if cmd[0] == "show" && cmd[1] == "--property=Id,ActiveState,UnitFileState,Type" {
			s := fmt.Sprintf("Type=oneshot\nId=%s\nActiveState=inactive\nUnitFileState=enabled\n", cmd[2])
			return []byte(s), nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	info := makeMockSnapdSnap(c)

	units := [][]string{
		{filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"), "from-snapd"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.service"), "from-snapd"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.socket"), "from-snapd"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.snap-repair.timer"), "from-snapd"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service"), "from-snapd"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service"), "from-snapd"},
		{filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"), "from-snapd"},
		{filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"), "from-snapd"},
		// extra unit not present in core snap
		{filepath.Join(dirs.SnapServicesDir, "snapd.not-in-core.service"), "from-snapd"},
	}
	// content list uses absolute paths already
	snaptest.PopulateDir("/", units)

	// add the extra unit to the snap
	snaptest.PopulateDir("/", [][]string{
		{filepath.Join(info.MountDir(), "lib/systemd/system/snapd.not-in-core.service"), "from-snapd"},
	})

	coreUnits := [][]string{
		{filepath.Join(dirs.GlobalRootDir, "lib/systemd/system/snapd.service"), "# X-Snapd-Snap: do-not-start"},
		{filepath.Join(dirs.GlobalRootDir, "lib/systemd/system/snapd.socket"), "from-core"},
		{filepath.Join(dirs.GlobalRootDir, "lib/systemd/system/snapd.snap-repair.timer"), "from-core"},
		{filepath.Join(dirs.GlobalRootDir, "lib/systemd/system/snapd.autoimport.service"), "from-core"},
		{filepath.Join(dirs.GlobalRootDir, "lib/systemd/system/snapd.system-shutdown.service"), "# X-Snapd-Snap: do-not-start"},
		{filepath.Join(dirs.GlobalRootDir, "usr/lib/systemd/user/snapd.session-agent.service"), "from-core"},
		{filepath.Join(dirs.GlobalRootDir, "usr/lib/systemd/user/snapd.session-agent.socket"), "from-core"},
	}
	// content list uses absolute paths already
	snaptest.PopulateDir("/", coreUnits)

	// remove the snapd service
	err := wrappers.UndoSnapdServicesOnCore(info, progress.Null)
	c.Assert(err, IsNil)

	for _, unit := range units {
		c.Check(unit[0], testutil.FileAbsent)
	}

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "disable", "snapd.socket"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.socket"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.autoimport.service"},
		{"stop", "snapd.autoimport.service"},
		{"show", "--property=ActiveState", "snapd.autoimport.service"},
		{"start", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.not-in-core.service"},
		{"stop", "snapd.not-in-core.service"},
		{"show", "--property=ActiveState", "snapd.not-in-core.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.system-shutdown.service"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.system-shutdown.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.snap-repair.timer"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.snap-repair.timer"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.snap-repair.timer"},
		{"stop", "snapd.snap-repair.timer"},
		{"show", "--property=ActiveState", "snapd.snap-repair.timer"},
		{"start", "snapd.snap-repair.timer"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "disable", "snapd.session-agent.service"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "enable", "snapd.session-agent.service"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "disable", "snapd.session-agent.socket"},
		{"--user", "--global", "--root", dirs.GlobalRootDir, "enable", "snapd.session-agent.socket"},
		{"--root", dirs.GlobalRootDir, "disable", "usr-lib-snapd.mount"},
		{"stop", "usr-lib-snapd.mount"},
		{"show", "--property=ActiveState", "usr-lib-snapd.mount"},
	})
}
