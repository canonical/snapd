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
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

type linkSuite struct {
	be backend.Backend

	systemctlRestorer func()

	perfTimings *timings.Timings
}

var _ = Suite(&linkSuite{})

func (s *linkSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.perfTimings = timings.New(nil)
	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
}

func (s *linkSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.systemctlRestorer()
}

func (s *linkSuite) TestLinkSnapGivesLastActiveDisabledServicesToWrappers(c *C) {
	const yaml = `name: hello
version: 1.0
environment:
 KEY: value

apps:
 bin:
   command: bin
   daemon: simple
 svc:
   command: svc
   daemon: simple
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	svcsDisabled := []string{}
	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// drop --root from the cmd
		if len(cmd) >= 3 && cmd[0] == "--root" {
			cmd = cmd[2:]
		}
		// if it's an enable, save the service name to check later
		if len(cmd) >= 2 && cmd[0] == "enable" {
			svcsDisabled = append(svcsDisabled, cmd[1])
		}
		return nil, nil
	})
	defer r()

	_, err := s.be.LinkSnap(info, mockDev, []string{"svc"}, s.perfTimings)
	c.Assert(err, IsNil)

	c.Assert(svcsDisabled, DeepEquals, []string{"snap.hello.bin.service"})
}

func (s *linkSuite) TestLinkDoUndoGenerateWrappers(c *C) {
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

	_, err := s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	// undo will remove
	err = s.be.UnlinkSnap(info, progress.Null)
	c.Assert(err, IsNil)

	l, err = filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 0)
}

func (s *linkSuite) TestLinkDoUndoCurrentSymlink(c *C) {
	const yaml = `name: hello
version: 1.0
`
	const contents = ""

	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	reboot, err := s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(reboot, Equals, false)

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
	err = s.be.UnlinkSnap(info, progress.Null)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(currentActiveSymlink), Equals, false)
	c.Check(osutil.FileExists(currentDataSymlink), Equals, false)

}

func (s *linkSuite) TestLinkSetNextBoot(c *C) {
	coreDev := boottest.MockDevice("base")

	bl := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bl)
	defer bootloader.Force(nil)
	bl.SetBootBase("base_1.snap")

	const yaml = `name: base
version: 1.0
type: base
`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	reboot, err := s.be.LinkSnap(info, coreDev, nil, s.perfTimings)
	c.Assert(err, IsNil)
	c.Check(reboot, Equals, true)
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
	const contents = ""

	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
	c.Assert(err, IsNil)

	_, err = s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
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
	const contents = ""

	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	_, err := s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, progress.Null)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, progress.Null)
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
}

func (s *linkSuite) TestLinkFailsForUnsetRevision(c *C) {
	info := &snap.Info{
		SuggestedName: "foo",
	}
	_, err := s.be.LinkSnap(info, mockDev, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, `cannot link snap "foo" with unset revision`)
}

type linkCleanupSuite struct {
	linkSuite
	info *snap.Info
}

var _ = Suite(&linkCleanupSuite{})

func (s *linkCleanupSuite) SetUpTest(c *C) {
	s.linkSuite.SetUpTest(c)

	const yaml = `name: hello
version: 1.0
environment:
 KEY: value

apps:
 foo:
   command: foo
 bar:
   command: bar
 svc:
   command: svc
   daemon: simple
`
	s.info = snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})

	guiDir := filepath.Join(s.info.MountDir(), "meta", "gui")
	c.Assert(os.MkdirAll(guiDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(guiDir, "bin.desktop"), []byte(`
[Desktop Entry]
Name=bin
Icon=${SNAP}/bin.png
Exec=bin
`), 0644), IsNil)

	r := systemd.MockSystemctl(func(...string) ([]byte, error) {
		return nil, nil
	})
	defer r()

	// sanity checks
	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir} {
		os.MkdirAll(d, 0755)
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Assert(err, IsNil, Commentf(d))
		c.Assert(l, HasLen, 0, Commentf(d))
	}
}

func (s *linkCleanupSuite) testLinkCleanupDirOnFail(c *C, dir string) {
	c.Assert(os.Chmod(dir, 0), IsNil)
	defer os.Chmod(dir, 0755)

	_, err := s.be.LinkSnap(s.info, mockDev, nil, s.perfTimings)
	c.Assert(err, NotNil)
	_, isPathError := err.(*os.PathError)
	_, isLinkError := err.(*os.LinkError)
	c.Assert(isPathError || isLinkError, Equals, true, Commentf("%T", err))

	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir} {
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

func (s *linkCleanupSuite) TestLinkCleanupOnSystemctlFail(c *C) {
	r := systemd.MockSystemctl(func(...string) ([]byte, error) {
		return nil, errors.New("ouchie")
	})
	defer r()

	_, err := s.be.LinkSnap(s.info, mockDev, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, "ouchie")

	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir} {
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Check(err, IsNil, Commentf(d))
		c.Check(l, HasLen, 0, Commentf(d))
	}
}

func (s *linkCleanupSuite) TestLinkCleansUpDataDirAndSymlinksOnSymlinkFail(c *C) {
	// sanity check
	c.Assert(s.info.DataDir(), testutil.FileAbsent)

	// the mountdir symlink is currently the last thing in
	// LinkSnap that can make it fail
	d := filepath.Dir(s.info.MountDir())
	c.Assert(os.Chmod(d, 0), IsNil)
	defer os.Chmod(d, 0755)

	_, err := s.be.LinkSnap(s.info, mockDev, nil, s.perfTimings)
	c.Assert(err, ErrorMatches, `(?i).*symlink.*permission denied.*`)

	c.Check(s.info.DataDir(), testutil.FileAbsent)
	c.Check(filepath.Join(s.info.DataDir(), "..", "current"), testutil.FileAbsent)
	c.Check(filepath.Join(s.info.MountDir(), "..", "current"), testutil.FileAbsent)
}

func (s *linkCleanupSuite) TestLinkRunsUpdateFontconfigCachesClassic(c *C) {
	current := filepath.Join(s.info.MountDir(), "..", "current")

	for _, dev := range []boot.Device{mockDev, mockClassicDev} {
		var updateFontconfigCaches int
		restore := backend.MockUpdateFontconfigCaches(func() error {
			c.Assert(osutil.FileExists(current), Equals, false)
			updateFontconfigCaches += 1
			return nil
		})
		defer restore()

		_, err := s.be.LinkSnap(s.info, dev, nil, s.perfTimings)
		c.Assert(err, IsNil)
		if dev.Classic() {
			c.Assert(updateFontconfigCaches, Equals, 1)
		} else {
			c.Assert(updateFontconfigCaches, Equals, 0)
		}
		c.Assert(os.Remove(current), IsNil)
	}
}

func (s *linkCleanupSuite) TestLinkRunsUpdateFontconfigCachesCallsFromNewCurrent(c *C) {
	const yaml = `name: core
version: 1.0
type: os
`
	// old version is 'current'
	infoOld := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(11)})
	mountDirOld := infoOld.MountDir()
	err := os.Symlink(filepath.Base(mountDirOld), filepath.Join(mountDirOld, "..", "current"))
	c.Assert(err, IsNil)

	oldCmdV6 := testutil.MockCommand(c, filepath.Join(mountDirOld, "bin", "fc-cache-v6"), "")
	oldCmdV7 := testutil.MockCommand(c, filepath.Join(mountDirOld, "bin", "fc-cache-v7"), "")

	infoNew := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(12)})
	mountDirNew := infoNew.MountDir()

	newCmdV6 := testutil.MockCommand(c, filepath.Join(mountDirNew, "bin", "fc-cache-v6"), "")
	newCmdV7 := testutil.MockCommand(c, filepath.Join(mountDirNew, "bin", "fc-cache-v7"), "")

	// provide our own mock, osutil.CommandFromCore expects an ELF binary
	restore := backend.MockCommandFromSystemSnap(func(name string, args ...string) (*exec.Cmd, error) {
		cmd := filepath.Join(dirs.SnapMountDir, "core", "current", name)
		c.Logf("command from core: %v", cmd)
		return exec.Command(cmd, args...), nil
	})
	defer restore()

	_, err = s.be.LinkSnap(infoNew, mockClassicDev, nil, s.perfTimings)
	c.Assert(err, IsNil)

	c.Check(oldCmdV6.Calls(), HasLen, 0)
	c.Check(oldCmdV7.Calls(), HasLen, 0)

	c.Check(newCmdV6.Calls(), HasLen, 1)
	c.Check(newCmdV7.Calls(), HasLen, 1)
}
