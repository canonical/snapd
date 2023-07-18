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
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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
	err = os.MkdirAll(dirs.SnapDBusSystemPolicyDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapDBusSessionPolicyDir, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnapWithFiles(c, snapdYaml, &snap.SideInfo{Revision: snap.R(1)}, [][]string{
		// system services
		{"lib/systemd/system/snapd.service", "[Unit]\n[Service]\nExecStart=/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start"},
		{"lib/systemd/system/snapd.system-shutdown.service", "[Unit]\n[Service]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start"},
		{"lib/systemd/system/snapd.autoimport.service", "[Unit]\n[Service]\nExecStart=/usr/bin/snap auto-import"},
		{"lib/systemd/system/snapd.socket", "[Unit]\n[Socket]\nListenStream=/run/snapd.socket"},
		{"lib/systemd/system/snapd.snap-repair.timer", "[Unit]\n[Timer]\nOnCalendar=*-*-* 5,11,17,23:00"},
		// user services
		{"usr/lib/systemd/user/snapd.session-agent.service", "[Unit]\n[Service]\nExecStart=/usr/bin/snap session-agent"},
		{"usr/lib/systemd/user/snapd.session-agent.socket", "[Unit]\n[Socket]\nListenStream=%t/snap-session.socket"},
		// D-Bus configuration
		{"usr/share/dbus-1/session.d/snapd.session-services.conf", "<busconfig/>"},
		{"usr/share/dbus-1/system.d/snapd.system-services.conf", "<busconfig/>"},
		// Extra non-snapd D-Bus config that shouldn't be copied
		{"usr/share/dbus-1/system.d/io.netplan.Netplan.conf", "<busconfig/>"},
		// D-Bus activation files
		{"usr/share/dbus-1/services/io.snapcraft.Launcher.service", "[D-BUS Service]\nName=io.snapcraft.Launcher"},
		{"usr/share/dbus-1/services/io.snapcraft.Prompt.service", "[D-BUS Service]\nName=io.snapcraft.Prompt"},
		{"usr/share/dbus-1/services/io.snapcraft.Settings.service", "[D-BUS Service]\nName=io.snapcraft.Settings"},
		{"usr/share/dbus-1/services/io.snapcraft.SessionAgent.service", "[D-BUS Service]\nName=io.snapcraft.SessionAgent"},
	})

	return info
}

type mockSystemctlError struct {
	msg      string
	exitCode int
}

func (m *mockSystemctlError) Msg() []byte {
	return []byte(m.msg)
}

func (m *mockSystemctlError) ExitCode() int {
	return m.exitCode
}

