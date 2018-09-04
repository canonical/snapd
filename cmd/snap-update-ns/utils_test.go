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
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	log *bytes.Buffer
	sec *update.Secure
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
	s.sec = &update.Secure{}
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

// secure-mkdir-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkdirAllUnclean(c *C) {
	err := s.sec.MkdirAll("/unclean//path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a directory with an relative path.
func (s *utilsSuite) TestSecureMkdirAllRelative(c *C) {
	err := s.sec.MkdirAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can "create the root directory.
func (s *utilsSuite) TestSecureMkdirAllLevel0(c *C) {
	c.Assert(s.sec.MkdirAll("/", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory in the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel1(c *C) {
	c.Assert(s.sec.MkdirAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a directory two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel2(c *C) {
	c.Assert(s.sec.MkdirAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 3`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we can create a directory three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkdirAllLevel3(c *C) {
	c.Assert(s.sec.MkdirAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 5 "something" 0755`},
		{C: `openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 5`},
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkdirAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EROFS)
	err := s.sec.MkdirAll("/rofs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "path" 0755`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// Ensure that we don't chown existing directories.
func (s *utilsSuite) TestSecureMkdirAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "path" 0755`, syscall.EEXIST)
	err := s.sec.MkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `mkdirat 4 "path" 0755`, E: syscall.EEXIST},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we we close everything when mkdirat fails.
func (s *utilsSuite) TestSecureMkdirAllMkdiratError(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, errTesting)
	err := s.sec.MkdirAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkdirAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := s.sec.MkdirAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown directory "/path" to 123.456: testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := s.sec.MkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkdirAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := s.sec.MkdirAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
	})
}

func (s *utilsSuite) TestPlanWritableMimic(c *C) {
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return []os.FileInfo{
			testutil.FakeFileInfo("file", 0),
			testutil.FakeFileInfo("dir", os.ModeDir),
			testutil.FakeFileInfo("symlink", os.ModeSymlink),
			testutil.FakeFileInfo("error-symlink-readlink", os.ModeSymlink),
			// NOTE: None of the filesystem entries below are supported because
			// they cannot be placed inside snaps or can only be created at
			// runtime in areas that are already writable and this would never
			// have to be handled in a writable mimic.
			testutil.FakeFileInfo("block-dev", os.ModeDevice),
			testutil.FakeFileInfo("char-dev", os.ModeDevice|os.ModeCharDevice),
			testutil.FakeFileInfo("socket", os.ModeSocket),
			testutil.FakeFileInfo("pipe", os.ModeNamedPipe),
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

	changes, err := update.PlanWritableMimic("/foo", "/foo/bar")
	c.Assert(err, IsNil)

	c.Assert(changes, DeepEquals, []*update.Change{
		// Store /foo in /tmp/.snap/foo while we set things up
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		// Put a tmpfs over /foo
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar", "mode=0755", "uid=0", "gid=0"}}, Action: update.Mount},
		// Bind mount files and directories over. Note that files are identified by x-snapd.kind=file option.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// Create symlinks.
		// Bad symlinks and all other file types are skipped and not
		// recorded in mount changes.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// Unmount the safe-keeping directory
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestPlanWritableMimicErrors(c *C) {
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	restore := update.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return nil, errTesting
	})
	defer restore()
	restore = update.MockReadlink(func(name string) (string, error) {
		return "", errTesting
	})
	defer restore()

	changes, err := update.PlanWritableMimic("/foo", "/foo/bar")
	c.Assert(err, ErrorMatches, "testing")
	c.Assert(changes, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicSuccess(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes, each of the change we perform is coming from the plan.
	restore := update.MockChangePerform(func(chg *update.Change, sec *update.Secure) ([]*update.Change, error) {
		c.Assert(plan, testutil.DeepContains, chg)
		return nil, nil
	})
	defer restore()

	// The executed plan leaves us with a simplified view of the plan that is suitable for undo.
	undoPlan, err := update.ExecWritableMimic(plan, s.sec)
	c.Assert(err, IsNil)
	c.Assert(undoPlan, DeepEquals, []*update.Change{
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar", "x-snapd.detach"}}, Action: update.Mount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorWithRecovery(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		// NOTE: the next perform will fail. Notably the symlink did not fail.
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes. Before we inject a failure we ensure
	// that each of the change we perform is coming from the plan. For the
	// purpose of the test the change that bind mounts the "dir" over itself
	// will fail and will trigger an recovery path. The changes performed in
	// the recovery path are recorded.
	var recoveryPlan []*update.Change
	recovery := false
	restore := update.MockChangePerform(func(chg *update.Change, sec *update.Secure) ([]*update.Change, error) {
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
	undoPlan, err := update.ExecWritableMimic(plan, s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
	// The changes we managed to perform were undone correctly.
	c.Assert(recoveryPlan, DeepEquals, []*update.Change{
		// NOTE: there is no symlink undo entry as it is implicitly undone by unmounting the tmpfs.
		{Entry: osutil.MountEntry{Name: "/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Unmount},
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind", "x-snapd.detach"}}, Action: update.Unmount},
	})
}

func (s *utilsSuite) TestExecWirableMimicErrorNothingDone(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes and just fail on any request.
	restore := update.MockChangePerform(func(chg *update.Change, sec *update.Secure) ([]*update.Change, error) {
		return nil, errTesting
	})
	defer restore()

	// The executed plan fails, the recovery didn't fail (it's empty) so we just return that error.
	undoPlan, err := update.ExecWritableMimic(plan, s.sec)
	c.Assert(err, Equals, errTesting)
	c.Assert(undoPlan, HasLen, 0)
}

func (s *utilsSuite) TestExecWirableMimicErrorCannotUndo(c *C) {
	// This plan is the same as in the test above. This is what comes out of planWritableMimic.
	plan := []*update.Change{
		{Entry: osutil.MountEntry{Name: "/foo", Dir: "/tmp/.snap/foo", Options: []string{"rbind"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/foo", Type: "tmpfs", Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/file", Dir: "/foo/file", Options: []string{"bind", "x-snapd.kind=file", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/dir", Dir: "/foo/dir", Options: []string{"rbind", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "/tmp/.snap/foo/symlink", Dir: "/foo/symlink", Options: []string{"x-snapd.kind=symlink", "x-snapd.symlink=target", "x-snapd.synthetic", "x-snapd.needed-by=/foo/bar"}}, Action: update.Mount},
		{Entry: osutil.MountEntry{Name: "none", Dir: "/tmp/.snap/foo", Options: []string{"x-snapd.detach"}}, Action: update.Unmount},
	}

	// Mock the act of performing changes. After performing the first change
	// correctly we will fail forever (this includes the recovery path) so the
	// execute function ends up in a situation where it cannot perform the
	// recovery path and will have to return a fatal error.
	i := -1
	restore := update.MockChangePerform(func(chg *update.Change, sec *update.Secure) ([]*update.Change, error) {
		i++
		if i > 0 {
			return nil, fmt.Errorf("failure-%d", i)
		}
		return nil, nil
	})
	defer restore()

	// The plan partially succeeded and we cannot undo those changes.
	_, err := update.ExecWritableMimic(plan, s.sec)
	c.Assert(err, ErrorMatches, `cannot undo change ".*" while recovering from earlier error failure-1: failure-2`)
	c.Assert(err, FitsTypeOf, &update.FatalError{})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct {
	sec *update.Secure
}

var _ = Suite(&realSystemSuite{})

func (s *realSystemSuite) SetUpTest(c *C) {
	s.sec = &update.Secure{}
}

// Check that we can actually create directories.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMkdirAllForReal(c *C) {
	d := c.MkDir()

	// Create d (which already exists) with mode 0777 (but c.MkDir() used 0700
	// internally and since we are not creating the directory we should not be
	// changing that.
	c.Assert(s.sec.MkdirAll(d, 0777, sys.FlagID, sys.FlagID), IsNil)
	fi, err := os.Stat(d)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0700))

	// Create d1, which is a simple subdirectory, with a distinct mode and
	// check that it was applied. Note that default umask 022 is subtracted so
	// effective directory has different permissions.
	d1 := filepath.Join(d, "subdir")
	c.Assert(s.sec.MkdirAll(d1, 0707, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(d1)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create d2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	d2 := filepath.Join(d, "subdir/subdir/subdir")
	c.Assert(s.sec.MkdirAll(d2, 0750, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(d2)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// secure-mkfile-all

// Ensure that we reject unclean paths.
func (s *utilsSuite) TestSecureMkfileAllUnclean(c *C) {
	err := s.sec.MkfileAll("/unclean//path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot split unclean path .*`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse to create a file with an relative path.
func (s *utilsSuite) TestSecureMkfileAllRelative(c *C) {
	err := s.sec.MkfileAll("rel/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create file with relative path: "rel/path"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we refuse creating the root directory as a file.
func (s *utilsSuite) TestSecureMkfileAllLevel0(c *C) {
	err := s.sec.MkfileAll("/", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
	c.Assert(s.sys.RCalls(), HasLen, 0)
}

// Ensure that we can create a file in the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel1(c *C) {
	c.Assert(s.sec.MkfileAll("/path", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Ensure that we can create a file two levels from the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel2(c *C) {
	c.Assert(s.sec.MkfileAll("/path/to", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `close 3`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we can create a file three levels from the top-level directory.
func (s *utilsSuite) TestSecureMkfileAllLevel3(c *C) {
	c.Assert(s.sec.MkfileAll("/path/to/something", 0755, 123, 456), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "path" 0755`},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fchown 4 123 456`},
		{C: `mkdirat 4 "to" 0755`},
		{C: `openat 4 "to" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `fchown 5 123 456`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `openat 5 "something" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 5`},
	})
}

// Ensure that we can detect read only filesystems.
func (s *utilsSuite) TestSecureMkfileAllROFS(c *C) {
	s.sys.InsertFault(`mkdirat 3 "rofs" 0755`, syscall.EEXIST) // just realistic
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EROFS)
	err := s.sec.MkfileAll("/rofs/path", 0755, 123, 456)
	c.Check(err, ErrorMatches, `cannot operate on read-only filesystem at /rofs`)
	c.Assert(err.(*update.ReadOnlyFsError).Path, Equals, "/rofs")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "rofs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "rofs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EROFS},
		{C: `close 4`},
	})
}

// Ensure that we don't chown existing files or directories.
func (s *utilsSuite) TestSecureMkfileAllExistingDirsDontChown(c *C) {
	s.sys.InsertFault(`mkdirat 3 "abs" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	err := s.sec.MkfileAll("/abs/path", 0755, 123, 456)
	c.Check(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EEXIST},
		{C: `openat 4 "path" O_NOFOLLOW|O_CLOEXEC 0`, R: 3},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// Ensure that we we close everything when openat fails.
func (s *utilsSuite) TestSecureMkfileAllOpenat2ndError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, syscall.EEXIST)
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, errTesting)
	err := s.sec.MkfileAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: syscall.EEXIST},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC 0`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when openat (non-exclusive) fails.
func (s *utilsSuite) TestSecureMkfileAllOpenatError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, errTesting)
	err := s.sec.MkfileAll("/abs", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open file "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, E: errTesting},
		{C: `close 3`},
	})
}

// Ensure that we we close everything when fchown fails.
func (s *utilsSuite) TestSecureMkfileAllFchownError(c *C) {
	s.sys.InsertFault(`fchown 4 123 456`, errTesting)
	err := s.sec.MkfileAll("/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot chown file "/path" to 123.456: testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `openat 3 "path" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 4},
		{C: `fchown 4 123 456`, E: errTesting},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// Check error path when we cannot open root directory.
func (s *utilsSuite) TestSecureMkfileAllOpenRootError(c *C) {
	s.sys.InsertFault(`open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := s.sec.MkfileAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, "cannot open root directory: testing")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
	})
}

// Check error path when we cannot open non-root directory.
func (s *utilsSuite) TestSecureMkfileAllOpenError(c *C) {
	s.sys.InsertFault(`openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, errTesting)
	err := s.sec.MkfileAll("/abs/path", 0755, 123, 456)
	c.Assert(err, ErrorMatches, `cannot open directory "/abs": testing`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "abs" 0755`},
		{C: `openat 3 "abs" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, E: errTesting},
		{C: `close 3`},
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
	c.Assert(s.sec.MkfileAll(f1, 0707, sys.FlagID, sys.FlagID), IsNil)
	fi, err := os.Stat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0705))

	// Create f2, which is a deeper subdirectory, with another distinct mode
	// and check that it was applied.
	f2 := filepath.Join(d, "subdir/subdir/file")
	c.Assert(s.sec.MkfileAll(f2, 0750, sys.FlagID, sys.FlagID), IsNil)
	fi, err = os.Stat(f2)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().IsRegular(), Equals, true)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0750))
}

// Check that we can actually create symlinks.
// This doesn't test the chown logic as that requires root.
func (s *realSystemSuite) TestSecureMksymlinkAllForReal(c *C) {
	d := c.MkDir()

	// Create symlink f1 that points to "oldname" and check that it
	// is correct. Note that symlink permissions are always set to 0777
	f1 := filepath.Join(d, "symlink")
	err := s.sec.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname")
	c.Assert(err, IsNil)
	fi, err := os.Lstat(f1)
	c.Assert(err, IsNil)
	c.Check(fi.Mode()&os.ModeSymlink, Equals, os.ModeSymlink)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0777))

	target, err := os.Readlink(f1)
	c.Assert(err, IsNil)
	c.Check(target, Equals, "oldname")

	// Create an identical symlink to see that it doesn't fail.
	err = s.sec.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "oldname")
	c.Assert(err, IsNil)

	// Create a different symlink and see that it fails now
	err = s.sec.MksymlinkAll(f1, 0755, sys.FlagID, sys.FlagID, "other")
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/symlink": existing symbolic link in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f2 := filepath.Join(d, "file")
	err = s.sec.MkfileAll(f2, 0755, sys.FlagID, sys.FlagID)
	c.Assert(err, IsNil)
	err = s.sec.MksymlinkAll(f2, 0755, sys.FlagID, sys.FlagID, "oldname")
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/file": existing file in the way`)

	// Create an file and check that it clashes with a symlink we attempt to create.
	f3 := filepath.Join(d, "dir")
	err = s.sec.MkdirAll(f3, 0755, sys.FlagID, sys.FlagID)
	c.Assert(err, IsNil)
	err = s.sec.MksymlinkAll(f3, 0755, sys.FlagID, sys.FlagID, "oldname")
	c.Assert(err, ErrorMatches, `cannot create symbolic link ".*/dir": existing file in the way`)

	err = s.sec.MksymlinkAll("/", 0755, sys.FlagID, sys.FlagID, "oldname")
	c.Assert(err, ErrorMatches, `cannot create non-file path: "/"`)
}

func (s *utilsSuite) TestCleanTrailingSlash(c *C) {
	// This is a sanity test for the use of filepath.Clean in secureMk{dir,file}All
	c.Assert(filepath.Clean("/path/"), Equals, "/path")
	c.Assert(filepath.Clean("path/"), Equals, "path")
	c.Assert(filepath.Clean("path/."), Equals, "path")
	c.Assert(filepath.Clean("path/.."), Equals, ".")
	c.Assert(filepath.Clean("other/path/.."), Equals, "other")
}

// secure-open-path

func (s *utilsSuite) TestSecureOpenPath(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 5 <ptr>", stat)
	fd, err := update.OpenPath("/foo/bar")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 5)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 4},
		{C: `openat 4 "bar" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 5},
		{C: `fstat 5 <ptr>`, R: stat},
		{C: `close 4`},
		{C: `close 3`},
	})
}

func (s *utilsSuite) TestSecureOpenPathSingleSegment(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 4 <ptr>", stat)
	fd, err := update.OpenPath("/foo")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 4)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `openat 3 "foo" O_NOFOLLOW|O_CLOEXEC|O_PATH 0`, R: 4},
		{C: `fstat 4 <ptr>`, R: stat},
		{C: `close 3`},
	})
}

func (s *utilsSuite) TestSecureOpenPathRoot(c *C) {
	stat := syscall.Stat_t{Mode: syscall.S_IFDIR}
	s.sys.InsertFstatResult("fstat 3 <ptr>", stat)
	fd, err := update.OpenPath("/")
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	c.Assert(fd, Equals, 3)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY|O_PATH 0`, R: 3},
		{C: `fstat 3 <ptr>`, R: stat},
	})
}

func (s *utilsSuite) TestIsReadOnlySquashfsMountedRo(c *C) {
	statfs := &syscall.Statfs_t{Type: update.SquashfsMagic, Flags: update.StReadOnly}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsReadOnly(fd, path, statfs)
	c.Assert(result, Equals, true)
}

func (s *utilsSuite) TestIsReadOnlySquashfsMountedRw(c *C) {
	statfs := &syscall.Statfs_t{Type: update.SquashfsMagic}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsReadOnly(fd, path, statfs)
	c.Assert(result, Equals, true)
}

func (s *utilsSuite) TestIsReadOnlyExt4MountedRw(c *C) {
	statfs := &syscall.Statfs_t{Type: update.Ext4Magic}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsReadOnly(fd, path, statfs)
	c.Assert(result, Equals, false)
}

func (s *utilsSuite) TestIsSnapdCreatedPrivateTmpfsNotATmpfs(c *C) {
	// An ext4 (which is not a tmpfs) is not a private tmpfs.
	statfs := &syscall.Statfs_t{Type: update.Ext4Magic}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, nil)
	c.Assert(result, Equals, false)
}

func (s *utilsSuite) TestIsSnapdCreatedPrivateTmpfsNotTrusted(c *C) {
	// A tmpfs is not private if it doesn't come from a change we made.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, nil)
	c.Assert(result, Equals, false)
}

func (s *utilsSuite) TestIsSnapdCreatedPrivateTmpfsViaChanges(c *C) {
	// A tmpfs is private because it was mounted by snap-update-ns.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	path := "/some/path"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)

	// A tmpfs was mounted in the past so it is private.
	result := update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)

	// A tmpfs was mounted but then it was unmounted so it is not private anymore.
	result = update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)

	// Finally, after the mounting and unmounting the tmpfs was mounted again.
	result = update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Unmount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: path, Type: "tmpfs"}},
	})
	c.Assert(result, Equals, true)
}

