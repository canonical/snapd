// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	"github.com/snapcore/snapd/wrappers"
)

type linkSuiteCommon struct {
	testutil.BaseTest

	be backend.Backend

	perfTimings *timings.Timings
}

func (s *linkSuiteCommon) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.perfTimings = timings.New(nil)
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
	s.AddCleanup(restore)
}

type linkSuite struct {
	linkSuiteCommon
}

var _ = Suite(&linkSuite{})

func (s *linkSuite) TestLinkDoUndoGenerateWrappers(c *C) {
	const yaml = `name: hello
version: 1.0
environment:
 KEY: value

slots:
  system-slot:
    interface: dbus
    bus: system
    name: org.example.System
  session-slot:
    interface: dbus
    bus: session
    name: org.example.Session

apps:
 bin:
   command: bin
 svc:
   command: svc
   daemon: simple
 dbus-system:
   daemon: simple
   activates-on: [system-slot]
 dbus-session:
   daemon: simple
   daemon-scope: user
   activates-on: [session-slot]
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 2)
	l, err = filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDBusSystemServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDBusSessionServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	// undo will remove
	err = s.be.UnlinkSnap(info, backend.LinkContext{}, progress.Null)
	c.Assert(err, IsNil)

	l, err = filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapUserServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDBusSystemServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDBusSessionServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
}

func (s *linkSuite) TestLinkDoUndoGenerateWrappersNoSkipBinaries(c *C) {
	const yaml = `name: hello
version: 1.0

apps:
 foo:
   command: foo
 bar:
   command: bar
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})
	// create gui/icons dir
	guiDir := filepath.Join(info.MountDir(), "meta", "gui")
	iconsDir := filepath.Join(info.MountDir(), "meta", "gui", "icons")
	c.Assert(os.MkdirAll(guiDir, 0755), IsNil)
	c.Assert(os.MkdirAll(iconsDir, 0755), IsNil)
	// add desktop files
	c.Assert(os.WriteFile(filepath.Join(guiDir, "foo.desktop"), []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/icon.png
Exec=foo
`), 0644), IsNil)
	// add icons
	c.Assert(os.WriteFile(filepath.Join(iconsDir, "snap.hello.png"), []byte(""), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(iconsDir, "snap.hello.svg"), []byte(""), 0644), IsNil)

	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopFilesDir, "*.desktop"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopIconsDir, "snap.hello.*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)

	// undo will remove
	err = s.be.UnlinkSnap(info, backend.LinkContext{}, progress.Null)
	c.Assert(err, IsNil)

	l, err = filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopFilesDir, "*.desktop"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopIconsDir, "snap.hello.*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 0)
}

func (s *linkSuite) TestLinkDoUndoGenerateWrappersSkipBinaries(c *C) {
	const yaml = `name: hello
version: 1.0

apps:
 foo:
   command: foo
 bar:
   command: bar
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})
	// create gui/icons dir
	guiDir := filepath.Join(info.MountDir(), "meta", "gui")
	iconsDir := filepath.Join(info.MountDir(), "meta", "gui", "icons")
	c.Assert(os.MkdirAll(guiDir, 0755), IsNil)
	c.Assert(os.MkdirAll(iconsDir, 0755), IsNil)
	// add desktop files
	c.Assert(os.WriteFile(filepath.Join(guiDir, "foo.desktop"), []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/icon.png
Exec=foo
`), 0644), IsNil)
	// add icons
	c.Assert(os.WriteFile(filepath.Join(iconsDir, "snap.hello.png"), []byte(""), 0644), IsNil)
	c.Assert(os.WriteFile(filepath.Join(iconsDir, "snap.hello.svg"), []byte(""), 0644), IsNil)

	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopFilesDir, "*.desktop"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopIconsDir, "snap.hello.*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)

	// unlink should skip binaries, icons and desktop files
	linkCtx := backend.LinkContext{
		SkipBinaries: true,
	}
	err = s.be.UnlinkSnap(info, linkCtx, progress.Null)
	c.Assert(err, IsNil)

	l, err = filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopFilesDir, "*.desktop"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapDesktopIconsDir, "snap.hello.*"))
	c.Assert(err, IsNil)
	c.Check(l, HasLen, 2)
}

func (s *linkSuite) TestLinkDoUndoCurrentSymlink(c *C) {
	const yaml = `name: hello
version: 1.0
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	reboot, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(reboot, Equals, boot.RebootInfo{RebootRequired: false})

	mountDir := info.MountDir()
	dataDir := info.DataDir()
	currentActiveSymlink := filepath.Join(mountDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentActiveDir, Equals, mountDir)

	currentDataSymlink := filepath.Join(dataDir, "..", "current")
	currentDataDir, err := filepath.EvalSymlinks(currentDataSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentDataDir, Equals, dataDir)

	// undo will remove the symlinks
	err = s.be.UnlinkSnap(info, backend.LinkContext{}, progress.Null)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(currentActiveSymlink), Equals, false)
	c.Check(osutil.FileExists(currentDataSymlink), Equals, false)

}