func (m *mockSystemctlError) Error() string {
	return fmt.Sprintf("mocked systemctl error: code: %v msg: %q", m.exitCode, m.msg)
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
		if len(cmd) == 2 && cmd[0] == "is-enabled" {
			// pretend snapd.socket is disabled
			if cmd[1] == "snapd.socket" {
				return []byte("disabled"), &mockSystemctlError{msg: "disabled", exitCode: 1}
			}
			return []byte("enabled"), nil
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapdSnapServices(info, nil, progress.Null)
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
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.autoimport.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/bin/snap auto-import\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.system-shutdown.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service"),
		// and paths *do not* get re-written
		"[Unit]\n[Service]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start",
	}, {
		// check that usr-lib-snapd.mount is created
		filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"),
		mountUnit,
	}, {
		// check that snapd.session-agent.service is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/bin/snap session-agent\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.session-agent.socket is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"),
		"[Unit]\n[Socket]\nListenStream=%t/snap-session.socket",
	}, {
		filepath.Join(dirs.SnapDBusSystemPolicyDir, "snapd.system-services.conf"),
		"<busconfig/>",
	}, {
		filepath.Join(dirs.SnapDBusSessionPolicyDir, "snapd.session-services.conf"),
		"<busconfig/>",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service"),
		"[D-BUS Service]\nName=io.snapcraft.Launcher",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service"),
		"[D-BUS Service]\nName=io.snapcraft.Settings",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service"),
		"[D-BUS Service]\nName=io.snapcraft.SessionAgent",
	}} {
		c.Check(entry[0], testutil.FileEquals, entry[1])
	}

	// Non-snapd D-Bus config is not copied
	c.Check(filepath.Join(dirs.SnapDBusSystemPolicyDir, "io.netplan.Netplan.conf"), testutil.FileAbsent)

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"daemon-reload"},
		{"--no-reload", "enable", "usr-lib-snapd.mount"},
		{"stop", "usr-lib-snapd.mount"},
		{"show", "--property=ActiveState", "usr-lib-snapd.mount"},
		{"start", "usr-lib-snapd.mount"},
		{"daemon-reload"},
		{"is-enabled", "snapd.autoimport.service"},
		{"is-enabled", "snapd.service"},
		{"is-enabled", "snapd.snap-repair.timer"},
		// test pretends snapd.socket is disabled and needs enabling
		{"is-enabled", "snapd.socket"},
		{"--no-reload", "enable", "snapd.socket"},
		{"is-enabled", "snapd.system-shutdown.service"},
		{"is-active", "snapd.autoimport.service"},
		{"stop", "snapd.autoimport.service"},
		{"show", "--property=ActiveState", "snapd.autoimport.service"},
		{"start", "snapd.autoimport.service"},
		{"is-active", "snapd.snap-repair.timer"},
		{"stop", "snapd.snap-repair.timer"},
		{"show", "--property=ActiveState", "snapd.snap-repair.timer"},
		{"start", "snapd.snap-repair.timer"},
		{"is-active", "snapd.socket"},
		{"start", "--no-block", "snapd.service"},
		{"start", "--no-block", "snapd.seeded.service"},
		{"start", "--no-block", "snapd.autoimport.service"},
		{"--user", "--global", "--no-reload", "disable", "snapd.session-agent.service"},
		{"--user", "--global", "--no-reload", "enable", "snapd.session-agent.service"},
		{"--user", "--global", "--no-reload", "disable", "snapd.session-agent.socket"},
		{"--user", "--global", "--no-reload", "enable", "snapd.session-agent.socket"},
		{"--user", "daemon-reload"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnCorePreseeding(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	// reset root dir
	dirs.SetRootDir(s.tempdir)

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapdSnapServices(info, &wrappers.AddSnapdSnapServicesOptions{Preseeding: true}, progress.Null)
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
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.autoimport.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/bin/snap auto-import\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.system-shutdown.service is created
		filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service"),
		// and paths *do not* get re-written
		"[Unit]\n[Service]\nExecStart=/bin/umount --everything\n# X-Snapd-Snap: do-not-start",
	}, {
		// check that usr-lib-snapd.mount is created
		filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"),
		mountUnit,
	}, {
		// check that snapd.session-agent.service is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"),
		// and paths get re-written
		fmt.Sprintf("[Unit]\n[Service]\nExecStart=%[1]s/snapd/1/usr/bin/snap session-agent\n[Unit]\nRequiresMountsFor=%[1]s/snapd/1\n", dirs.SnapMountDir),
	}, {
		// check that snapd.session-agent.socket is created
		filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"),
		"[Unit]\n[Socket]\nListenStream=%t/snap-session.socket",
	}, {
		filepath.Join(dirs.SnapDBusSystemPolicyDir, "snapd.system-services.conf"),
		"<busconfig/>",
	}, {
		filepath.Join(dirs.SnapDBusSessionPolicyDir, "snapd.session-services.conf"),
		"<busconfig/>",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service"),
		"[D-BUS Service]\nName=io.snapcraft.Launcher",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service"),
		"[D-BUS Service]\nName=io.snapcraft.Settings",
	}, {
		filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service"),
		"[D-BUS Service]\nName=io.snapcraft.SessionAgent",
	}} {
		c.Check(entry[0], testutil.FileEquals, entry[1])
	}

	// Non-snapd D-Bus config is not copied
	c.Check(filepath.Join(dirs.SnapDBusSystemPolicyDir, "io.netplan.Netplan.conf"), testutil.FileAbsent)

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--root", s.tempdir, "enable", "usr-lib-snapd.mount"},
		{"--root", s.tempdir, "enable", "snapd.autoimport.service"},
		{"--root", s.tempdir, "enable", "snapd.service"},
		{"--root", s.tempdir, "enable", "snapd.snap-repair.timer"},
		{"--root", s.tempdir, "enable", "snapd.socket"},
		{"--root", s.tempdir, "enable", "snapd.system-shutdown.service"},
		{"--user", "--global", "--no-reload", "disable", "snapd.session-agent.service"},
		{"--user", "--global", "--no-reload", "enable", "snapd.session-agent.service"},
		{"--user", "--global", "--no-reload", "disable", "snapd.session-agent.socket"},
		{"--user", "--global", "--no-reload", "enable", "snapd.session-agent.socket"},
	})
}

func (s *servicesTestSuite) TestAddSnapServicesForSnapdOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	info := makeMockSnapdSnap(c)
	// add the snapd service
	err := wrappers.AddSnapdSnapServices(info, nil, progress.Null)
	c.Assert(err, IsNil)

	// check that snapd services were *not* created
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSystemPolicyDir, "snapd.system-services.conf")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSessionPolicyDir, "snapd.session-services.conf")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service")), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service")), Equals, false)

	// check that no systemctl calls happened
	c.Check(s.sysdLog, IsNil)
}

