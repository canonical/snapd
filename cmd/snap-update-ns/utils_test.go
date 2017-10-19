// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	"os"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.SecureMkdirAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can create a directory with an absolute path.
func (s *utilsSuite) TestSecureMkdirAllAbsolute(c *C) {
	c.Assert(update.SecureMkdirAll("/abs/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 4 123 456`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`fchown 5 123 456`,
		`close 5`,
		`close 4`,
		`close 3`,
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`close 5`,
		`close 4`,
		`close 3`,
	})
}

// Ensure that we we close everything when mkdir fails.
func (s *utilsSuite) TestSecureMkdirAllCloseOnError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := update.SecureMkdirAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot mkdir path segment "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,
		`mkdirat 3 "abs" 0755`,
		`close 3`,
	})
}

func (s *utilsSuite) TestDesignWritableMimic(c *C) {
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return []os.FileInfo{
			update.FakeFileInfo("file", 0),
			update.FakeFileInfo("dir", os.ModeDir),
			update.FakeFileInfo("symlink", os.ModeSymlink),
			update.FakeFileInfo("error-symlink-readlink", os.ModeSymlink),
			// NOTE: None of the filesystem entries below are supported because
			// they cannot be placed inside snaps or can only be created at
			// runtime in areas that are already writable and this would never
			// have to be handled in a writable mimic.
			update.FakeFileInfo("block-dev", os.ModeDevice),
			update.FakeFileInfo("char-dev", os.ModeDevice|os.ModeCharDevice),
			update.FakeFileInfo("socket", os.ModeSocket),
			update.FakeFileInfo("pipe", os.ModeNamedPipe),
		}, nil
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		switch name {
		case "/foo/symlink":
			return "target", nil
		case "/foo/error-symlink-readlink":
			return "", errTesting
		}
		panic("unexpected")
	})
	defer restore()

	changes, err := update.DesignWritableMimic("/foo")
	c.Assert(err, IsNil)
	c.Assert(changes, DeepEquals, []*update.Change{
		// Store /foo in /tmp.snap/foo while we set things up
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		// Put a tmpfs over /foo
		{Entry: mount.Entry{Name: "none", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		// Bind mount files and directories over. Note that files are identified by x-snapd.kind=file option.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "ro", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind", "ro"}}, Action: update.Mount},
		// Create symlinks, if we cannot readlink just skip that entry.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"bind", "ro", "x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		// Unmount the safe-keeping directory
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestDesignWritableMimicErrors(c *C) {
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return nil, errTesting
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		return "", errTesting
	})
	defer restore()

	changes, err := update.DesignWritableMimic("/foo")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(changes, HasLen, 0)
}