func (s *linkSuite) TestLinkSetNextBoot(c *C) {
	coreDev := boottest.MockDevice("base")

	bl := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	bl.SetBootBase("base_1.snap")

	const yaml = `name: base
version: 1.0
type: base
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	reboot, err := s.be.LinkSnap(info, coreDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(reboot, Equals, boot.RebootInfo{RebootRequired: true})
}

func (s *linkSuite) TestLinkNoSetNextBootWhenPreseeding(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	be := backend.NewForPreseedMode()
	coreDev := boottest.MockUC20Device("run", nil)

	bl := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	const yaml = `name: pc-kernel
version: 1.0
type: kernel
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	reboot, err := be.LinkSnap(info, coreDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(reboot, DeepEquals, boot.RebootInfo{})
}

func (s *linkSuite) TestLinkSnapdSnapCallsWrappersWithPreseedingFlag(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	var called bool
	restoreAddSnapdSnapWrappers := backend.MockWrappersAddSnapdSnapServices(func(s *snap.Info, opts *wrappers.AddSnapdSnapServicesOptions, inter wrappers.Interacter) (wrappers.SnapdRestart, error) {
		c.Check(opts.Preseeding, Equals, true)
		called = true
		return nil, nil
	})
	defer restoreAddSnapdSnapWrappers()

	be := backend.NewForPreseedMode()
	coreDev := boottest.MockUC20Device("run", nil)

	bl := boottest.MockUC20RunBootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bl)
	defer bootloader.Force(nil)

	const yaml = `name: snapd
version: 1.0
type: snapd
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := be.LinkSnap(info, coreDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *linkSuite) TestLinkDoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	const yaml = `name: hello
version: 1.0
environment:
 KEY: value
apps:
 bin:
   command: bin
 svc:
   command: svc
   daemon: simple
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	_, err = s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	mountDir := info.MountDir()
	dataDir := info.DataDir()
	currentActiveSymlink := filepath.Join(mountDir, "..", "current")
	currentActiveDir, err := filepath.EvalSymlinks(currentActiveSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentActiveDir, Equals, mountDir)

	currentDataSymlink := filepath.Join(dataDir, "..", "current")
	currentDataDir, err := filepath.EvalSymlinks(currentDataSymlink)
	c.Assert(err, IsNil)
	c.Assert(currentDataDir, Equals, dataDir)

	c.Check(filepath.Join(runinhibit.InhibitDir, "hello.lock"), testutil.FileAbsent)
}