func (s *servicesTestSuite) TestAddSessionServicesWithReadOnlyFilesystem(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restoreEnsureDirState := wrappers.MockEnsureDirState(func(dir string, glob string, content map[string]osutil.FileState) (changed, removed []string, err error) {
		return nil, nil, &os.PathError{Err: syscall.EROFS}
	})
	defer restoreEnsureDirState()

	info := makeMockSnapdSnap(c)

	logBuf, restore := logger.MockLogger()
	defer restore()

	// add the snapd service
	err := wrappers.AddSnapdSnapServices(info, nil, progress.Null)

	// didn't fail despite of read-only SnapDBusSessionPolicyDir
	c.Assert(err, IsNil)

	// check that snapd services were *not* created
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.service")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.autoimport.service")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "snapd.system-shutdown.service")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSystemPolicyDir, "snapd.system-services.conf")), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapDBusSessionPolicyDir, "snapd.session-services.conf")), Equals, false)

	c.Assert(logBuf.String(), testutil.Contains, "/etc/dbus-1/session.d appears to be read-only, could not write snapd dbus config files")
}

func (s *servicesTestSuite) TestAddSnapdServicesWithNonSnapd(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := snaptest.MockInfo(c, "name: foo\nversion: 1.0", &snap.SideInfo{})
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	err := wrappers.AddSnapdSnapServices(info, nil, progress.Null)
	c.Assert(err, ErrorMatches, `internal error: adding explicit snapd services for snap "foo" type "app" is unexpected`)
}

func (s *servicesTestSuite) TestRemoveSnapServicesForFirstInstallSnapdOnCore(c *C) {
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
		if len(cmd) == 4 && cmd[2] == "is-enabled" {
			// pretend snapd.socket is disabled
			if cmd[3] == "snapd.socket" {
				return []byte("disabled"), &mockSystemctlError{msg: "disabled", exitCode: 1}
			}
			return []byte("enabled"), nil
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
		{filepath.Join(dirs.SnapDBusSystemPolicyDir, "snapd.system-services.conf"), "from-snapd"},
		{filepath.Join(dirs.SnapDBusSessionPolicyDir, "snapd.session-services.conf"), "from-snapd"},
		// extra unit not present in core snap
		{filepath.Join(dirs.SnapServicesDir, "snapd.not-in-core.service"), "from-snapd"},
		// D-Bus service activation files
		{filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service"), "from-snapd"},
		{filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service"), "from-snapd"},
		{filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service"), "from-snapd"},
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
	err := wrappers.RemoveSnapdSnapServicesOnCore(info, progress.Null)
	c.Assert(err, IsNil)

	for _, unit := range units {
		c.Check(unit[0], testutil.FileAbsent)
	}

	// check the systemctl calls
	c.Check(s.sysdLog, DeepEquals, [][]string{
		// pretend snapd socket needs enabling
		{"--root", dirs.GlobalRootDir, "is-enabled", "snapd.socket"},
		{"--root", dirs.GlobalRootDir, "enable", "snapd.socket"},

		{"--root", dirs.GlobalRootDir, "is-enabled", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "is-active", "snapd.autoimport.service"},
		{"stop", "snapd.autoimport.service"},
		{"show", "--property=ActiveState", "snapd.autoimport.service"},
		{"start", "snapd.autoimport.service"},
		{"--root", dirs.GlobalRootDir, "disable", "snapd.not-in-core.service"},
		{"stop", "snapd.not-in-core.service"},
		{"show", "--property=ActiveState", "snapd.not-in-core.service"},
		{"--root", dirs.GlobalRootDir, "is-enabled", "snapd.service"},
		{"--root", dirs.GlobalRootDir, "is-enabled", "snapd.system-shutdown.service"},
		{"--root", dirs.GlobalRootDir, "is-enabled", "snapd.snap-repair.timer"},
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

func (s *servicesTestSuite) TestRemoveSnapdServicesWithNonSnapd(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := snaptest.MockInfo(c, "name: foo\nversion: 1.0", &snap.SideInfo{})
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	err := wrappers.RemoveSnapdSnapServicesOnCore(info, progress.Null)
	c.Assert(err, ErrorMatches, `internal error: removing explicit snapd services for snap "foo" type "app" is unexpected`)
}

func (s *servicesTestSuite) TestDeriveSnapdDBusConfig(c *C) {
	info := makeMockSnapdSnap(c)

	sessionContent, systemContent, err := wrappers.DeriveSnapdDBusConfig(info)
	c.Assert(err, IsNil)
	c.Check(sessionContent, DeepEquals, map[string]osutil.FileState{
		"snapd.session-services.conf": &osutil.FileReference{
			Path: filepath.Join(info.MountDir(), "usr/share/dbus-1/session.d/snapd.session-services.conf"),
		},
	})
	c.Check(systemContent, DeepEquals, map[string]osutil.FileState{
		"snapd.system-services.conf": &osutil.FileReference{
			Path: filepath.Join(info.MountDir(), "usr/share/dbus-1/system.d/snapd.system-services.conf"),
		},
	})
}
