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
	"time"

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

	s.AddCleanup(cgroup.MockKillThawCooldown(1 * time.Nanosecond))
}

func mockCgroupsWithProcs(c *C, cgroupsToProcs map[string][]string) {
	for cg, pids := range cgroupsToProcs {
		procs := filepath.Join(dirs.GlobalRootDir, cg, "cgroup.procs")
		c.Assert(os.MkdirAll(filepath.Dir(procs), 0o755), IsNil)
		c.Assert(os.WriteFile(procs, []byte(strings.Join(pids, "\n")), 0o644), IsNil)
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
	// Replace with syscallKill mock
	restore = cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		c.Assert(sig, Equals, syscall.SIGKILL)

		// simulate killing pid
		cgroupsToProcs = removePid(cgroupsToProcs, pid)
		mockCgroupsWithProcs(c, cgroupsToProcs)

		ops = append(ops, fmt.Sprintf("kill-pid:%d, signal:%d", pid, sig))
		return nil
	})
	defer restore()
	restore = cgroup.MockFreezeSnapProcessesImplV1(func(ctx context.Context, snapName string) error {
		ops = append(ops, "freeze-snap-processes-v1:"+snapName)
		return nil
	})
	defer restore()
	restore = cgroup.MockThawSnapProcessesImplV1(func(snapName string) error {
		ops = append(ops, "thaw-snap-processes-v1:"+snapName)
		return nil
	})
	defer restore()

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)
	c.Assert(ops, DeepEquals, []string{
		// freeze/kill/thaw for snap.foo.app-1.1234-1234-1234.scope
		"freeze-snap-processes-v1:foo",
		"kill-pid:1, signal:9",
		"thaw-snap-processes-v1:foo",
		// freeze/kill/thaw for snap.foo.app-1.9876.scope
		"freeze-snap-processes-v1:foo",
		"kill-pid:2, signal:9",
		"kill-pid:3, signal:9",
		"thaw-snap-processes-v1:foo",
		// freeze/kill/thaw for snap.foo.app-2.some-scope.scope
		"freeze-snap-processes-v1:foo",
		"kill-pid:4, signal:9",
		"thaw-snap-processes-v1:foo",
		// no more pids left for freezer cgroup
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

func (s *killSuite) testKillSnapProcessesV2(c *C, cgroupKillSupported, pidsControllerMounted bool) {
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

	for cg := range cgroupsToProcs {
		if cgroupKillSupported {
			c.Assert(os.WriteFile(filepath.Join(s.rootDir, cg, "cgroup.kill"), []byte(""), 0o644), IsNil)
		}
		if pidsControllerMounted {
			c.Assert(os.WriteFile(filepath.Join(s.rootDir, cg, "pids.max"), []byte(""), 0o644), IsNil)
		}
	}

	var ops []string
	// Replace with syscallKill mock
	restore = cgroup.MockKillProcessesInCgroup(func(ctx context.Context, dir string, freeze func(ctx context.Context), thaw func()) error {
		c.Assert(freeze, IsNil)
		c.Assert(thaw, IsNil)
		// trim tmp root dir
		dir = strings.TrimPrefix(dir, s.rootDir)
		ops = append(ops, fmt.Sprintf("kill cgroup: %s", dir))
		return nil
	})
	defer restore()

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)

	for cg := range cgroupsToProcs {
		if cgroupKillSupported {
			cgKill := filepath.Join(s.rootDir, cg, "cgroup.kill")
			if strings.HasSuffix(cg, "snap.bar.app-1.1234-1234-1234.scope") {
				c.Assert(cgKill, testutil.FileEquals, "")
			} else {
				// "1" was written to cgroup.kill
				c.Assert(cgKill, testutil.FileEquals, "1")
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
		if pidsControllerMounted {
			pidsMax := filepath.Join(s.rootDir, cg, "pids.max")
			if strings.HasSuffix(cg, "snap.bar.app-1.1234-1234-1234.scope") || cgroupKillSupported {
				// if cgroup.kill exists, fallback code is not hit
				c.Assert(pidsMax, testutil.FileEquals, "")
			} else {
				// "0" was written to pids.max
				c.Assert(pidsMax, testutil.FileEquals, "0")
			}
		} else {
			pidsMax := filepath.Join(s.rootDir, cg, "pids.max")
			c.Assert(pidsMax, testutil.FileAbsent)
		}
	}
}

func (s *killSuite) TestKillSnapProcessesV2(c *C) {
	const cgroupKillSupported = false
	const pidsControllerMounted = true
	s.testKillSnapProcessesV2(c, cgroupKillSupported, pidsControllerMounted)
}

func (s *killSuite) TestKillSnapProcessesV2NoPidController(c *C) {
	const cgroupKillSupported = false
	const pidsControllerMounted = false
	s.testKillSnapProcessesV2(c, cgroupKillSupported, pidsControllerMounted)
}

func (s *killSuite) TestKillSnapProcessesV2CgKillSupported(c *C) {
	// cgroup.kill requires linux 5.14+
	const cgroupKillSupported = true
	const pidsControllerMounted = true
	s.testKillSnapProcessesV2(c, cgroupKillSupported, pidsControllerMounted)
}

func (s *killSuite) TestKillSnapProcessesV2CgKillSupportedNoPidControler(c *C) {
	// cgroup.kill requires linux 5.14+
	const cgroupKillSupported = true
	const pidsControllerMounted = false
	s.testKillSnapProcessesV2(c, cgroupKillSupported, pidsControllerMounted)
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
	restore = cgroup.MockFreezeSnapProcessesImplV1(func(ctx context.Context, snapName string) error {
		ops = append(ops, "freeze-snap-processes-v1:"+snapName)
		return nil
	})
	defer restore()
	restore = cgroup.MockThawSnapProcessesImplV1(func(snapName string) error {
		ops = append(ops, "thaw-snap-processes-v1:"+snapName)
		return nil
	})
	defer restore()

	// Call failed and reported first error only
	err := cgroup.KillSnapProcesses(context.TODO(), "foo")
	c.Assert(err, ErrorMatches, "mock error for pid 1")

	// But kept going
	if cgVersion == cgroup.V1 {
		if freezerOnly {
			c.Assert(ops, DeepEquals, []string{
				"freeze-snap-processes-v1:foo",
				// Pid 1 not killed due to error
				// Pid 2 not killed due to error
				"kill-pid:3, signal:9",
				"kill-pid:4, signal:9",
				"thaw-snap-processes-v1:foo",
			})
		} else {
			c.Assert(ops, DeepEquals, []string{
				// freeze/kill/thaw for snap.foo.app-1.1234-1234-1234.scope
				"freeze-snap-processes-v1:foo",
				// Pid 1 not killed due to error
				"thaw-snap-processes-v1:foo",
				// freeze/kill/thaw for snap.foo.app-1.9876.scope
				"freeze-snap-processes-v1:foo",
				// Pid 2 not killed due to error
				"kill-pid:3, signal:9",
				"thaw-snap-processes-v1:foo",
				// freeze/kill/thaw for snap.foo.app-2.some-scope.scope
				"freeze-snap-processes-v1:foo",
				"kill-pid:4, signal:9",
				"thaw-snap-processes-v1:foo",
				// freeze/kill/that for freezer cgroup since pids 1 and 2 are still not killed
				"freeze-snap-processes-v1:foo",
				"thaw-snap-processes-v1:foo",
			})
		}
	} else {
		c.Assert(ops, DeepEquals, []string{
			// Pid 1 not killed due to error
			// Pid 2 not killed due to error
			"kill-pid:3, signal:9",
			"kill-pid:4, signal:9",
		})
	}
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
	restore = cgroup.MockKillProcessesInCgroup(func(ctx context.Context, dir string, freeze func(ctx context.Context), thaw func()) error {
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
	c.Assert(os.MkdirAll(cg, 0o755), IsNil)

	pid := 2
	procs := filepath.Join(cg, "cgroup.procs")
	c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0o644), IsNil)

	var ops []string
	restore := cgroup.MockSyscallKill(func(targetPid int, sig syscall.Signal) error {
		c.Assert(targetPid, Equals, pid)
		ops = append(ops, fmt.Sprintf("kill-pid:%d, signal:%d", pid, sig))
		// Mock a new fork for next check
		if pid < 4 {
			pid++
			c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0o644), IsNil)
		} else {
			c.Assert(os.WriteFile(procs, nil, 0o644), IsNil)
		}
		return nil
	})
	defer restore()

	mockFreeze := func(ctx context.Context) { ops = append(ops, "freeze") }
	mockThaw := func() { ops = append(ops, "thaw") }

	c.Assert(cgroup.KillProcessesInCgroup(context.TODO(), cg, mockFreeze, mockThaw), IsNil)
	c.Assert(pid, Equals, 4)
	c.Assert(ops, DeepEquals, []string{
		// Pid 2
		"freeze",
		"kill-pid:2, signal:9",
		"thaw",
		// Pid 3
		"freeze",
		"kill-pid:3, signal:9",
		"thaw",
		// Pid 4
		"freeze",
		"kill-pid:4, signal:9",
		"thaw",
	})
}

func (s *killSuite) TestKillProcessesInCgroupPidNotFound(c *C) {
	cg := filepath.Join(s.rootDir, "/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope")
	c.Assert(os.MkdirAll(cg, 0o755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(cg, "cgroup.procs"), []byte("1"), 0o644), IsNil)

	var n int
	restore := cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		n++
		c.Assert(pid, Equals, 1)
		c.Assert(os.WriteFile(filepath.Join(cg, "cgroup.procs"), nil, 0o644), IsNil)
		return syscall.ESRCH
	})
	defer restore()

	var freezeCalled, thawCalled int
	mockFreeze := func(ctx context.Context) { freezeCalled++ }
	mockThaw := func() { thawCalled++ }

	c.Assert(cgroup.KillProcessesInCgroup(context.TODO(), cg, mockFreeze, mockThaw), IsNil)
	c.Assert(n, Equals, 1)
	c.Assert(freezeCalled, Equals, 1)
	c.Assert(thawCalled, Equals, 1)
}

