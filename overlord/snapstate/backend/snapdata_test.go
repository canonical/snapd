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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/user"
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
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user1")
	usr := &user.User{Uid: "1000", HomeDir: homedir}
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	homeData := filepath.Join(homedir, "snap", "hello", "10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)
	varData := filepath.Join(dirs.SnapDataDir, "hello", "10")
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

// same as TestRemoveSnapData but with multiple users in different homedirs
func (s *snapdataSuite) TestRemoveSnapDataMulti(c *C) {
	userHomes := []string{
		filepath.Join(dirs.GlobalRootDir, "home", "user1"),
		filepath.Join(dirs.GlobalRootDir, "home", "company", "user2"),
		filepath.Join(dirs.GlobalRootDir, "home", "department", "user3"),
		filepath.Join(dirs.GlobalRootDir, "office", "user4"),
	}
	var users []*user.User
	for i, home := range userHomes {
		users = append(users, &user.User{
			Uid:     fmt.Sprintf("%d", 1000+i),
			HomeDir: home,
		})
	}
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return users, nil
	})
	defer restore()

	snapHomeDataDirs := make([]string, 0, len(userHomes))
	for _, home := range userHomes {
		snapHomeData := filepath.Join(home, "snap", "hello", "10")
		err := os.MkdirAll(snapHomeData, 0755)
		c.Assert(err, IsNil)
		snapHomeDataDirs = append(snapHomeDataDirs, snapHomeData)
	}

	varData := filepath.Join(dirs.SnapDataDir, "hello", "10")
	err := os.MkdirAll(varData, 0755)
	c.Assert(err, IsNil)
	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err = s.be.RemoveSnapData(info, nil)
	c.Assert(err, IsNil)

	for _, v := range snapHomeDataDirs {
		c.Assert(osutil.FileExists(v), Equals, false)
		c.Assert(osutil.FileExists(filepath.Dir(v)), Equals, true)
	}
	c.Assert(osutil.FileExists(varData), Equals, false)
	c.Assert(osutil.FileExists(filepath.Dir(varData)), Equals, true)
}

func (s *snapdataSuite) TestSnapDataDirs(c *C) {
	homeDir1 := filepath.Join(dirs.GlobalRootDir, "home", "users")
	homeDir2 := filepath.Join(dirs.GlobalRootDir, "remote", "users")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "0", HomeDir: filepath.Join(dirs.GlobalRootDir, "/root")},
			{Uid: "1001", HomeDir: filepath.Join(homeDir1, "user1")},
			{Uid: "1002", HomeDir: filepath.Join(homeDir1, "user2")},
			{Uid: "1003", HomeDir: filepath.Join(homeDir2, "user3")},
			{Uid: "1004", HomeDir: filepath.Join(homeDir2, "user4")},
		}, nil
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	result, err := backend.SnapDataDirs(info, nil)
	c.Assert(err, IsNil)
	c.Check(result, DeepEquals, []string{
		filepath.Join(homeDir1, "user1", "snap", "hello", "10"),
		filepath.Join(homeDir1, "user2", "snap", "hello", "10"),
		filepath.Join(homeDir2, "user3", "snap", "hello", "10"),
		filepath.Join(homeDir2, "user4", "snap", "hello", "10"),
		filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "10"),
		filepath.Join(dirs.SnapDataDir, "hello", "10"),
	})
}

func (s *snapdataSuite) TestSnapCommonDataDirs(c *C) {
	homeDir1 := filepath.Join(dirs.GlobalRootDir, "home", "users")
	homeDir2 := filepath.Join(dirs.GlobalRootDir, "remote", "users")

	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "0", Gid: "0", HomeDir: filepath.Join(dirs.GlobalRootDir, "/root")},
			{Uid: "1001", Gid: "1001", HomeDir: filepath.Join(homeDir1, "user1")},
			{Uid: "1002", Gid: "1002", HomeDir: filepath.Join(homeDir2, "user2")},
		}, nil
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	result, err := backend.SnapCommonDataDirs(info, nil)
	c.Assert(err, IsNil)
	// order: home common dirs, root common dir, XDG runtime dirs (all users incl. root), system common dir
	c.Check(result, DeepEquals, []string{
		filepath.Join(homeDir1, "user1", "snap", "hello", "common"),
		filepath.Join(homeDir2, "user2", "snap", "hello", "common"),
		filepath.Join(dirs.GlobalRootDir, "root", "snap", "hello", "common"),
		filepath.Join(dirs.XdgRuntimeDirBase, "0", "snap.hello"),
		filepath.Join(dirs.XdgRuntimeDirBase, "1001", "snap.hello"),
		filepath.Join(dirs.XdgRuntimeDirBase, "1002", "snap.hello"),
		filepath.Join(dirs.SnapDataDir, "hello", "common"),
	})
}

