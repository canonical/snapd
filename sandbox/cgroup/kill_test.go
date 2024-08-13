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
	"fmt"
	"os"
	"path/filepath"
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

	ops []string
}

var _ = Suite(&killSuite{})

func (s *killSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	restore := cgroup.MockFreezePulseDelay(time.Nanosecond)
	s.AddCleanup(restore)

	restore = cgroup.MockFreezeSnapProcessesImplV1(func(ctx context.Context, snapName string) error {
		s.ops = append(s.ops, "freeze-snap-processes-v1:"+snapName)
		return nil
	})
	s.AddCleanup(restore)
	restore = cgroup.MockFreezeOneV2(func(ctx context.Context, dir string) error {
		s.ops = append(s.ops, "freeze-one-v2:"+filepath.Base(dir))
		return nil
	})
	s.AddCleanup(restore)

	restore = cgroup.MockThawSnapProcessesImplV1(func(snapName string) error {
		s.ops = append(s.ops, "thaw-snap-processes-v1:"+snapName)
		return nil
	})
	s.AddCleanup(restore)
	restore = cgroup.MockThawOneV2(func(dir string) error {
		s.ops = append(s.ops, "thaw-one-v2:"+filepath.Base(dir))
		return nil
	})
	s.AddCleanup(restore)

	restore = cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		s.ops = append(s.ops, fmt.Sprintf("kill-pid:%d, signal:%d", pid, sig))
		return nil
	})
	s.AddCleanup(restore)

	s.AddCleanup(s.clearOps)
}

func (s *killSuite) clearOps() {
	s.ops = nil
}

func (s *killSuite) TestKillSnapProcessesV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	snapName := "foo"
	cg := filepath.Join(cgroup.FreezerCgroupV1Dir(), fmt.Sprintf("snap.%s", snapName))
	procs := filepath.Join(cg, "cgroup.procs")

	c.Assert(os.MkdirAll(cg, 0755), IsNil)
	c.Assert(os.WriteFile(procs, nil, 0644), IsNil)

	// When no pids exist in cgroup.procs, do nothing
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), snapName), IsNil)
	c.Assert(s.ops, DeepEquals, []string{
		"freeze-snap-processes-v1:foo",
		"thaw-snap-processes-v1:foo",
	})
	// Clear logged ops for following checks
	s.clearOps()

	// Now mock running pids
	c.Assert(os.WriteFile(procs, []byte("3\n1\n2"), 0644), IsNil)
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), snapName), IsNil)
	c.Assert(s.ops, DeepEquals, []string{
		"freeze-snap-processes-v1:foo",
		"kill-pid:3, signal:9",
		"kill-pid:1, signal:9",
		"kill-pid:2, signal:9",
		"thaw-snap-processes-v1:foo",
	})
}

