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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type utilsSuite struct {
	testutil.BaseTest
	sys *testutil.SyscallRecorder
	log *bytes.Buffer
	as  *update.Assumptions
}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.sys = &testutil.SyscallRecorder{}
	s.BaseTest.AddCleanup(osutil.MockSystemCalls(s.sys))
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
	s.as = &update.Assumptions{}
}

func (s *utilsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.sys.CheckForStrayDescriptors(c)
}

// plan and execute writable mimic

func (s *utilsSuite) TestPlanWritableMimic(c *C) {
	s.sys.InsertSysLstatResult(`lstat "/foo" <ptr>`, syscall.Stat_t{Uid: 0, Gid: 0, Mode: 0755})
	restore := osutil.MockReadDir(func(dir string) ([]os.FileInfo, error) {
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
	restore = osutil.MockReadlink(func(name string) (string, error) {
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
	restore := osutil.MockReadDir(func(dir string) ([]os.FileInfo, error) {
		c.Assert(dir, Equals, "/foo")
		return nil, errTesting
	})
	defer restore()
	restore = osutil.MockReadlink(func(name string) (string, error) {
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
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		c.Assert(plan, testutil.DeepContains, chg)
		return nil, nil
	})
	defer restore()

	// The executed plan leaves us with a simplified view of the plan that is suitable for undo.
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
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
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
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
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
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
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		return nil, errTesting
	})
	defer restore()

	// The executed plan fails, the recovery didn't fail (it's empty) so we just return that error.
	undoPlan, err := update.ExecWritableMimic(plan, s.as)
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
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		i++
		if i > 0 {
			return nil, fmt.Errorf("failure-%d", i)
		}
		return nil, nil
	})
	defer restore()

	// The plan partially succeeded and we cannot undo those changes.
	_, err := update.ExecWritableMimic(plan, s.as)
	c.Assert(err, ErrorMatches, `cannot undo change ".*" while recovering from earlier error failure-1: failure-2`)
	c.Assert(err, FitsTypeOf, &update.FatalError{})
}

// secure-mkdir-all

// Ensure that writes to /etc/demo are interrupted if /etc is restricted.
func (s *utilsSuite) TestSecureMkdirAllWithRestrictedEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/demo")
	err := osutil.MkdirAll("/etc/demo", 0755, 123, 456, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/demo" because it would affect the host in "/etc"`)
	c.Assert(err.(*osutil.TrespassingError).ViolatedPath, Equals, "/etc")
	c.Assert(err.(*osutil.TrespassingError).DesiredPath, Equals, "/etc/demo")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// we are inspecting the type of the filesystem we are about to perform operation on.
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		// ext4 is writable, refuse further operations.
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `close 4`},
	})
}

// Ensure that writes to /etc/demo allowed if /etc is unrestricted.
func (s *utilsSuite) TestSecureMkdirAllWithUnrestrictedEtc(c *C) {
	defer s.as.MockUnrestrictedPaths("/etc")() // Mark /etc as unrestricted.
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/demo")
	c.Assert(osutil.MkdirAll("/etc/demo", 0755, 123, 456, rs), IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		// We are not interested in the type of filesystem at /
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		// We are not interested in the type of filesystem at /etc
		{C: `close 3`},
		{C: `mkdirat 4 "demo" 0755`},
		{C: `openat 4 "demo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 123 456`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// realSystemSuite is not isolated / mocked from the system.
type realSystemSuite struct {
	as *update.Assumptions
}

var _ = Suite(&realSystemSuite{})

func (s *realSystemSuite) SetUpTest(c *C) {
	s.as = &update.Assumptions{}
	s.as.AddUnrestrictedPaths("/tmp")
}

// We want to create a symlink in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMksymlinkAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/symlink")
	err := osutil.MksymlinkAll("/etc/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/symlink" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `close 4`},
	})
}