func (s *snapdataSuite) TestRemoveSnapCommonData(c *C) {
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "user1")
	usr := &user.User{Uid: "1000", Gid: "1000", HomeDir: homedir}
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	homeCommonData := filepath.Join(homedir, "snap", "hello", "common")
	err := os.MkdirAll(homeCommonData, 0755)
	c.Assert(err, IsNil)
	varCommonData := filepath.Join(dirs.SnapDataDir, "hello", "common")
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

func (s *snapdataSuite) testRemoveSnapDataDir(c *C, hasOtherInstances bool, opts *dirs.SnapDirOptions) {
	var baseDataDirs, baseDataInstanceDirs []string

	// system data dirs
	baseDataDirs = append(baseDataDirs, filepath.Join(dirs.SnapDataDir, "hello"))
	baseDataInstanceDirs = append(baseDataInstanceDirs, filepath.Join(dirs.SnapDataDir, "hello_instance"))

	snapHomeDir := "snap"
	if opts.HiddenSnapDataDir {
		snapHomeDir = ".snap/data"
	}

	// root + user home data dirs
	homeDir1 := filepath.Join(dirs.GlobalRootDir, "home", "users")
	homeDir2 := filepath.Join(dirs.GlobalRootDir, "remote", "users")
	userHomes := []string{
		filepath.Join(homeDir1, "user1"),
		filepath.Join(homeDir1, "user2"),
		filepath.Join(homeDir2, "user3"),
		filepath.Join(homeDir2, "user4"),
	}
	rootHome := filepath.Join(dirs.GlobalRootDir, "root")
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		users := []*user.User{
			{Uid: "0", HomeDir: rootHome},
		}
		for i, home := range userHomes {
			users = append(users, &user.User{
				Uid:     fmt.Sprintf("%d", 1001+i),
				HomeDir: home,
			})
		}
		return users, nil
	})
	defer restore()

	for _, home := range append(userHomes, rootHome) {
		baseDataDirs = append(baseDataDirs, filepath.Join(home, snapHomeDir, "hello"))
		baseDataInstanceDirs = append(baseDataInstanceDirs, filepath.Join(home, snapHomeDir, "hello_instance"))
	}

	// create all base directories
	for _, dir := range append(baseDataDirs, baseDataInstanceDirs...) {
		c.Assert(os.MkdirAll(dir, 0755), IsNil)
		// populate home data dirs with broken symlink to simulate https://bugs.launchpad.net/snapd/+bug/2009617
		if strings.Contains(dir, dirs.SnapDataDir) {
			// skip system data dirs
			continue
		}
		dataCurrentSymlink := filepath.Join(dir, "current")
		c.Assert(os.Symlink("10", dataCurrentSymlink), IsNil)
	}

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})

	err := s.be.RemoveSnapDataDir(info, true, opts)
	c.Assert(err, IsNil)
	for _, dir := range baseDataInstanceDirs {
		c.Assert(osutil.FileExists(dir), Equals, true)
	}
	for _, dir := range baseDataDirs {
		c.Assert(osutil.FileExists(dir), Equals, true)
	}

	// now with instance key
	info.InstanceKey = "instance"
	err = s.be.RemoveSnapDataDir(info, hasOtherInstances, opts)
	c.Assert(err, IsNil)
	// instance directories are gone
	for _, dir := range baseDataInstanceDirs {
		c.Assert(osutil.FileExists(dir), Equals, false)
	}
	// but the snap-name one is still around if there are other instances
	for _, dir := range baseDataDirs {
		c.Assert(osutil.FileExists(dir), Equals, hasOtherInstances)
	}

	if hasOtherInstances {
		// back to no instance key
		info.InstanceKey = ""
		err = s.be.RemoveSnapDataDir(info, false, opts)
		c.Assert(err, IsNil)
		// the snap-name directory is gone now too
		for _, dir := range baseDataDirs {
			c.Assert(osutil.FileExists(dir), Equals, false)
		}
	}
}

