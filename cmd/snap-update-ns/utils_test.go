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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
	log *bytes.Buffer
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.sys.CheckForStrayDescriptors(c)
	s.BaseTest.TearDownTest(c)
}

// secure-mkdir-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkdirAllUnclean(c *C) {
	err := update.SecureMkdirAll("/unclean//path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := update.SecureMkdirAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can "create the root directory.
func (s *utilsSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(update.SecureMkdirAll("/", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel1(c *C) {
	os.Setenv("SNAPD_DEBUG", "1")
	defer os.Unsetenv("SNAPD_DEBUG")
	c.Assert(update.SecureMkdirAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> ...`)
	c.Assert(s.log.String(), testutil.Contains, `secure-mk-dir 3 ["path"] 0 -rwxr-xr-x 123 456 -> 4`)
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 3`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(update.SecureMkdirAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 5
		`fchown 5 123 456`,
		`close 4`,
		`close 3`,
		`mkdirat 5 "something" 0755`,
		`openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 5`,
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkdirAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EROFS)
	err := update.SecureMkdirAll("/rofs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,        // -> 3
		`mkdirat 3 "rofs" 0755`,                              // -> EEXIST
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`mkdirat 4 "path" 0755`, // -> EROFS
		`close 4`,
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`mkdirat 4 "path" 0755`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`close 3`,
		`close 4`,
	})
}

// Ensure that we we close everything when mkdirat fails.
func (s *utilsSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := update.SecureMkdirAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot mkdir path segment "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`close 3`,
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.SecureMkdirAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown path segment "path" to 123.456 \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open path segment "abs" \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
		`close 3`,
	})
}

func (s *utilsSuite) TestPlanWritableMimic(c *C) {
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

	changes, err := update.PlanWritableMimic("/foo")
	c.Assert(err, IsNil)
	c.Assert(changes, DeepEquals, []*update.Change{
		// Store /foo in /tmp/.snap/foo while we set things up
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		// Put a tmpfs over /foo
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		// Bind mount files and directories over. Note that files are identified by x-snapd.kind=file option.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
		// Create symlinks.
		// Bad symlinks and all other file types are skipped and not
		// recorded in mount changes.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		// Unmount the safe-keeping directory
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestPlanWritableMimicErrors(c *C) {
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return nil, errTesting
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		return "", errTesting
	})
	defer restore()

	changes, err := update.PlanWritableMimic("/foo")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(changes, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicSuccess(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	}

	// Mock the act of performing changes, each of the change we perform is coming from the plan.
	restore := update.MockChangePerform(func(chg *update.Change) ([]*update.Change, error) {
		c.Assert(plan, testutil.DeepContains, chg)
		return nil, nil
	})
	defer restore()

	// The executed plan leaves us with a simplified view of the plan that is suitable for undo.
	undoPlan, err := update.ExecWritableMimic(plan)
	c.Assert(err, IsNil)
	c.Assert(undoPlan, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorWithRecovery(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		// NOTE: the next perform will fail. Notably the symlink did not fail.
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	}

	// Mock the act of performing changes. Before we inject a failure we ensure
	// that each of the change we perform is coming from the plan. For the
	// purpose of the test the change that bind mounts the "dir" over itself
	// will fail and will trigger an recovery path. The changes performed in
	// the recovery path are recorded.
	var recoveryPlan []*update.Change
	recovery := false
	restore := update.MockChangePerform(func(chg *update.Change) ([]*update.Change, error) {
		if !recovery {
			c.Assert(plan, testutil.DeepContains, chg)
			if chg.Entry.Name == "/tmp/.snap/foo/dir" {
				recovery = true // switch to recovery mode
				return nil, errTesting
			}
		} else {
			recoveryPlan = append(recoveryPlan, chg)
		}
		return nil, nil
	})
	defer restore()

	// The executed plan fails, leaving us with the error and an empty undo plan.
	undoPlan, err := update.ExecWritableMimic(plan)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
	// The changes we managed to perform were undone correctly.
	c.Assert(recoveryPlan, DeepEquals, []*update.Change{
		// NOTE: there is no symlink undo entry as it is implicitly undone by unmounting the tmpfs.
		{Entry: mount.Entry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Unmount},
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Unmount},
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorNothingDone(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	}

	// Mock the act of performing changes and just fail on any request.
	restore := update.MockChangePerform(func(chg *update.Change) ([]*update.Change, error) {
		return nil, errTesting
	})
	defer restore()

	// The executed plan fails, the recovery didn't fail (it's empty) so we just return that error.
	undoPlan, err := update.ExecWritableMimic(plan)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicErrorCannotUndo(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: mount.Entry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs"}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"bind"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target"}}, Action: update.Mount},
		{Entry: mount.Entry{Name: "none", Dir: "/tmp/.snap/foo"}, Action: update.Unmount},
	}

	// Mock the act of performing changes. After performing the first change
	// correctly we will fail forever (this includes the recovery path) so the
	// execute function ends up in a situation where it cannot perform the
	// recovery path and will have to return a fatal error.
	i := -1
	restore := update.MockChangePerform(func(chg *update.Change) ([]*update.Change, error) {
		i++
		if i > 0 {
			return nil, fmt.Errorf("failure-%d", i)
		}
		return nil, nil
	})
	defer restore()

	// The plan partially succeeded and we cannot undo those changes.
	_, err := update.ExecWritableMimic(plan)
	c.Assert(err, ErrorMatches, `cannot undo change ".*" while recovering from earlier error failure-1: failure-2`)
	c.Assert(err, FitsTypeOf, &update.FatalError{})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct{}

var _ = Suite(&realSystemSuite{})

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllForReal(c *C) {
	d := c.MkDir()

	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(update.SecureMkdirAll(d, 0777, sys.FlagID, sys.FlagID), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(update.SecureMkdirAll(d1, 0707, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(update.SecureMkdirAll(d2, 0750, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// secure-mkfile-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkfileAllUnclean(c *C) {
	err := update.SecureMkfileAll("/unclean//path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we refuse to create a file with an relative path.
func (s *utilsSuite) TestSecureMkfileAllRelative(c *C) {
	err := update.SecureMkfileAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create file with relative path: "rel/path"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we refuse creating the root directory as a file.
func (s *utilsSuite) TestSecureMkfileAllLevel0(c *C) {
	err := update.SecureMkfileAll("/", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Ensure that we can create a file in the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel1(c *C) {
	c.Assert(update.SecureMkfileAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,              // -> 3
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
}

// Ensure that we can create a file two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel2(c *C) {
	c.Assert(update.SecureMkfileAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`close 3`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 4`,
	})
}

// Ensure that we can create a file three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel3(c *C) {
	c.Assert(update.SecureMkfileAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "path" 0755`,
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`fchown 4 123 456`,
		`mkdirat 4 "to" 0755`,
		`openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 5
		`fchown 5 123 456`,
		`close 4`,
		`close 3`,
		`openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> 3
		`fchown 3 123 456`,
		`close 3`,
		`close 5`,
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkfileAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS)
	err := update.SecureMkfileAll("/rofs/path", 0755, 123, 456)
	c.Check(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,        // -> 3
		`mkdirat 3 "rofs" 0755`,                              // -> EEXIST
		`openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> EROFS
		`close 4`,
	})
}

// Ensure that we don't chown existing files or directories.
func (s *utilsSuite) TestSecureMkfileAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	err := update.SecureMkfileAll("/abs/path", 0755, 123, 456)
	c.Check(err, IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 4
		`close 3`,
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> EEXIST
		`openat 4 "path" O_NOFOLLOW|O_CLOEXEC 0`,                   // -> 3
		`close 3`,
		`close 4`,
	})
}

// Ensure that we we close everything when openat fails.
func (s *utilsSuite) TestSecureMkfileAllOpenat2ndError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, errTesting)
	err := update.SecureMkfileAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open file "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,             // -> 3
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> EEXIST
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`,                   // -> err
		`close 3`,
	})
}

// Ensure that we we close everything when openat (non-exclusive) fails.
func (s *utilsSuite) TestSecureMkfileAllOpenatError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	err := update.SecureMkfileAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open file "abs": testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,             // -> 3
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> err
		`close 3`,
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkfileAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := update.SecureMkfileAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown file "path" to 123.456: testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`,              // -> 3
		`openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, // -> 4
		`fchown 4 123 456`,
		`close 4`,
		`close 3`,
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkfileAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkfileAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkfileAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := update.SecureMkfileAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open path segment "abs" \(got up to "/"\): testing`)
	c.Assert(s.sys.Calls(), DeepEquals, []string{
		`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> 3
		`mkdirat 3 "abs" 0755`,
		`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, // -> err
		`close 3`,
	})
}

// Check that we can actually create files.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkfileAllForReal(c *C) {
	d := c.MkDir()

	// Create f1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	f1 := filepath.Join(d, "file")
	c.Assert(update.SecureMkfileAll(f1, 0707, sys.FlagID, sys.FlagID), IsNil)
	fi, err := os.Stat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create f2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	f2 := filepath.Join(d, "subdir/subdir/file")
	c.Assert(update.SecureMkfileAll(f2, 0750, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(f2)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

func (s *utilsSuite) TestCleanTrailingSlash(c *C) {
	// This is a sanity test for the use of filepath.Clean in secureMk{dir,file}All
	c.Assert(filepath.Clean("/path/"), Equals, "/path")
	c.Assert(filepath.Clean("path/"), Equals, "path")
	c.Assert(filepath.Clean("path/."), Equals, "path")
	c.Assert(filepath.Clean("path/.."), Equals, ".")
	c.Assert(filepath.Clean("other/path/.."), Equals, "other")
}

func (s *utilsSuite) TestSplitIntoSegments(c *C) {
	sg, err := update.SplitIntoSegments("/foo/bar/froz")
	c.Assert(err, IsNil)
	c.Assert(sg, DeepEquals, []string{"foo", "bar", "froz"})

	sg, err = update.SplitIntoSegments("/foo//fii/../.")
	c.Assert(err, ErrorMatches, `cannot split unclean path ".+"`)
	c.Assert(sg, HasLen, 0)
}
