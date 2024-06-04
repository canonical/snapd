// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type memorySuite struct {
	testutil.BaseTest

	mockCgroupsFile string
}

var _ = Suite(&memorySuite{})

var (
	cgroupContentCommon = `#subsys_name	hierarchy	num_cgroups	enabled
cpuset	6	3	1
cpu	3	133	1
devices	10	135	1`
)

func (s *memorySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.mockCgroupsFile = filepath.Join(c.MkDir(), "cgroups")
	s.AddCleanup(cgroup.MockCgroupsFilePath(s.mockCgroupsFile))
}

func (s *memorySuite) TestCheckMemoryCgroupHappy(c *C) {
	extra := "memory	2	223	1"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, IsNil)
}

func (s *memorySuite) TestCheckMemoryCgroupMissing(c *C) {
	// note there is no file created for s.mockCgroupsFile

	err := cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cannot open cgroups file: open .*/cgroups: no such file or directory")
}

func (s *memorySuite) TestCheckMemoryCgroupNoMemoryEntry(c *C) {
	content := cgroupContentCommon

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cannot find memory cgroup in .*/cgroups")
}

func (s *memorySuite) TestCheckMemoryCgroupInvalidMemoryEntry(c *C) {
	extra := "memory	invalid line"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, `cannot parse cgroups file: invalid line "memory\\tinvalid line"`)
}

func (s *memorySuite) TestCheckMemoryCgroupDisabled(c *C) {
	extra := "memory	2	223	0"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "memory cgroup is disabled on this system")
}