func (s *snapdataSuite) TestRemoveSnapDataDir(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: false}
	hasOtherInstances := true
	s.testRemoveSnapDataDir(c, hasOtherInstances, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirWithHiddenDataDir(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	hasOtherInstances := true
	s.testRemoveSnapDataDir(c, hasOtherInstances, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirNoOtherInstances(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: false}
	hasOtherInstances := false
	s.testRemoveSnapDataDir(c, hasOtherInstances, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirWithHiddenDataDirNoOtherInstances(c *C) {
	opts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	hasOtherInstances := false
	s.testRemoveSnapDataDir(c, hasOtherInstances, opts)
}

func (s *snapdataSuite) TestRemoveSnapDataDirWithUnexpectedFiles(c *C) {
	homedir := filepath.Join(dirs.GlobalRootDir, "home", "users")
	usr := &user.User{Uid: "1000", HomeDir: homedir}
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{usr}, nil
	})
	defer restore()

	baseDataDir := filepath.Join(homedir, "snap", "hello")
	c.Assert(os.MkdirAll(baseDataDir, 0755), IsNil)
	// expected current symlink
	dataCurrentSymlink := filepath.Join(baseDataDir, "current")
	c.Assert(os.Symlink("10", dataCurrentSymlink), IsNil)
	// unexpected folder
	c.Assert(os.Mkdir(filepath.Join(baseDataDir, "unexpected"), 0755), IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapDataDir(info, false, nil)
	c.Assert(err, ErrorMatches, `failed to remove snap "hello" base directory: remove .*/home/users/snap/hello: directory not empty\ndir contents: \[unexpected\]`)
}

func (s *snapdataSuite) TestRemoveSnapDataDirEnotemptyWithReadDirError(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "home", "users")
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "1000", HomeDir: homeDir},
		}, nil
	})
	defer restore()

	baseDataDir := filepath.Join(homeDir, "snap", "hello")
	c.Assert(os.MkdirAll(baseDataDir, 0755), IsNil)
	// unexpected folder to make removal fail with ENOTEMPTY
	c.Assert(os.Mkdir(filepath.Join(baseDataDir, "unexpected"), 0755), IsNil)
	// make the dir non-readable so Readdirnames fails; dir contents should not
	// be appended to the error message in this case
	c.Assert(os.Chmod(baseDataDir, 0111), IsNil)
	defer os.Chmod(baseDataDir, 0755)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapDataDir(info, false, nil)
	c.Assert(err, ErrorMatches, `failed to remove snap "hello" base directory: remove .*/home/users/snap/hello: directory not empty`)
}

func (s *snapdataSuite) TestRemoveSnapDataDirErrorNotEnotempty(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "home", "users")
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "1000", HomeDir: homeDir},
		}, nil
	})
	defer restore()

	parentDir := filepath.Join(homeDir, "snap")
	baseDataDir := filepath.Join(parentDir, "hello")
	c.Assert(os.MkdirAll(baseDataDir, 0755), IsNil)
	// make parent non-writable so removal fails with EACCES, not ENOTEMPTY;
	// dir contents should not be appended to the error message in this case
	c.Assert(os.Chmod(parentDir, 0555), IsNil)
	defer os.Chmod(parentDir, 0755)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapDataDir(info, false, nil)
	c.Assert(err, ErrorMatches, `failed to remove snap "hello" base directory: remove .*/home/users/snap/hello: permission denied`)
}

