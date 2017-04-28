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
	be           backend.Backend
	nullProgress progress.NullProgress
	prevctlCmd   func(...string) ([]byte, error)
}

var _ = Suite(&linkSuite{})

func (s *linkSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.prevctlCmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
}

func (s *linkSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	systemd.SystemctlCmd = s.prevctlCmd
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
	const contents = ""

	info := snaptest.MockSnap(c, yaml, contents, &snap.SideInfo{Revision: snap.R(11)})

	err := s.be.LinkSnap(info)
	c.Assert(err, IsNil)

	l, err := filepath.Glob(filepath.Join(dirs.SnapBinariesDir, "*"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)
	l, err = filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.service"))
	c.Assert(err, IsNil)
	c.Assert(l, HasLen, 1)

	// undo will remove
	err = s.be.UnlinkSnap(info, &s.nullProgress)
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

	info := snaptest.MockSnap(c, yaml, contents, &snap.SideInfo{Revision: snap.R(11)})

	err := s.be.LinkSnap(info)
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
	err = s.be.UnlinkSnap(info, &s.nullProgress)
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

	info := snaptest.MockSnap(c, yaml, contents, &snap.SideInfo{Revision: snap.R(11)})

	err := s.be.LinkSnap(info)
	c.Assert(err, IsNil)

	err = s.be.LinkSnap(info)
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

	info := snaptest.MockSnap(c, yaml, contents, &snap.SideInfo{Revision: snap.R(11)})

	err := s.be.LinkSnap(info)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, &s.nullProgress)
	c.Assert(err, IsNil)

	err = s.be.UnlinkSnap(info, &s.nullProgress)
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
	err := s.be.LinkSnap(info)
	c.Assert(err, ErrorMatches, `cannot link snap "foo" with unset revision`)
}