func (s *killSuite) testKillSnapProcessesV2(c *C, cgroupKillSupported bool) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	scopesToProcs := map[string]string{
		"snap.foo.app-1.1234-1234-1234.scope": "1\n2\n3",
		"snap.foo.app-1.no-pids.scope":        "",
		"snap.foo.app-2.some-scope.scope":     "9\n8\n7",
	}

	for scope, pids := range scopesToProcs {
		cg := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice", scope)
		procs := filepath.Join(cg, "cgroup.procs")
		c.Assert(os.MkdirAll(cg, 0755), IsNil)
		c.Assert(os.WriteFile(procs, []byte(pids), 0644), IsNil)
		if cgroupKillSupported {
			c.Assert(os.WriteFile(filepath.Join(cg, "cgroup.kill"), []byte(""), 0644), IsNil)
		}
	}

	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), IsNil)

	if cgroupKillSupported {
		for scope := range scopesToProcs {
			cgKill := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice", scope, "cgroup.kill")
			// "1" was written to cgroup.kill
			c.Assert(cgKill, testutil.FileEquals, "1")
		}
		// Didn't fallback to classic implementation
		c.Assert(s.ops, IsNil)
	} else {
		for scope := range scopesToProcs {
			cgKill := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice", scope, "cgroup.kill")
			c.Assert(cgKill, testutil.FileAbsent)
		}
		c.Assert(s.ops, DeepEquals, []string{
			// Kill first cgroup
			"freeze-one-v2:snap.foo.app-1.1234-1234-1234.scope",
			"kill-pid:1, signal:9",
			"kill-pid:2, signal:9",
			"kill-pid:3, signal:9",
			// No pids to kill in second cgroup
			"freeze-one-v2:snap.foo.app-1.no-pids.scope",
			// Kill third cgroup
			"freeze-one-v2:snap.foo.app-2.some-scope.scope",
			"kill-pid:9, signal:9",
			"kill-pid:8, signal:9",
			"kill-pid:7, signal:9",
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

func (s *killSuite) testKillSnapProcessesError(c *C, cgVersion int) {
	restore := cgroup.MockVersion(cgVersion, nil)
	defer restore()

	if cgVersion == cgroup.V1 {
		procs := filepath.Join(cgroup.FreezerCgroupV1Dir(), "snap.foo", "cgroup.procs")
		c.Assert(os.MkdirAll(filepath.Dir(procs), 0755), IsNil)
		c.Assert(os.WriteFile(procs, []byte("1\n2\n3\n4"), 0644), IsNil)
	} else {
		scopesToProcs := map[string]string{
			"snap.foo.app-1.1234-1234-1234.scope": "1",
			"snap.foo.app-1.no-pids.scope":        "2\n3",
			"snap.foo.app-2.some-scope.scope":     "4",
		}
		for scope, pids := range scopesToProcs {
			procs := filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/system.slice", scope, "cgroup.procs")
			c.Assert(os.MkdirAll(filepath.Dir(procs), 0755), IsNil)
			c.Assert(os.WriteFile(procs, []byte(pids), 0644), IsNil)
		}
	}

	restore = cgroup.MockSyscallKill(func(pid int, sig syscall.Signal) error {
		if pid == 1 || pid == 2 {
			return fmt.Errorf("mock error for pid %d", pid)
		}
		s.ops = append(s.ops, fmt.Sprintf("kill-pid:%d, signal:%d", pid, sig))
		return nil
	})
	defer restore()

	// Call failed and reported first error only
	c.Assert(cgroup.KillSnapProcesses(context.TODO(), "foo"), ErrorMatches, "mock error for pid 1")

	// But kept going
	if cgVersion == cgroup.V1 {
		c.Assert(s.ops, DeepEquals, []string{
			"freeze-snap-processes-v1:foo",
			"kill-pid:3, signal:9",
			"kill-pid:4, signal:9",
			"thaw-snap-processes-v1:foo",
		})
	} else {
		c.Assert(s.ops, DeepEquals, []string{
			// Kill first cgroup
			"freeze-one-v2:snap.foo.app-1.1234-1234-1234.scope",
			// Pid 1 not killed due to error
			"thaw-one-v2:snap.foo.app-1.1234-1234-1234.scope", // Thaw on error
			// Kill second cgroup
			"freeze-one-v2:snap.foo.app-1.no-pids.scope",
			// Pid 2 not killed due to error
			"kill-pid:3, signal:9",
			"thaw-one-v2:snap.foo.app-1.no-pids.scope", // Thaw on error
			// Kill third cgroup
			"freeze-one-v2:snap.foo.app-2.some-scope.scope",
			"kill-pid:4, signal:9",
		})
	}
}

func (s *killSuite) TestKillSnapProcessesV1Error(c *C) {
	const cgVersion = cgroup.V1
	s.testKillSnapProcessesError(c, cgVersion)
}

func (s *killSuite) TestKillSnapProcessesV2Error(c *C) {
	const cgVersion = cgroup.V2
	s.testKillSnapProcessesError(c, cgVersion)
}
