// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2022 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
	dirs.SetSnapHomeDirs("/home")
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user1", "snap")
	homeData := filepath.Join(homedir, "hello/10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	varData := filepath.Join(dirs.SnapDataDir, "hello/10")
	err = os.MkdirAll(varData, 0755)
	c.Assert(err, IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err = s.be.RemoveSnapData(info, nil)

	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(homeData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(homeData)), Equals, true)
	c.Assert(osutil.FileExists(varData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varData)), Equals, true)
}

// same as TestRemoveSnapData but with multiple homedirs
func (s *snapdataSuite) TestRemoveSnapDataMulti(c *C) {
	homeDirs := []string{filepath.Join(dirs.GlobalRootDir, "home"),
		filepath.Join(dirs.GlobalRootDir, "home", "company"),
		filepath.Join(dirs.GlobalRootDir, "home", "department"),
		filepath.Join(dirs.GlobalRootDir, "office")}

	dirs.SetSnapHomeDirs(strings.Join(homeDirs, ","))
	snapHomeDataDirs := []string{}

	for _, v := range homeDirs {
		snapHomeDir := filepath.Join(v, "user1", "snap")
		snapHomeData := filepath.Join(snapHomeDir, "hello/10")
		err := os.MkdirAll(snapHomeData, 0755)
		c.Assert(err, IsNil)
		snapHomeDataDirs = append(snapHomeDataDirs, snapHomeData)
	}

	varData := filepath.Join(dirs.SnapDataDir, "hello/10")
	err := os.MkdirAll(varData, 0755)
	c.Assert(err, IsNil)
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err = s.be.RemoveSnapData(info, nil)

	for _, v := range snapHomeDataDirs {
		c.Assert(err, IsNil)
		c.Assert(osutil.FileExists(v), Equals, false)
		c.Assert(osutil.FileExists(filepath.Dir(v)), Equals, true)

	}

	c.Assert(osutil.FileExists(varData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varData)), Equals, true)
}

func (s *snapdataSuite) TestSnapDataDirs(c *C) {
	homeDir1 := filepath.Join("home", "users")
	homeDir2 := filepath.Join("remote", "users")
	homeDirs := homeDir1 + "," + homeDir2
	dirs.SetSnapHomeDirs(homeDirs)
	dataHomeDirs := []string{filepath.Join(dirs.GlobalRootDir, homeDir1, "user1", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, homeDir1, "user2", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, homeDir2, "user3", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, homeDir2, "user4", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, "var", "snap", "hello", "10")}
	for _, path := range dataHomeDirs {
		err := os.MkdirAll(path, 0755)
		c.Assert(err, IsNil)
	}

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	snapDataDirs, err := backend.SnapDataDirs(info, nil)
	c.Assert(err, IsNil)
	c.Check(snapDataDirs, DeepEquals, dataHomeDirs)
}

func (s *snapdataSuite) TestSnapCommonDataDirs(c *C) {
	homeDir1 := filepath.Join(dirs.GlobalRootDir, "home", "users")
	homeDir2 := filepath.Join(dirs.GlobalRootDir, "remote", "users")
	homeDirs := homeDir1 + "," + homeDir2
	dirs.SetSnapHomeDirs(homeDirs)
	dataHomeDirs := []string{filepath.Join(homeDir1, "user1", "snap", "hello", "common"), filepath.Join(homeDir1, "user2", "snap", "hello", "common"),
		filepath.Join(homeDir2, "user3", "snap", "hello", "common"), filepath.Join(homeDir2, "user4", "snap", "hello", "common"),
		filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "common"), filepath.Join(dirs.GlobalRootDir, "var", "snap", "hello", "common")}
	for _, path := range dataHomeDirs {
		err := os.MkdirAll(path, 0755)
		c.Assert(err, IsNil)
	}

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	snapCommonDataDirs, err := backend.SnapCommonDataDirs(info, nil)
	c.Assert(err, IsNil)
	c.Check(snapCommonDataDirs, DeepEquals, dataHomeDirs)
}

func (s *snapdataSuite) TestRemoveSnapCommonData(c *C) {
	dirs.SetSnapHomeDirs("/home")
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user1", "snap")
	homeCommonData := filepath.Join(homedir, "hello/common")
	err := os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)
	varCommonData := filepath.Join(dirs.SnapDataDir, "hello/common")
	err = os.MkdirAll(varCommonData, 0755)
	c.Assert(err, IsNil)

	rootCommonDir := filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "common")
	c.Assert(os.MkdirAll(rootCommonDir, 0700), IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err = s.be.RemoveSnapCommonData(info, nil)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(homeCommonData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(homeCommonData)), Equals, true)
	c.Assert(osutil.FileExists(varCommonData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varCommonData)), Equals, true)
	c.Assert(osutil.FileExists(rootCommonDir), Equals, false)
}

func (s *snapdataSuite) TestRemoveSnapCommonSave(c *C) {
	varSaveData := snap.CommonDataSaveDir("hello")
	err := os.MkdirAll(varSaveData, 0755)
	c.Assert(err, IsNil)

	varCommonData := filepath.Join(dirs.SnapDataDir, "hello/common")
	err = os.MkdirAll(varCommonData, 0755)
	c.Assert(err, IsNil)

	rootCommonDir := filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "common")
	c.Assert(os.MkdirAll(rootCommonDir, 0700), IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err = s.be.RemoveSnapSaveData(info, mockDev)
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(varSaveData), Equals, false)
	c.Check(osutil.FileExists(filepath.Dir(varSaveData)), Equals, true)
	c.Check(osutil.FileExists(varCommonData), Equals, true)
	c.Check(osutil.FileExists(filepath.Dir(varCommonData)), Equals, true)
	c.Check(osutil.FileExists(rootCommonDir), Equals, true)
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