func (s *snapdataSuite) TestRemoveSnapDataDirCurrentSymlinkRemovalFails(c *C) {
	homeDir := filepath.Join(dirs.GlobalRootDir, "home", "users")
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "1000", HomeDir: homeDir},
		}, nil
	})
	defer restore()

	baseDataDir := filepath.Join(homeDir, "snap", "hello")
	c.Assert(os.MkdirAll(baseDataDir, 0755), IsNil)
	// create the current symlink
	c.Assert(os.Symlink("10", filepath.Join(baseDataDir, "current")), IsNil)
	// make the dir non-writable so unlinking current fails with EACCES;
	// dir contents should not be appended since the error is not ENOTEMPTY
	c.Assert(os.Chmod(baseDataDir, 0555), IsNil)
	defer os.Chmod(baseDataDir, 0755)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapDataDir(info, false, nil)
	c.Assert(err, ErrorMatches, `failed to remove snap "hello" base directory: remove .*/home/users/snap/hello/current: permission denied`)
}

// TestRemoveSnapDataSkipsNonUserDir verifies that directories that look like
// home dirs but don't belong to any real user are not removed.
func (s *snapdataSuite) TestRemoveSnapDataSkipsNonUserDir(c *C) {
	realHomeDir := filepath.Join(dirs.GlobalRootDir, "home", "real-user")
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "1000", HomeDir: realHomeDir},
		}, nil
	})
	defer restore()

	// Create data for the real user
	realData := filepath.Join(realHomeDir, "snap", "hello", "10")
	c.Assert(os.MkdirAll(realData, 0755), IsNil)

	// Create data under a directory that looks like a home dir but isn't a real user
	nonUserData := filepath.Join(dirs.GlobalRootDir, "home", "build-artifact", "snap", "hello", "10")
	c.Assert(os.MkdirAll(nonUserData, 0755), IsNil)

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapData(info, nil)
	c.Assert(err, IsNil)

	// Real user's data is removed
	c.Assert(osutil.FileExists(realData), Equals, false)
	// Non-user directory is NOT removed
	c.Assert(osutil.FileExists(nonUserData), Equals, true)
}

// TestSnapDataDirsAllUsersError verifies that errors from allUsers() are propagated.
func (s *snapdataSuite) TestSnapDataDirsAllUsersError(c *C) {
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return nil, fmt.Errorf("mock AllUsers error")
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	_, err := backend.SnapDataDirs(info, nil)
	c.Assert(err, ErrorMatches, "mock AllUsers error")
}

// TestSnapCommonDataDirsAllUsersError verifies that errors from allUsers() are propagated.
func (s *snapdataSuite) TestSnapCommonDataDirsAllUsersError(c *C) {
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return nil, fmt.Errorf("mock AllUsers error")
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	_, err := backend.SnapCommonDataDirs(info, nil)
	c.Assert(err, ErrorMatches, "mock AllUsers error")
}

// TestRemoveSnapDataDirAllUsersError verifies that errors from allUsers() are
// propagated when removing the snap base data directory.
func (s *snapdataSuite) TestRemoveSnapDataDirAllUsersError(c *C) {
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return nil, fmt.Errorf("mock AllUsers error")
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	err := s.be.RemoveSnapDataDir(info, false, nil)
	c.Assert(err, ErrorMatches, "mock AllUsers error")
}

// TestSnapCommonDataDirsInvalidUid verifies that a non-numeric UID returned by
// allUsers() causes an error when building the XDG runtime directory list.
func (s *snapdataSuite) TestSnapCommonDataDirsInvalidUid(c *C) {
	restore := backend.MockAllUsers(func(_ *dirs.SnapDirOptions) ([]*user.User, error) {
		return []*user.User{
			{Uid: "not-a-uid", Gid: "0", HomeDir: filepath.Join(dirs.GlobalRootDir, "home", "user1")},
		}, nil
	})
	defer restore()

	info := snaptest.MockSnap(c, helloYaml1, &snap.SideInfo{Revision: snap.R(10)})
	_, err := backend.SnapCommonDataDirs(info, nil)
	c.Assert(err, ErrorMatches, `cannot parse user id not-a-uid: .*`)
}