func (s *linkSuite) TestLinkUndoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work

	const yaml = `name: hello
version: 1.0
apps:
 bin:
   command: bin
 svc:
   command: svc
   daemon: simple
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, backend.LinkContext{}, progress.Null)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, backend.LinkContext{}, progress.Null)
	c.Assert(err, IsNil)

	// no wrappers
	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)

	// no symlinks
	currentActiveSymlink := filepath.Join(info.MountDir(), "..", "current")
	currentDataSymlink := filepath.Join(info.DataDir(), "..", "current")
	c.Check(osutil.FileExists(currentActiveSymlink), Equals, false)
	c.Check(osutil.FileExists(currentDataSymlink), Equals, false)

	// no inhibition lock
	c.Check(filepath.Join(runinhibit.InhibitDir, "hello.lock"), testutil.FileAbsent)
}

func (s *linkSuite) TestLinkFailsForUnsetRevision(c *C) {
	info := &snap.Info{
		SuggestedName: "foo",
	}
	_, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, ErrorMatches, `cannot link snap "foo" with unset revision`)
}

func mockSnapdSnapForLink(c *C) (snapdSnap *snap.Info, units [][]string) {
	const yaml = `name: snapd
version: 1.0
type: snapd
`
	snapdUnits := [][]string{
		// system services
		{"lib/systemd/system/snapd.service", "[Unit]\n[Service]\nExecStart=/usr/lib/snapd/snapd\n# X-Snapd-Snap: do-not-start"},
		{"lib/systemd/system/snapd.socket", "[Unit]\n[Socket]\nListenStream=/run/snapd.socket"},
		{"lib/systemd/system/snapd.snap-repair.timer", "[Unit]\n[Timer]\nOnCalendar=*-*-* 5,11,17,23:00"},
		// user services
		{"usr/lib/systemd/user/snapd.session-agent.service", "[Unit]\n[Service]\nExecStart=/usr/bin/snap session-agent"},
		{"usr/lib/systemd/user/snapd.session-agent.socket", "[Unit]\n[Socket]\nListenStream=%t/snap-session.socket"},
	}
	otherFiles := [][]string{
		// D-Bus activation files
		{"usr/share/dbus-1/services/io.snapcraft.Launcher.service", "[D-BUS Service]\nName=io.snapcraft.Launcher"},
		{"usr/share/dbus-1/services/io.snapcraft.Settings.service", "[D-BUS Service]\nName=io.snapcraft.Settings"},
		{"usr/share/dbus-1/services/io.snapcraft.SessionAgent.service", "[D-BUS Service]\nName=io.snapcraft.SessionAgent"},
	}
	info := snaptest.MockSnapWithFiles(c, yaml, &snap.SideInfo{Revision: snap.R(11)}, append(snapdUnits, otherFiles...))
	return info, snapdUnits
}

func (s *linkSuite) TestLinkSnapdSnapOnCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapUserServicesDir, 0755)
	c.Assert(err, IsNil)

	info, _ := mockSnapdSnapForLink(c)

	reboot, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, boot.RebootInfo{RebootRequired: false})

	// system services
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.service"), testutil.FileContains,
		fmt.Sprintf("[Service]\nExecStart=%s/usr/lib/snapd/snapd\n", info.MountDir()))
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.socket"), testutil.FileEquals,
		"[Unit]\n[Socket]\nListenStream=/run/snapd.socket")
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.snap-repair.timer"), testutil.FileEquals,
		"[Unit]\n[Timer]\nOnCalendar=*-*-* 5,11,17,23:00")
	// user services
	c.Check(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"), testutil.FileContains,
		fmt.Sprintf("[Service]\nExecStart=%s/usr/bin/snap session-agent", info.MountDir()))
	c.Check(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"), testutil.FileEquals,
		"[Unit]\n[Socket]\nListenStream=%t/snap-session.socket")
	// auxiliary mount unit
	mountUnit := fmt.Sprintf(`[Unit]
Description=Make the snapd snap tooling available for the system
Before=snapd.service

[Mount]
What=%s/usr/lib/snapd
Where=/usr/lib/snapd
Type=none
Options=bind

