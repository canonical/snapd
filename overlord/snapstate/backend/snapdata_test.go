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

package backend_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
)

type snapdataSuite struct {
	be      backend.Backend
	tempdir string
}

var _ = Suite(&snapdataSuite{})

func (s *snapdataSuite) SetUpTest(c *C) {
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *snapdataSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *snapdataSuite) TestRemoveSnapData(c *C) {
	homedir := filepath.Join(s.tempdir, "home", "user1", "snap")
	homeData := filepath.Join(homedir, "hello/10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	varData := filepath.Join(dirs.SnapDataDir, "hello/10")
	err = os.MkdirAll(varData, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err = s.be.RemoveSnapData(info)

	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(homeData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(homeData)), Equals, true)
	c.Assert(osutil.FileExists(varData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varData)), Equals, true)
}

func (s *snapdataSuite) TestRemoveSnapCommonData(c *C) {
	homedir := filepath.Join(s.tempdir, "home", "user1", "snap")
	homeCommonData := filepath.Join(homedir, "hello/common")
	err := os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)
	varCommonData := filepath.Join(dirs.SnapDataDir, "hello/common")
	err = os.MkdirAll(varCommonData, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err = s.be.RemoveSnapCommonData(info)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(homeCommonData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(homeCommonData)), Equals, true)
	c.Assert(osutil.FileExists(varCommonData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varCommonData)), Equals, true)
}

func (s *snapdataSuite) TestRemoveSnapDataDir(c *C) {
	varBaseData := filepath.Join(dirs.SnapDataDir, "hello")
	err := os.MkdirAll(varBaseData, 0755)
	c.Assert(err, IsNil)
	varBaseDataInstance := filepath.Join(dirs.SnapDataDir, "hello_instance")
	err = os.MkdirAll(varBaseDataInstance, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err = s.be.RemoveSnapDataDir(info, true)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(varBaseData), Equals, true)
	c.Assert(osutil.FileExists(varBaseDataInstance), Equals, true)

	// now with instance key
	info.InstanceKey = "instance"
	err = s.be.RemoveSnapDataDir(info, true)
	c.Assert(err, IsNil)
	// instance directory is gone
	c.Assert(osutil.FileExists(varBaseDataInstance), Equals, false)
	// but the snap-name one is still around
	c.Assert(osutil.FileExists(varBaseData), Equals, true)

	// back to no instance key
	info.InstanceKey = ""
	err = s.be.RemoveSnapDataDir(info, false)
	c.Assert(err, IsNil)
	// the snap-name directory is gone now too
	c.Assert(osutil.FileExists(varBaseData), Equals, false)
}
