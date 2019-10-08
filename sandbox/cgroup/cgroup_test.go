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

	group, err := cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "freezer"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/snap.hello-world")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "memory"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/memory/group")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Name: "systemd"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/user.slice/user-1000.slice/user@1000.service/gnome-terminal-server.service")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Name: "snapd"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/snap.foo.bar")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Unified: true})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/systemd/unified")
}

func (s *cgroupSuite) TestProgGroupMissingFile(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)

	group, err := cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "freezer"})
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

	group, err := cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "freezer"})
	c.Assert(err, ErrorMatches, `cannot find controller "freezer" cgroup path for pid 333`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Unified: true})
	c.Assert(err, ErrorMatches, `cannot find unified hierarchy cgroup path for pid 333`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Name: "snapd"})
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

	group, err := cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "cpu"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.many-cpu")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "cpuacct"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.cpuacct")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "cpuset"})
	c.Assert(err, IsNil)
	c.Check(group, Equals, "/foo.many-cpu")
}

func (s *cgroupSuite) TestProgGroupBadSelector(c *C) {
	group, err := cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "cpu", Name: "foo"})
	c.Assert(err, ErrorMatches, `invalid group selector: named hierarchy "foo" with a controller "cpu"`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Unified: true, Name: "foo"})
	c.Assert(err, ErrorMatches, `invalid group selector: named hierarchy "foo" with a unified one`)
	c.Check(group, Equals, "")

	group, err = cgroup.ProcGroup(333, cgroup.GroupSelector{Controller: "cpu", Name: "foo"})
	c.Assert(err, ErrorMatches, `invalid group selector: named hierarchy "foo" with a controller "cpu"`)
	c.Check(group, Equals, "")
}