[Install]
WantedBy=snapd.service
`, info.MountDir())
	c.Check(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"), testutil.FileEquals, mountUnit)
	// D-Bus service activation files for snap userd
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service"), testutil.FileEquals,
		"[D-BUS Service]\nName=io.snapcraft.Launcher")
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service"), testutil.FileEquals,
		"[D-BUS Service]\nName=io.snapcraft.Settings")
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service"), testutil.FileEquals,
		"[D-BUS Service]\nName=io.snapcraft.SessionAgent")
}

type linkCleanupSuite struct {
	linkSuiteCommon
	info *snap.Info
}

var _ = Suite(&linkCleanupSuite{})

func (s *linkCleanupSuite) SetUpTest(c *C) {
	s.linkSuiteCommon.SetUpTest(c)

	const yaml = `name: hello
version: 1.0
environment:
 KEY: value

slots:
  system-slot:
    interface: dbus
    bus: system
    name: org.example.System
  session-slot:
    interface: dbus
    bus: session
    name: org.example.Session

apps:
 foo:
   command: foo
 bar:
   command: bar
 svc:
   command: svc
   daemon: simple
 dbus-system:
   daemon: simple
   activates-on: [system-slot]
 dbus-session:
   daemon: simple
   daemon-scope: user
   activates-on: [session-slot]
`
	cmd := testutil.MockCommand(c, "update-desktop-database", "")
	s.AddCleanup(cmd.Restore)

	s.info = snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	guiDir := filepath.Join(s.info.MountDir(), "meta", "gui")
	c.Assert(os.MkdirAll(guiDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(guiDir, "bin.desktop"), []byte(`
