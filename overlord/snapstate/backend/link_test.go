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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

type linkSuite struct {
	be backend.Backend

	systemctlRestorer func()
}

var _ = Suite(&linkSuite{})

func (s *linkSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})
}

func (s *linkSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.systemctlRestorer()
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

	err := s.be.LinkSnap(info, nil)
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

	err := s.be.LinkSnap(info, nil)
	c.Assert(err, IsNil)

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

	err := s.be.LinkSnap(info, nil)
	c.Assert(err, IsNil)

	err = s.be.LinkSnap(info, nil)
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

	err := s.be.LinkSnap(info, nil)
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
	err := s.be.LinkSnap(info, nil)
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

	err := s.be.LinkSnap(s.info, nil)
	c.Assert(err, NotNil)
	c.Assert(err, FitsTypeOf, &os.PathError{})

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

func (s *linkCleanupSuite) TestLinkCleanupOnSystemctlFail(c *C) {
	r := systemd.MockSystemctl(func(...string) ([]byte, error) {
		return nil, errors.New("ouchie")
	})
	defer r()

	err := s.be.LinkSnap(s.info, nil)
	c.Assert(err, ErrorMatches, "ouchie")

	for _, d := range []string{dirs.SnapBinariesDir, dirs.SnapDesktopFilesDir, dirs.SnapServicesDir} {
		l, err := filepath.Glob(filepath.Join(d, "*"))
		c.Check(err, IsNil, Commentf(d))
		c.Check(l, HasLen, 0, Commentf(d))
	}

}
