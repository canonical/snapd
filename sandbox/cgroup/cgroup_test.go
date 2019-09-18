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
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type cgroupSuite struct{}

var _ = Suite(&cgroupSuite{})

func TestCgroup(t *testing.T) { TestingT(t) }

func (s *cgroupSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
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
		c.Assert(p, Equals, filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup"))
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
		c.Assert(p, Equals, filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup"))
		return TMPFS_MAGIC, nil
	})
	defer restore()
	v, err := cgroup.ProbeCgroupVersion()
	c.Assert(err, IsNil)
	c.Assert(v, Equals, cgroup.V1)
}

func (s *cgroupSuite) TestProbeVersionUnhappy(c *C) {
	restore := cgroup.MockFsTypeForPath(func(p string) (int64, error) {
		c.Assert(p, Equals, filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup"))
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
	c.Assert(cgroup.ProcPidPath(1), Equals, filepath.Join(dirs.GlobalRootDir, "/proc/1/cgroup"))
	c.Assert(cgroup.ProcPidPath(1234), Equals, filepath.Join(dirs.GlobalRootDir, "/proc/1234/cgroup"))
}

func (s *cgroupSuite) TestControllerPathV1(c *C) {
	c.Assert(cgroup.ControllerPathV1("freezer"), Equals, filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/freezer"))
	c.Assert(cgroup.ControllerPathV1("memory"), Equals, filepath.Join(dirs.GlobalRootDir, "/sys/fs/cgroup/memory"))
}