func (s *utilsSuite) TestIsSnapdCreatedPrivateTmpfsDeeper(c *C) {
	// A tmpfs is not private beyond the exact mount point from a change.
	// That is, sub-directories of a private tmpfs are not recognized as private.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	fd, err := s.sys.Open("/some/path/below", syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsSnapdCreatedPrivateTmpfs(fd, "/some/path/below", statfs, []*update.Change{
		{Action: update.Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: "/some/path", Type: "tmpfs"}},
	})
	c.Assert(result, Equals, false)
}

func (s *utilsSuite) TestIsSnapdCreatedPrivateTmpfsViaVarLib(c *C) {
	// A tmpfs in /var/lib is private because it is a special
	// quirk applied by snap-confine, without having a change record.
	statfs := &syscall.Statfs_t{Type: update.TmpfsMagic}
	path := "/var/lib"
	fd, err := s.sys.Open(path, syscall.O_DIRECTORY, 0)
	c.Assert(err, IsNil)
	defer s.sys.Close(fd)
	result := update.IsSnapdCreatedPrivateTmpfs(fd, path, statfs, nil)
	c.Assert(result, Equals, true)
}

func (s *realSystemSuite) TestSecureOpenPathDirectory(c *C) {
	path := filepath.Join(c.MkDir(), "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenPath(path)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	// check that the file descriptor is for the expected path
	origDir, err := os.Getwd()
	c.Assert(err, IsNil)
	defer os.Chdir(origDir)

	c.Assert(syscall.Fchdir(fd), IsNil)
	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Check(cwd, Equals, path)
}

func (s *realSystemSuite) TestSecureOpenPathRelativePath(c *C) {
	fd, err := update.OpenPath("relative/path")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "path .* is not absolute")
}

