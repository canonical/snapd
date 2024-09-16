// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
package cgroup_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type killSuite struct {
	testutil.BaseTest
	rootDir string
}

var _ = Suite(&killSuite{})

func (s *killSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func mockCgroupsWithProcs(c *C, cgroupsToProcs map[string][]string) {
	for cg, pids := range cgroupsToProcs {
		procs := filepath.Join(dirs.GlobalRootDir, cg, "cgroup.procs")
		c.Assert(os.MkdirAll(filepath.Dir(procs), 0755), IsNil)
		c.Assert(os.WriteFile(procs, []byte(strings.Join(pids, "\n")), 0644), IsNil)
	}
}

func (s *killSuite) TestKillSnapProcessesV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	cgroupsToProcs := map[string][]string{
		// Transient cgroups for snap "foo"
		"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.1234-1234-1234.scope": {"1"},
		"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.9876.scope":           {"2", "3"},
		"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-2.some-scope.scope":     {"4"},
		// Freezer cgroup for snap "foo"
		"/sys/fs/cgroup/freezer/snap.foo": {"1", "2", "3", "4"},
		// Transient cgroups for snap "bar"
		"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.bar.app-1.1234-1234-1234.scope": {"6", "7"},
		// Freezer cgroup for snap "bar"
		"/sys/fs/cgroup/freezer/snap.bar": {"6", "7"},
	}
	mockCgroupsWithProcs(c, cgroupsToProcs)

	var ops []string
	restore = cgroup.MockKillProcessesInCgroup(func(dir string) error {
		// trim tmp root dir
		dir = strings.TrimPrefix(dir, s.rootDir)
		ops = append(ops, fmt.Sprintf("kill cgroup: %s", dir))
		return nil
	})
	defer restore()

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)
	c.Assert(ops, DeepEquals, []string{
		"kill cgroup: /sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.1234-1234-1234.scope",
		"kill cgroup: /sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.9876.scope",
		"kill cgroup: /sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-2.some-scope.scope",
		"kill cgroup: /sys/fs/cgroup/freezer/snap.foo",
	})
}

func (s *killSuite) TestKillSnapProcessesV1NoCgroups(c *C) {
	// Simulate the case of a snap that was never run so
	// snap-confine never even created the freezer for it.
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	snapName := "foo"
	cg := filepath.Join(cgroup.FreezerCgroupV1Dir(), fmt.Sprintf("snap.%s", snapName))
	c.Assert(cg, testutil.FileAbsent)

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), snapName), IsNil)
}

func (s *killSuite) testKillSnapProcessesV2(c *C, cgroupKillSupported bool) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	cgroupsToProcs := map[string][]string{
		// Transient cgroups for snap "foo"
		"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope": {"1"},
		"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.9876.scope":           {"2", "3"},
		"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-2.some-scope.scope":     {"4"},
		// Transient cgroups for snap "bar"
		"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.bar.app-1.1234-1234-1234.scope": {"5"},
	}
	mockCgroupsWithProcs(c, cgroupsToProcs)

	if cgroupKillSupported {
		for cg := range cgroupsToProcs {
			c.Assert(os.WriteFile(filepath.Join(s.rootDir, cg, "cgroup.kill"), []byte(""), 0644), IsNil)
		}
	}

	var ops []string
	restore = cgroup.MockKillProcessesInCgroup(func(dir string) error {
		// trim tmp root dir
		dir = strings.TrimPrefix(dir, s.rootDir)
		ops = append(ops, fmt.Sprintf("kill cgroup: %s", dir))
		return nil
	})
	defer restore()

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)

	if cgroupKillSupported {
		for cg := range cgroupsToProcs {
			cgKill := filepath.Join(s.rootDir, cg, "cgroup.kill")
			if strings.HasSuffix(cg, "snap.bar.app-1.1234-1234-1234.scope") {
				c.Assert(cgKill, testutil.FileEquals, "")
			} else {
				// "1" was written to cgroup.kill
				c.Assert(cgKill, testutil.FileEquals, "1")
			}
		}
		// Didn't fallback to classic implementation
		c.Assert(ops, IsNil)
	} else {
		for cg := range cgroupsToProcs {
			cgKill := filepath.Join(s.rootDir, cg, "cgroup.kill")
			c.Assert(cgKill, testutil.FileAbsent)
		}
		c.Assert(ops, DeepEquals, []string{
			"kill cgroup: /sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope",
			"kill cgroup: /sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.9876.scope",
			"kill cgroup: /sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-2.some-scope.scope",
		})
	}
}

func (s *killSuite) TestKillSnapProcessesV2(c *C) {
	const cgroupKillSupported = false
	s.testKillSnapProcessesV2(c, cgroupKillSupported)
}

func (s *killSuite) TestKillSnapProcessesV2CgKillSupported(c *C) {
	// cgroup.kill requires linux 5.14+
	const cgroupKillSupported = true
	s.testKillSnapProcessesV2(c, cgroupKillSupported)
}

func removePid(cgroupsToProcs map[string][]string, targetPid int) map[string][]string {
	newCgroupsToProcs := make(map[string][]string, len(cgroupsToProcs))
	for cg, pids := range cgroupsToProcs {
		var newPids []string
		for _, pid := range pids {
			if strconv.Itoa(targetPid) == pid {
				continue
			}
			newPids = append(newPids, pid)
		}
		newCgroupsToProcs[cg] = newPids
	}

	return newCgroupsToProcs
}