// We want to create a symlink deep in /etc but the host filesystem would be affected.
// This just shows that we pick the right place to construct the mimic
func (s *utilsSuite) TestSecureMksymlinkAllDeepInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/some/other/stuff/symlink")
	err := osutil.MksymlinkAll("/etc/some/other/stuff/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/some/other/stuff/symlink" because it would affect the host in "/etc/"`)
	c.Assert(err.(*osutil.TrespassingError).ViolatedPath, Equals, "/etc/")
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `close 4`},
		{C: `close 3`},
	})
}

// We want to create a file in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMkfileAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/file")
	err := osutil.MkfileAll("/etc/file", 0755, 0, 0, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/file" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `close 4`},
	})
}

// We want to create a directory in /etc but the host filesystem would be affected.
func (s *utilsSuite) TestSecureMkdirAllInEtc(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.Ext4Magic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	rs := s.as.RestrictionsFor("/etc/dir")
	err := osutil.MkdirAll("/etc/dir", 0755, 0, 0, rs)
	c.Assert(err, ErrorMatches, `cannot write to "/etc/dir" because it would affect the host in "/etc"`)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.Ext4Magic}},
		{C: `close 4`},
	})
}

// We want to create a directory in /snap/foo/42/dir and want to know what happens.
func (s *utilsSuite) TestSecureMkdirAllInSNAP(c *C) {
	// Allow creating directories under /snap/ related to this snap ("foo").
	// This matches what is done inside main().
	restore := s.as.MockUnrestrictedPaths("/snap/foo")
	defer restore()

	s.sys.InsertFault(`mkdirat 3 "snap" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 4 "foo" 0755`, syscall.EEXIST)
	s.sys.InsertFault(`mkdirat 5 "42" 0755`, syscall.EEXIST)

	rs := s.as.RestrictionsFor("/snap/foo/42/dir")
	err := osutil.MkdirAll("/snap/foo/42/dir", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `mkdirat 3 "snap" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "snap" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `mkdirat 4 "foo" 0755`, E: syscall.EEXIST},
		{C: `openat 4 "foo" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 5},
		{C: `mkdirat 5 "42" 0755`, E: syscall.EEXIST},
		{C: `openat 5 "42" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 6},
		{C: `close 5`},
		{C: `close 4`},
		{C: `close 3`},
		{C: `mkdirat 6 "dir" 0755`},
		{C: `openat 6 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 6`},
	})
}

// We want to create a symlink in /etc which is a tmpfs that we mounted so that is ok.
func (s *utilsSuite) TestSecureMksymlinkAllInEtcAfterMimic(c *C) {
	// Because /etc is not on a list of unrestricted paths the write to
	// /etc/symlink must be validated with step-by-step operation.
	rootStatfs := syscall.Statfs_t{Type: update.SquashfsMagic, Flags: update.StReadOnly}
	etcStatfs := syscall.Statfs_t{Type: update.TmpfsMagic}
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, rootStatfs)
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, etcStatfs)
	rs := s.as.RestrictionsFor("/etc/symlink")
	err := osutil.MksymlinkAll("/etc/symlink", 0755, 0, 0, "/oldname", rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: rootStatfs},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: etcStatfs},
		{C: `symlinkat "/oldname" 4 "symlink"`},
		{C: `close 4`},
	})
}

// We want to create a file in /etc which is a tmpfs created by snapd so that's okay.
func (s *utilsSuite) TestSecureMkfileAllInEtcAfterMimic(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	rs := s.as.RestrictionsFor("/etc/file")
	err := osutil.MkfileAll("/etc/file", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `openat 4 "file" O_NOFOLLOW|O_CLOEXEC|O_CREAT|O_EXCL 0755`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

// We want to create a directory in /etc which is a tmpfs created by snapd so that is ok.
func (s *utilsSuite) TestSecureMkdirAllInEtcAfterMimic(c *C) {
	s.sys.InsertFstatfsResult(`fstatfs 3 <ptr>`, syscall.Statfs_t{Type: update.SquashfsMagic})
	s.sys.InsertFstatfsResult(`fstatfs 4 <ptr>`, syscall.Statfs_t{Type: update.TmpfsMagic})
	s.sys.InsertFault(`mkdirat 3 "etc" 0755`, syscall.EEXIST)
	s.as.AddChange(&update.Change{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/etc", Type: "tmpfs", Name: "tmpfs"}})
	rs := s.as.RestrictionsFor("/etc/dir")
	err := osutil.MkdirAll("/etc/dir", 0755, 0, 0, rs)
	c.Assert(err, IsNil)
	c.Assert(s.sys.RCalls(), testutil.SyscallsEqual, []testutil.CallResultError{
		{C: `open "/" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fstatfs 3 <ptr>`, R: syscall.Statfs_t{Type: update.SquashfsMagic}},
		{C: `mkdirat 3 "etc" 0755`, E: syscall.EEXIST},
		{C: `openat 3 "etc" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 4},
		{C: `close 3`},
		{C: `fstatfs 4 <ptr>`, R: syscall.Statfs_t{Type: update.TmpfsMagic}},
		{C: `mkdirat 4 "dir" 0755`},
		{C: `openat 4 "dir" O_NOFOLLOW|O_CLOEXEC|O_DIRECTORY 0`, R: 3},
		{C: `fchown 3 0 0`},
		{C: `close 3`},
		{C: `close 4`},
	})
}

func (s *utilsSuite) TestCleanTrailingSlash(c *C) {
	// This is a sanity test for the use of filepath.Clean in secureMk{dir,file}All
	c.Assert(filepath.Clean("/path/"), Equals, "/path")
	c.Assert(filepath.Clean("path/"), Equals, "path")
	c.Assert(filepath.Clean("path/."), Equals, "path")
	c.Assert(filepath.Clean("path/.."), Equals, ".")
	c.Assert(filepath.Clean("other/path/.."), Equals, "other")
}