[Desktop Entry]
Name=bin
Icon=${SNAP}/bin.png
Exec=bin
`), 0644), IsNil)

	// validity checks
	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir, dirs.SnapDBusSystemServicesDir, dirs.SnapDBusSessionServicesDir} {
		os.MkdirAll(d, 0755)
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Assert(err, IsNil, Commentf(d))
		c.Assert(l, HasLen, 0, Commentf(d))
	}
}

func (s *linkCleanupSuite) testLinkCleanupDirOnFail(c *C, dir string) {
	c.Assert(os.Chmod(dir, 0555), IsNil)
	defer os.Chmod(dir, 0755)

	_, err := s.be.LinkSnap(s.info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, NotNil)
	_, isPathError := err.(*os.PathError)
	_, isLinkError := err.(*os.LinkError)
	c.Assert(isPathError || isLinkError, Equals, true, Commentf("%#v", err))

	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir, dirs.SnapDBusSystemServicesDir, dirs.SnapDBusSessionServicesDir} {
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Check(err, IsNil, Commentf(d))
		c.Check(l, HasLen, 0, Commentf(d))
	}
}

func (s *linkCleanupSuite) TestLinkCleanupOnDesktopFail(c *C) {
	s.testLinkCleanupDirOnFail(c, dirs.SnapDesktopFilesDir)
}

func (s *linkCleanupSuite) TestLinkCleanupOnBinariesFail(c *C) {
	// this one is the trivial case _as the code stands today_,
	// but nothing guarantees that ordering.
	s.testLinkCleanupDirOnFail(c, dirs.SnapBinariesDir)
}

func (s *linkCleanupSuite) TestLinkCleanupOnServicesFail(c *C) {
	s.testLinkCleanupDirOnFail(c, dirs.SnapServicesDir)
}

func (s *linkCleanupSuite) TestLinkCleanupOnMountDirFail(c *C) {
	s.testLinkCleanupDirOnFail(c, filepath.Dir(s.info.MountDir()))
}

func (s *linkCleanupSuite) TestLinkCleanupOnDBusSystemFail(c *C) {
	s.testLinkCleanupDirOnFail(c, dirs.SnapDBusSystemServicesDir)
}

func (s *linkCleanupSuite) TestLinkCleanupOnDBusSessionFail(c *C) {
	s.testLinkCleanupDirOnFail(c, dirs.SnapDBusSessionServicesDir)
}

func (s *linkCleanupSuite) TestLinkCleanupOnSystemctlFail(c *C) {
	r := systemd.MockSystemctl(func(...string) ([]byte, error) {
		return nil, errors.New("ouchie")
	})
	defer r()

	_, err := s.be.LinkSnap(s.info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, ErrorMatches, "ouchie")

	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir} {
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Check(err, IsNil, Commentf(d))
		c.Check(l, HasLen, 0, Commentf(d))
	}
}

func (s *linkCleanupSuite) TestLinkCleansUpDataDirAndSymlinksOnSymlinkFail(c *C) {
	// validity check
	c.Assert(s.info.DataDir(), testutil.FileAbsent)

	// the mountdir symlink is currently the last thing in LinkSnap that can
	// make it fail, creating a symlink requires write permissions
	d := filepath.Dir(s.info.MountDir())
	c.Assert(os.Chmod(d, 0555), IsNil)
	defer os.Chmod(d, 0755)

	_, err := s.be.LinkSnap(s.info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, ErrorMatches, `(?i).*symlink.*permission denied.*`)

	c.Check(s.info.DataDir(), testutil.FileAbsent)
	c.Check(filepath.Join(s.info.DataDir(), "..", "current"), testutil.FileAbsent)
	c.Check(filepath.Join(s.info.MountDir(), "..", "current"), testutil.FileAbsent)
}

func (s *linkCleanupSuite) testLinkCleanupFailedSnapdSnapOnCorePastWrappers(c *C, firstInstall bool) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	info, _ := mockSnapdSnapForLink(c)

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapUserServicesDir, 0755)
	c.Assert(err, IsNil)

	// make snap mount dir non-writable, triggers error updating the current symlink
	snapdSnapDir := filepath.Dir(info.MountDir())

	if firstInstall {
		err := os.Remove(filepath.Join(snapdSnapDir, "1234"))
		c.Assert(err == nil || os.IsNotExist(err), Equals, true, Commentf("err: %v, err"))
	} else {
		err := os.Mkdir(filepath.Join(snapdSnapDir, "1234"), 0755)
		c.Assert(err, IsNil)
	}

	// triggers permission denied error when symlink is manipulated
	err = os.Chmod(snapdSnapDir, 0555)
	c.Assert(err, IsNil)
	defer os.Chmod(snapdSnapDir, 0755)

	linkCtx := backend.LinkContext{
		FirstInstall: firstInstall,
	}
	reboot, err := s.be.LinkSnap(info, mockDev, linkCtx, s.perfTimings)
	c.Assert(err, ErrorMatches, fmt.Sprintf("symlink %s /.*/snapd/current.*: permission denied", info.Revision))
	c.Assert(reboot, Equals, boot.RebootInfo{RebootRequired: false})

	checker := testutil.FilePresent
	if firstInstall {
		checker = testutil.FileAbsent
	}

	// system services
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.service"), checker)
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.socket"), checker)
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.snap-repair.timer"), checker)
	// user services
	c.Check(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"), checker)
	c.Check(filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.socket"), checker)
	c.Check(filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"), checker)
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FileAbsent)

	// D-Bus service activation
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Launcher.service"), checker)
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.Settings.service"), checker)
	c.Check(filepath.Join(dirs.SnapDBusSessionServicesDir, "io.snapcraft.SessionAgent.service"), checker)
}

func (s *linkCleanupSuite) TestLinkCleanupFailedSnapdSnapFirstInstallOnCore(c *C) {
	// test failure mode when snapd is first installed, its units were
	// correctly written and corresponding services were started, but
	// current symlink failed
	restore := release.MockOnClassic(false)
	defer restore()
	s.testLinkCleanupFailedSnapdSnapOnCorePastWrappers(c, true)
}

func (s *linkCleanupSuite) TestLinkCleanupFailedSnapdSnapNonFirstInstallOnCore(c *C) {
	// test failure mode when a new revision of snapd is installed, its was
	// units were correctly written and corresponding services were started,
	// but current symlink failed
	restore := release.MockOnClassic(false)
	defer restore()
	s.testLinkCleanupFailedSnapdSnapOnCorePastWrappers(c, false)
}

type snapdOnCoreUnlinkSuite struct {
	linkSuiteCommon
}

var _ = Suite(&snapdOnCoreUnlinkSuite{})

func (s *snapdOnCoreUnlinkSuite) TestUndoGeneratedWrappers(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapUserServicesDir, 0755)
	c.Assert(err, IsNil)

	info, snapdUnits := mockSnapdSnapForLink(c)
	// all generated units
	generatedSnapdUnits := append(snapdUnits,
		[]string{"usr-lib-snapd.mount", "mount unit"})

	toEtcUnitPath := func(p string) string {
		if strings.HasPrefix(p, "usr/lib/systemd/user") {
			return filepath.Join(dirs.SnapUserServicesDir, filepath.Base(p))
		}
		return filepath.Join(dirs.SnapServicesDir, filepath.Base(p))
	}

	reboot, err := s.be.LinkSnap(info, mockDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(reboot, Equals, boot.RebootInfo{RebootRequired: false})

	// validity checks
	c.Check(filepath.Join(dirs.SnapServicesDir, "snapd.service"), testutil.FileContains,
		fmt.Sprintf("[Service]\nExecStart=%s/usr/lib/snapd/snapd\n", info.MountDir()))
	// expecting all generated units to be present
	for _, entry := range generatedSnapdUnits {
		c.Check(toEtcUnitPath(entry[0]), testutil.FilePresent)
	}
	// linked snaps do not have a run inhibition lock
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FileAbsent)

	linkCtx := backend.LinkContext{
		FirstInstall:   true,
		RunInhibitHint: runinhibit.HintInhibitedForRefresh,
	}
	err = s.be.UnlinkSnap(info, linkCtx, nil)
	c.Assert(err, IsNil)

	// generated wrappers should be gone now
	for _, entry := range generatedSnapdUnits {
		c.Check(toEtcUnitPath(entry[0]), testutil.FileAbsent)
	}
	// unlinked snaps have a run inhibition lock
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FilePresent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.refresh"), testutil.FilePresent)

	// unlink is idempotent
	err = s.be.UnlinkSnap(info, linkCtx, nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FilePresent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.refresh"), testutil.FilePresent)
}

func (s *snapdOnCoreUnlinkSuite) TestUnlinkNonFirstSnapdOnCoreDoesNothing(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapUserServicesDir, 0755)
	c.Assert(err, IsNil)

	info, _ := mockSnapdSnapForLink(c)

	units := [][]string{
		{filepath.Join(dirs.SnapServicesDir, "snapd.service"), "precious"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.socket"), "precious"},
		{filepath.Join(dirs.SnapServicesDir, "snapd.snap-repair.timer"), "precious"},
		{filepath.Join(dirs.SnapServicesDir, "usr-lib-snapd.mount"), "precious"},
		{filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agent.service"), "precious"},
		{filepath.Join(dirs.SnapUserServicesDir, "snapd.session-agentsocket"), "precious"},
	}
	// content list uses absolute paths already
	snaptest.PopulateDir("/", units)
	linkCtx := backend.LinkContext{
		FirstInstall:   false,
		RunInhibitHint: runinhibit.HintInhibitedForRefresh,
	}
	err = s.be.UnlinkSnap(info, linkCtx, nil)
	c.Assert(err, IsNil)
	for _, unit := range units {
		c.Check(unit[0], testutil.FileEquals, "precious")
	}

	// unlinked snaps have a run inhibition lock. XXX: the specific inhibition hint can change.
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FilePresent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "snapd.lock"), testutil.FileEquals, "refresh")
	// check inhibit info file content
	inhibitInfoPath := filepath.Join(runinhibit.InhibitDir, "snapd.refresh")
	var inhibitInfo runinhibit.InhibitInfo
	buf, err := os.ReadFile(inhibitInfoPath)
	c.Assert(err, IsNil)
	c.Assert(json.Unmarshal(buf, &inhibitInfo), IsNil)
	c.Check(inhibitInfo, Equals, runinhibit.InhibitInfo{Previous: snap.R(11)})
}

func (s *linkSuite) TestLinkOptRequiresTooling(c *C) {
	const yaml = `name: hello