func (s *killSuite) testKillSnapProcessesError(c *C, cgVersion int, freezerOnly bool) {
	restore := cgroup.MockVersion(cgVersion, nil)
	defer restore()

	var cgroupsToProcs map[string][]string
	if cgVersion == cgroup.V1 {
		if freezerOnly {
			// This tests the workaround implemented for systemd v237 (used by Ubuntu 18.04) for
			// non-root users where a transient scope cgroup is not created for a snap hence it
			// cannot be tracked by the usual snap.<security-tag>-<uuid>.scope pattern.
			cgroupsToProcs = map[string][]string{
				"/sys/fs/cgroup/freezer/snap.foo": {"1", "2", "3", "4"},
			}
		} else {
			cgroupsToProcs = map[string][]string{
				"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.1234-1234-1234.scope": {"1"},
				"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.9876.scope":           {"2", "3"},
				"/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-2.some-scope.scope":     {"4"},
				"/sys/fs/cgroup/freezer/snap.foo": {"1", "2", "3", "4"},
			}
		}
	} else {
		cgroupsToProcs = map[string][]string{
			"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope": {"1"},
			"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.9876.scope":           {"2", "3"},
			"/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-2.some-scope.scope":     {"4"},
		}
	}
	mockCgroupsWithProcs(c, cgroupsToProcs)

	var ops []string
	restore = cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		if pid == 1 || pid == 2 {
			return fmt.Errorf("mock error for pid %d", pid)
		}

		// simulate killing pid
		cgroupsToProcs = removePid(cgroupsToProcs, pid)
		mockCgroupsWithProcs(c, cgroupsToProcs)

		ops = append(ops, fmt.Sprintf("kill-pid:%d, signal:%d", pid, sig))
		return nil
	})
	defer restore()

	// Call failed and reported first error only
	err := cgroup.KillSnapProcesses(context.TODO(), "foo")
	c.Assert(err, ErrorMatches, "mock error for pid 1")

	// But kept going
	c.Assert(ops, DeepEquals, []string{
		// Pid 1 not killed due to error
		// Pid 2 not killed due to error
		"kill-pid:3, signal:9",
		"kill-pid:4, signal:9",
	})
}

func (s *killSuite) TestKillSnapProcessesV1Error(c *C) {
	const cgVersion = cgroup.V1
	const freezerOnly = false
	s.testKillSnapProcessesError(c, cgVersion, freezerOnly)
}

func (s *killSuite) TestKillSnapProcessesV1ErrorSystemd237Regression(c *C) {
	// This tests the workaround implemented for systemd v237 (used by Ubuntu 18.04) for
	// non-root users where a transient scope cgroup is not created for a snap hence it
	// cannot be tracked by the usual snap.<security-tag>-<uuid>.scope pattern.
	const cgVersion = cgroup.V1
	const freezerOnly = true
	s.testKillSnapProcessesError(c, cgVersion, freezerOnly)
}

func (s *killSuite) TestKillSnapProcessesV2Error(c *C) {
	const cgVersion = cgroup.V2
	const freezerOnly = false
	s.testKillSnapProcessesError(c, cgVersion, freezerOnly)
}

func (s *killSuite) testKillSnapProcessesSkippedErrors(c *C, cgVersion int) {
	restore := cgroup.MockVersion(cgVersion, nil)
	defer restore()

	cg := "/sys/fs/cgroup/pids/user.slice/user-0.slice/user@0.service/snap.foo.app-1.1234-1234-1234.scope"
	if cgVersion == cgroup.V2 {
		cg = "/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope"
	}
	mockCgroupsWithProcs(c, map[string][]string{cg: {"1"}})

	var cgroupErr error
	restore = cgroup.MockKillProcessesInCgroup(func(dir string) error {
		return cgroupErr
	})
	defer restore()

	// ENOENT should be ignored to account for a cgroup going away before processing
	cgroupErr = fs.ErrNotExist
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)

	// ENODEV should also be ignored to account to cgroup going away in the middle of
	// kernel work (kernfs implementation return ENODEV)
	cgroupErr = syscall.ENODEV
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)

	// Other errors are propagated
	cgroupErr = errors.New("cgroup error")
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), ErrorMatches, "cgroup error")
}

func (s *killSuite) TestKillSnapProcessesSkippedErrorsV1(c *C) {
	const cgVersion = cgroup.V1
	s.testKillSnapProcessesSkippedErrors(c, cgVersion)
}

func (s *killSuite) TestKillSnapProcessesSkippedErrorsV2(c *C) {
	const cgVersion = cgroup.V2
	s.testKillSnapProcessesSkippedErrors(c, cgVersion)
}

func (s *killSuite) TestKillProcessesInCgroupForkingProcess(c *C) {
	cg := filepath.Join(s.rootDir, "/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope")
	c.Assert(os.MkdirAll(cg, 0755), IsNil)

	pid := 2
	procs := filepath.Join(cg, "cgroup.procs")
	c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0644), IsNil)

	restore := cgroup.MockSyscallKill(func(targetPid int, sig syscall.Signal) error {
		c.Assert(targetPid, Equals, pid)
		// Mock a new fork for next check
		if pid < 10 {
			pid++
			c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0644), IsNil)
		} else {
			c.Assert(os.WriteFile(procs, nil, 0644), IsNil)
		}
		return nil
	})
	defer restore()

	c.Assert(cgroup.KillProcessesInCgroup(cg), IsNil)
	c.Assert(pid, Equals, 10)
}

func (s *killSuite) TestKillProcessesInCgroupPidNotFound(c *C) {
	cg := filepath.Join(s.rootDir, "/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope")
	c.Assert(os.MkdirAll(cg, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte("1"), 0644), IsNil)

	var n int
	restore := cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		n++
		c.Assert(pid, Equals, 1)
		c.Assert(os.WriteFile(filepath.Join(cg, "cgroup.procs"), nil, 0644), IsNil)
		return syscall.ESRCH
	})
	defer restore()

	c.Assert(cgroup.KillProcessesInCgroup(cg), IsNil)
	c.Assert(n, Equals, 1)
}
