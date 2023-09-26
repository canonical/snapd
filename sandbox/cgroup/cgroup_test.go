// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"errors"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type cgroupSuite struct {
	testutil.BaseTest
	rootDir string
}

var _ = Suite(&cgroupSuite{})

func TestCgroup(t *testing.T) { TestingT(t) }

func (s *cgroupSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *cgroupSuite) TestIsUnified(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()
	c.Assert(cgroup.IsUnified(), Equals, true)

	restore = cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	c.Assert(cgroup.IsUnified(), Equals, false)

	restore = cgroup.MockVersion(cgroup.Unknown, nil)
	defer restore()
	c.Assert(cgroup.IsUnified(), Equals, false)
}

func (s *cgroupSuite) TestProbeVersion2(c *C) {
	restore := cgroup.MockFsTypeForPath(func(p string) (int64, error) {
		c.Assert(p, Equals, filepath.Join(s.rootDir, "/sys/fs/cgroup"))
		return int64(cgroup.Cgroup2SuperMagic), nil
	})
	defer restore()
	v, err := cgroup.ProbeCgroupVersion()
	c.Assert(err, IsNil)
	c.Assert(v, Equals, cgroup.V2)
}

func (s *cgroupSuite) TestProbeVersion1(c *C) {
	const TMPFS_MAGIC = 0x1021994
	restore := cgroup.MockFsTypeForPath(func(p string) (int64, error) {
		c.Assert(p, Equals, filepath.Join(s.rootDir, "/sys/fs/cgroup"))
		return TMPFS_MAGIC, nil
	})
	defer restore()
	v, err := cgroup.ProbeCgroupVersion()
	c.Assert(err, IsNil)
	c.Assert(v, Equals, cgroup.V1)
}

func (s *cgroupSuite) TestProbeVersionUnhappy(c *C) {
	restore := cgroup.MockFsTypeForPath(func(p string) (int64, error) {
		c.Assert(p, Equals, filepath.Join(s.rootDir, "/sys/fs/cgroup"))
		return 0, errors.New("statfs fail")
	})
	defer restore()
	v, err := cgroup.ProbeCgroupVersion()
	c.Assert(err, ErrorMatches, "cannot determine cgroup version: statfs fail")
	c.Assert(v, Equals, cgroup.Unknown)
}

func (s *cgroupSuite) TestVersion(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()
	v, err := cgroup.Version()
	c.Assert(v, Equals, cgroup.V2)
	c.Assert(err, IsNil)

	restore = cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	v, err = cgroup.Version()
	c.Assert(v, Equals, cgroup.V1)
	c.Assert(err, IsNil)

	restore = cgroup.MockVersion(cgroup.Unknown, nil)
	defer restore()
	v, err = cgroup.Version()
	c.Assert(v, Equals, cgroup.Unknown)
	c.Assert(err, IsNil)

	restore = cgroup.MockVersion(cgroup.Unknown, errors.New("foo"))
	defer restore()
	v, err = cgroup.Version()
	c.Assert(v, Equals, cgroup.Unknown)
	c.Assert(err, ErrorMatches, "foo")
}

func (s *cgroupSuite) TestProcPidPath(c *C) {
	c.Assert(cgroup.ProcPidPath(1), Equals, filepath.Join(s.rootDir, "/proc/1/cgroup"))
	c.Assert(cgroup.ProcPidPath(1234), Equals, filepath.Join(s.rootDir, "/proc/1234/cgroup"))
}

var mockCgroup = []byte(`
10:devices:/user.slice
9:cpuset:/
8:net_cls,net_prio:/
7:freezer:/snap.hello-world
6:perf_event:/
5:pids:/user.slice/user-1000.slice/user@1000.service
4:cpu,cpuacct:/
3:memory:/memory/group
2:blkio:/
1:name=systemd:/user.slice/user-1000.slice/user@1000.service/gnome-terminal-server.service
0:foo:/illegal/unified/entry
0::/systemd/unified
11:name=snapd:/snap.foo.bar
`)

func (s *cgroupSuite) TestProgGroupHappy(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), mockCgroup, 0755)
	c.Assert(err, IsNil)

	group, err := cgroup.ProcGroup(333, cgroup.MatchV1Controller("freezer"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/snap.hello-world")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1Controller("memory"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/memory/group")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1NamedHierarchy("systemd"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/user.slice/user-1000.slice/user@1000.service/gnome-terminal-server.service")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1NamedHierarchy("snapd"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/snap.foo.bar")

	group, err = cgroup.ProcGroup(333, cgroup.MatchUnifiedHierarchy())
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/systemd/unified")
}

func (s *cgroupSuite) TestProgGroupMissingFile(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)

	group, err := cgroup.ProcGroup(333, cgroup.MatchV1Controller("freezer"))
	c.Assert(err, ErrorMatches, "open .*/proc/333/cgroup: no such file or directory")
	c.Check(group, Equals, "")
}