version: 1.0

apps:
 svc:
   command: svc
   daemon: simple
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	linkCtxWithTooling := backend.LinkContext{
		RequireMountedSnapdSnap: true,
	}
	_, err := s.be.LinkSnap(info, mockDev, linkCtxWithTooling, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.hello.svc.service"), testutil.FileContains,
		`Wants=usr-lib-snapd.mount
After=usr-lib-snapd.mount`)

	// remove it now
	err = s.be.UnlinkSnap(info, linkCtxWithTooling, nil)
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.hello.svc.service"), testutil.FileAbsent)

	linkCtxNoTooling := backend.LinkContext{
		RequireMountedSnapdSnap: false,
	}
	_, err = s.be.LinkSnap(info, mockDev, linkCtxNoTooling, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.hello.svc.service"), Not(testutil.FileContains), `usr-lib-snapd.mount`)
}

func (s *linkSuite) TestLinkOptHasQuotaGroup(c *C) {
	const yaml = `name: hello
version: 1.0

apps:
 svc:
   command: svc
   daemon: simple
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	grp, err := quota.NewGroup("foogroup", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build())
	c.Assert(err, IsNil)

	linkCtxWithGroup := backend.LinkContext{
		ServiceOptions: &wrappers.SnapServiceOptions{
			QuotaGroup: grp,
		},
	}
	_, err = s.be.LinkSnap(info, mockDev, linkCtxWithGroup, s.perfTimings)
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(dirs.SnapServicesDir, "snap.hello.svc.service"), testutil.FileContains,
		"\nSlice=snap.foogroup.slice\n")
}

type OverridenSnapdRestart struct {
	callback func() error
}

func (r *OverridenSnapdRestart) Restart() error {
	return r.callback()
}

func (s *linkSuite) TestLinkSnapdSnapSetSymlinks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	const yaml = `name: snapd
version: 1.0
type: snapd
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})
	mountDir := info.MountDir()
	dataDir := info.DataDir()
	currentActiveSymlink := filepath.Join(filepath.Dir(mountDir), "current")
	currentDataSymlink := filepath.Join(filepath.Dir(dataDir), "current")
	err := os.Symlink("oldactivevalue", currentActiveSymlink)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Dir(dataDir), os.ModePerm)
	c.Assert(err, IsNil)
	err = os.Symlink("olddatavalue", currentDataSymlink)
	c.Assert(err, IsNil)

	var restartDone bool
	restartFunc := func() error {
		restartDone = true
		mountTarget, err := os.Readlink(currentDataSymlink)
		c.Assert(err, IsNil)
		dataTarget, err := os.Readlink(currentDataSymlink)
		c.Assert(err, IsNil)
		c.Check(mountTarget, Equals, filepath.Base(mountDir))
		c.Check(dataTarget, Equals, filepath.Base(dataDir))
		return fmt.Errorf("BROKEN")
	}
	restoreAddSnapdSnapWrappers := backend.MockWrappersAddSnapdSnapServices(func(s *snap.Info, opts *wrappers.AddSnapdSnapServicesOptions, inter wrappers.Interacter) (wrappers.SnapdRestart, error) {
		return &OverridenSnapdRestart{callback: restartFunc}, nil
	})
	defer restoreAddSnapdSnapWrappers()

	be := backend.NewForPreseedMode()
	coreDev := boottest.MockUC20Device("run", nil)

	_, err = be.LinkSnap(info, coreDev, backend.LinkContext{}, s.perfTimings)
	c.Assert(err, ErrorMatches, `BROKEN`)
	c.Assert(restartDone, Equals, true)

	readMountTarget, err := os.Readlink(currentActiveSymlink)
	c.Assert(err, IsNil)
	readDataTarget, err := os.Readlink(currentDataSymlink)
	c.Assert(err, IsNil)

	c.Check(readMountTarget, Equals, "oldactivevalue")
	c.Check(readDataTarget, Equals, "olddatavalue")
}