func (s *killSuite) testKillProcessInCgroupTimeout(c *C, cgVersion int) {
	restore := cgroup.MockVersion(cgVersion, nil)
	defer restore()

	cg := filepath.Join(s.rootDir, "/sys/fs/cgroup/user.slice/user-1001.slice/user@1001.service/app.slice/snap.foo.app-1.1234-1234-1234.scope")
	c.Assert(os.MkdirAll(cg, 0o755), IsNil)

	pid := 2
	procs := filepath.Join(cg, "cgroup.procs")
	c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0o644), IsNil)

	restore = cgroup.MockSyscallKill(func(targetPid int, sig syscall.Signal) error {
		c.Assert(targetPid, Equals, pid)
		// Mock a new fork for next check
		pid++
		c.Assert(os.WriteFile(procs, []byte(strconv.Itoa(pid)), 0o644), IsNil)
		// We should timeout after first check
		time.Sleep(50 * time.Millisecond)
		return nil
	})
	defer restore()

	restore = cgroup.MockMaxKillTimeout(10 * time.Millisecond)
	defer restore()

	err := cgroup.KillSnapProcesses(context.TODO(), "foo")
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot kill processes in cgroup %q: context deadline exceeded", cg))
	c.Assert(pid, Equals, 3)
}

func (s *killSuite) TestKillProcessInCgroupTimeoutV1(c *C) {
	const cgVersion = cgroup.V1
	s.testKillProcessInCgroupTimeout(c, cgVersion)
}

func (s *killSuite) TestKillProcessInCgroupTimeoutV2(c *C) {
	const cgVersion = cgroup.V2
	s.testKillProcessInCgroupTimeout(c, cgVersion)
}
