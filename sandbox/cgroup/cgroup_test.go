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
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

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
	s.AddCleanup(cgroup.MockFsRootPath(s.rootDir))
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
	c.Assert(err, ErrorMatches, "cannot determine filesystem type: statfs fail")
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

func (s *cgroupSuite) TestControllerPathV1(c *C) {
	c.Assert(cgroup.ControllerPathV1("freezer"), Equals, filepath.Join(s.rootDir, "/sys/fs/cgroup/freezer"))
	c.Assert(cgroup.ControllerPathV1("memory"), Equals, filepath.Join(s.rootDir, "/sys/fs/cgroup/memory"))
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
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), mockCgroup, 0755)
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
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), noFreezerCgroup, 0755)
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
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), mockCgroupConfusingCpu, 0755)
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

func (s *cgroupSuite) TestPidsInFileBadInput(c *C) {
	err := os.MkdirAll(s.rootDir, 0755)
	c.Assert(err, IsNil)
	gPids := []byte(`123
zebra
567
`)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "pids"), gPids, 0755)
	c.Assert(err, IsNil)

	pids, err := cgroup.PidsInFile(filepath.Join(s.rootDir, "pids"))
	c.Assert(err, ErrorMatches, `cannot parse pid "zebra"`)
	c.Assert(pids, IsNil)
}

func (s *cgroupSuite) TestParsePid(c *C) {
	pid, err := cgroup.ParsePid("10")
	c.Assert(err, IsNil)
	c.Check(pid, Equals, 10)

	_, err = cgroup.ParsePid("")
	c.Assert(err, ErrorMatches, `cannot parse pid ""`)

	_, err = cgroup.ParsePid("-1")
	c.Assert(err, ErrorMatches, `cannot parse pid "-1"`)
}

func (s *cgroupSuite) TestSecurityTagFromCgroupPath(c *C) {
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.foo.service"), Equals, "snap.foo.foo")
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.bar.service"), Equals, "snap.foo.bar")
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.$RANDOM.foo.bar.scope"), Equals, "snap.foo.bar")
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.$RANDOM.foo.hook.bar.scope"), Equals, "snap.foo.hook.bar")
	// We are not confused by snapd things.
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.service"), Equals, "")
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snapd.service"), Equals, "")
	// Real data looks like this.
	c.Check(cgroup.SecurityTagFromCgroupPath("snap.d854bd35-2457-4ac8-b494-06061d74df33.test-snapd-refresh.sh.scope"), Equals, "snap.test-snapd-refresh.sh")
	// Trailing slashes are automatically handled.
	c.Check(cgroup.SecurityTagFromCgroupPath("/a/b/snap.foo.foo.service/"), Equals, "snap.foo.foo")
}

func (s *cgroupSuite) writePids(c *C, dir string, pids []int) {
	var buf bytes.Buffer
	for _, pid := range pids {
		fmt.Fprintf(&buf, "%d\n", pid)
	}

	// The path looks like v1 path but for the purpose of tests it works
	// in both v1 and v2 modes.
	path := filepath.Join(s.rootDir, "/sys/fs/cgroup/systemd", dir)
	err := os.MkdirAll(path, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(path, "cgroup.procs"), buf.Bytes(), 0644)
	c.Assert(err, IsNil)
}

func (s *cgroupSuite) TestPidsOfSnapEmpty(c *C) {
	// Not having any cgroup directories is not an error.
	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, HasLen, 0)
}

func (s *cgroupSuite) TestPidsOfSnapUnrelatedStuff(c *C) {
	// Things that are not related to the snap are not being picked up.
	s.writePids(c, "udisks2.service", []int{100})
	s.writePids(c, "snap..service", []int{101})
	s.writePids(c, "snap..scope", []int{102})
	s.writePids(c, "snap.*.service", []int{103})
	s.writePids(c, "snap.*.scope", []int{104})

	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, HasLen, 0)
}

func (s *cgroupSuite) TestPidsOfSnapSecurityTags(c *C) {
	// Pids are collected and assigned to bins by security tag
	s.writePids(c, "system.slice/snap.$RANDOM.pkg.hook.configure.scope", []int{1})
	s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{2})

	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg.hook.configure": {1},
		"snap.pkg.daemon":         {2},
	})
}

func (s *cgroupSuite) TestPidsOfInstances(c *C) {
	// Instances are not confused between themselves and between the non-instance version.
	s.writePids(c, "system.slice/snap.pkg_prod.daemon.service", []int{1})
	s.writePids(c, "system.slice/snap.pkg_devel.daemon.service", []int{2})
	s.writePids(c, "system.slice/snap.pkg.daemon.service", []int{3})

	// The main one
	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg.daemon": {3},
	})

	// The development one
	pids, err = cgroup.PidsOfSnap("pkg_devel")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg_devel.daemon": {2},
	})

	// The production one
	pids, err = cgroup.PidsOfSnap("pkg_prod")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg_prod.daemon": {1},
	})
}

func (s *cgroupSuite) TestPidsOfAggregation(c *C) {
	// A single snap may be invoked by multiple users in different sessions.
	// All of their PIDs are collected though.
	s.writePids(c, "user.slice/user-1000.slice/user@1000.service/gnome-shell-wayland.service/snap.$RANDOM1.pkg.app.scope", []int{1})
	s.writePids(c, "user.slice/user-1001.slice/user@1001.service/gnome-shell-wayland.service/snap.$RANDOM2.pkg.app.scope", []int{2})

	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg.app": {1, 2},
	})
}

func (s *cgroupSuite) TestPidsOfUnrelated(c *C) {
	// We are not confusing snaps with other snaps and with non-snap hierarchies.
	s.writePids(c, "user.slice/.../snap.$RANDOM1.pkg.app.scope", []int{1})
	s.writePids(c, "user.slice/.../snap.$RANDOM2.other.snap.scope", []int{2})
	s.writePids(c, "user.slice/.../pkg.service", []int{3})

	pids, err := cgroup.PidsOfSnap("pkg")
	c.Assert(err, IsNil)
	c.Check(pids, DeepEquals, map[string][]int{
		"snap.pkg.app": {1},
	})
}
