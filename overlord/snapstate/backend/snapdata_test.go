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
	homedir := filepath.Join(s.tempdir, "home", "user1", "snap")
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

func (s *snapdataSuite) TestRemoveSnapCommonData(c *C) {
	homedir := filepath.Join(s.tempdir, "home", "user1", "snap")
	homeCommonData := filepath.Join(homedir, "hello/common")
	err := os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)
	varCommonData := filepath.Join(dirs.SnapDataDir, "hello/common")
	err = os.MkdirAll(varCommonData, 0755)
	c.Assert(err, IsNil)

	rootCommonDir := filepath.Join(s.tempdir, "root", "snap", "hello", "common")
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

	rootCommonDir := filepath.Join(s.tempdir, "root", "snap", "hello", "common")
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

func mockSnapDir(baseDir string) error {
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		return err
	}
	dataCurrentSymlink := filepath.Join(baseDir, "current")
	err = os.Symlink("10", dataCurrentSymlink)
	if err != nil {
		return err
	}
	return nil
}

func (s *snapdataSuite) testRemoveSnapDataDir(c *C, opts *dirs.SnapDirOptions) {
	// create system data dirs
	dataDir := filepath.Join(dirs.SnapDataDir, "hello")
	c.Assert(mockSnapDir(dataDir), IsNil)
	instanceDataDir := filepath.Join(dirs.SnapDataDir, "hello_instance")
	c.Assert(mockSnapDir(instanceDataDir), IsNil)

	snapHomeDir := "snap"
	if opts.HiddenSnapDataDir {
		snapHomeDir = ".snap/data"
	}

	// create user home data dirs
	homeDataDir := filepath.Join(s.tempdir, "home", "user1", snapHomeDir, "hello")
	c.Assert(mockSnapDir(homeDataDir), IsNil)
	instanceHomeDataDir := filepath.Join(s.tempdir, "home", "user1", snapHomeDir, "hello_instance")
	c.Assert(mockSnapDir(instanceHomeDataDir), IsNil)

	// create root home data dirs
	rootDataDir := filepath.Join(s.tempdir, "root", snapHomeDir, "hello")
	c.Assert(mockSnapDir(rootDataDir), IsNil)
	instanceRootDataDir := filepath.Join(s.tempdir, "root", snapHomeDir, "hello_instance")
	c.Assert(mockSnapDir(instanceRootDataDir), IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err := s.be.RemoveSnapDataDir(info, true, opts)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(dataDir), Equals, true)
	c.Assert(osutil.FileExists(instanceDataDir), Equals, true)
	c.Assert(osutil.FileExists(homeDataDir), Equals, true)
	c.Assert(osutil.FileExists(instanceHomeDataDir), Equals, true)
	c.Assert(osutil.FileExists(rootDataDir), Equals, true)
	c.Assert(osutil.FileExists(instanceRootDataDir), Equals, true)

	// now with instance key
	info.InstanceKey = "instance"
	err = s.be.RemoveSnapDataDir(info, true, opts)
	c.Assert(err, IsNil)
	// instance directories are gone
	c.Assert(osutil.FileExists(instanceDataDir), Equals, false)
	c.Assert(osutil.FileExists(instanceHomeDataDir), Equals, false)
	c.Assert(osutil.FileExists(instanceRootDataDir), Equals, false)
	// but the snap-name ones are still around
	c.Assert(osutil.FileExists(dataDir), Equals, true)
	c.Assert(osutil.FileExists(homeDataDir), Equals, true)
	c.Assert(osutil.FileExists(rootDataDir), Equals, true)

	// back to no instance key
	info.InstanceKey = ""
	err = s.be.RemoveSnapDataDir(info, false, opts)
	c.Assert(err, IsNil)
	// the snap-name directories are gone now too
	c.Assert(osutil.FileExists(dataDir), Equals, false)
	c.Assert(osutil.FileExists(homeDataDir), Equals, false)
	c.Assert(osutil.FileExists(rootDataDir), Equals, false)
}

func (s *snapdataSuite) TestRemoveSnapDataDir(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: false}
	s.testRemoveSnapDataDir(c, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirWithHiddenDataDir(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	s.testRemoveSnapDataDir(c, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirBadDir(c *C) {
	// we should try to cleanup as much as we can here (even if there is an error)
	// avoid one bad dir breaking the cleanup of the others

	// create system data dirs
	instanceDataDir := filepath.Join(dirs.SnapDataDir, "hello_instance")
	c.Assert(mockSnapDir(instanceDataDir), IsNil)

	// create root home data dirs
	instanceRootDataDir := filepath.Join(s.tempdir, "root", "snap", "hello_instance")
	c.Assert(mockSnapDir(instanceRootDataDir), IsNil)

	// create user home data dirs
	instanceHomeDataDir := filepath.Join(s.tempdir, "home", "user1", "snap", "hello_instance")
	c.Assert(mockSnapDir(instanceHomeDataDir), IsNil)
	// make dir read-only
	os.Chmod(instanceHomeDataDir, 0555)
	defer func() {
		// fix dir permissions to cleanup test
		os.Chmod(instanceHomeDataDir, 0755)
	}()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	info.InstanceKey = "instance"
	err := s.be.RemoveSnapDataDir(info, true, &dirs.SnapDirOptions{})
	// home directory removal fails
	c.Assert(err, ErrorMatches, `failed to remove snap "hello_instance" base directory:.*permission denied`)
	c.Assert(osutil.FileExists(instanceHomeDataDir), Equals, true)
	// other dirs are removed
	c.Assert(osutil.FileExists(instanceDataDir), Equals, false)
	c.Assert(osutil.FileExists(instanceRootDataDir), Equals, false)
}