func (s *cgroupSuite) TestProgGroupMissingGroup(c *C) {
	var noFreezerCgroup = []byte(`
10:devices:/user.slice
`)

	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), noFreezerCgroup, 0755)
	c.Assert(err, IsNil)

	group, err := cgroup.ProcGroup(333, cgroup.MatchV1Controller("freezer"))
	c.Assert(err, ErrorMatches, `cannot find controller "freezer" cgroup path for pid 333`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.MatchUnifiedHierarchy())
	c.Assert(err, ErrorMatches, `cannot find unified hierarchy cgroup path for pid 333`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1NamedHierarchy("snapd"))
	c.Assert(err, ErrorMatches, `cannot find named hierarchy "snapd" cgroup path for pid 333`)
	c.Check(group, Equals, "")
}

var mockCgroupConfusingCpu = []byte(`
8:cpuacct:/foo.cpuacct
7:cpuset,cpu,cpuacct:/foo.many-cpu
`)

func (s *cgroupSuite) TestProgGroupConfusingCpu(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), mockCgroupConfusingCpu, 0755)
	c.Assert(err, IsNil)

	group, err := cgroup.ProcGroup(333, cgroup.MatchV1Controller("cpu"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.many-cpu")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1Controller("cpuacct"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.cpuacct")

	group, err = cgroup.ProcGroup(333, cgroup.MatchV1Controller("cpuset"))
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.many-cpu")
}

func (s *cgroupSuite) TestProgGroupBadSelector(c *C) {
	group, err := cgroup.ProcGroup(333, nil)
	c.Assert(err, ErrorMatches, `internal error: cgroup matcher is nil`)
	c.Check(group, Equals, "")
}

func (s *cgroupSuite) TestProcessPathInTrackingCgroup(c *C) {
	const noise = `12:cpuset:/
11:rdma:/
10:blkio:/
9:freezer:/
8:cpu,cpuacct:/
7:perf_event:/
6:net_cls,net_prio:/
5:devices:/user.slice
4:hugetlb:/
3:memory:/user.slice/user-1000.slice/user@1000.service
2:pids:/user.slice/user-1000.slice/user@1000.service
`

	d := c.MkDir()
	defer dirs.SetRootDir(dirs.GlobalRootDir)
	dirs.SetRootDir(d)

	f := filepath.Join(d, "proc", "1234", "cgroup")
	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)

	for _, scenario := range []struct {
		cgVersion             int
		cgroups, path, errMsg string
	}{
		{cgVersion: cgroup.V2, cgroups: "", path: "", errMsg: "cannot find tracking cgroup"},
		{cgVersion: cgroup.V2, cgroups: noise + "", path: "", errMsg: "cannot find tracking cgroup"},
		{cgVersion: cgroup.V2, cgroups: noise + "0::/foo", path: "/foo"},
		{cgVersion: cgroup.V2, cgroups: noise + "1:name=systemd:/bar", errMsg: "cannot find tracking cgroup"},
		// If only V1 is mounted, then the same configuration works
		{cgVersion: cgroup.V1, cgroups: noise + "1:name=systemd:/bar", path: "/bar"},
		// First match wins (normally they are in sync).
		{cgVersion: cgroup.V1, cgroups: noise + "1:name=systemd:/bar\n0::/foo", path: "/bar"},
		{cgVersion: cgroup.V2, cgroups: noise + "1:name=systemd:/bar\n0::/foo", path: "/foo"},
		{cgVersion: cgroup.V2, cgroups: "0::/tricky:path", path: "/tricky:path"},
		{cgVersion: cgroup.V2, cgroups: "1:ctrl" /* no path */, errMsg: `cannot parse proc cgroup entry ".*": expected three fields`},
		{cgVersion: cgroup.V2, cgroups: "potato:foo:/bar" /* bad ID number */, errMsg: `cannot parse proc cgroup entry ".*": cannot parse cgroup id "potato"`},
	} {
		restoreCGVersion := cgroup.MockVersion(scenario.cgVersion, nil)

		c.Assert(os.WriteFile(f, []byte(scenario.cgroups), 0644), IsNil)
		path, err := cgroup.ProcessPathInTrackingCgroup(1234)
		if scenario.errMsg != "" {
			c.Assert(err, ErrorMatches, scenario.errMsg)
		} else {
			c.Assert(path, Equals, scenario.path)
			c.Assert(err, IsNil)
		}

		restoreCGVersion()
	}
}

func (s *cgroupSuite) TestProcessPathInTrackingCgroupV2SpecialCase(c *C) {
	const text = `0::/
1:name=systemd:/user.slice/user-0.slice/session-1.scope
`
	d := c.MkDir()
	defer dirs.SetRootDir(dirs.GlobalRootDir)
	dirs.SetRootDir(d)

	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	f := filepath.Join(d, "proc", "1234", "cgroup")
	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)

	c.Assert(os.WriteFile(f, []byte(text), 0644), IsNil)
	path, err := cgroup.ProcessPathInTrackingCgroup(1234)
	c.Assert(err, IsNil)
	// Because v2 is not really mounted, we ignore the entry 0::/
	// and return the v1 version instead.
	c.Assert(path, Equals, "/user.slice/user-0.slice/session-1.scope")
}