func (s *realSystemSuite) TestSecureOpenPathUncleanPath(c *C) {
	base := c.MkDir()
	path := filepath.Join(base, "test")
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenPath(base + "//test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*//test"`)

	fd, err = update.OpenPath(base + "/./test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/./test"`)

	fd, err = update.OpenPath(base + "/test/../test")
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `cannot open path: cannot iterate over unclean path ".*/test/../test"`)
}

func (s *realSystemSuite) TestSecureOpenPathFile(c *C) {
	path := filepath.Join(c.MkDir(), "file.txt")
	c.Assert(ioutil.WriteFile(path, []byte("hello"), 0644), IsNil)

	fd, err := update.OpenPath(path)
	c.Assert(err, IsNil)
	defer syscall.Close(fd)

	// Check that the file descriptor matches the file.
	var pathStat, fdStat syscall.Stat_t
	c.Assert(syscall.Stat(path, &pathStat), IsNil)
	c.Assert(syscall.Fstat(fd, &fdStat), IsNil)
	c.Check(pathStat, Equals, fdStat)
}

func (s *realSystemSuite) TestSecureOpenPathNotFound(c *C) {
	path := filepath.Join(c.MkDir(), "test")

	fd, err := update.OpenPath(path)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "no such file or directory")
}

func (s *realSystemSuite) TestSecureOpenPathSymlink(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "test")
	c.Assert(os.Mkdir(dir, 0755), IsNil)

	symlink := filepath.Join(base, "symlink")
	c.Assert(os.Symlink(dir, symlink), IsNil)

	fd, err := update.OpenPath(symlink)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, `".*" is a symbolic link`)
}

func (s *realSystemSuite) TestSecureOpenPathSymlinkedParent(c *C) {
	base := c.MkDir()
	dir := filepath.Join(base, "dir1")
	symlink := filepath.Join(base, "symlink")

	path := filepath.Join(dir, "dir2")
	symlinkedPath := filepath.Join(symlink, "dir2")

	c.Assert(os.Mkdir(dir, 0755), IsNil)
	c.Assert(os.Symlink(dir, symlink), IsNil)
	c.Assert(os.Mkdir(path, 0755), IsNil)

	fd, err := update.OpenPath(symlinkedPath)
	c.Check(fd, Equals, -1)
	c.Check(err, ErrorMatches, "not a directory")
}
