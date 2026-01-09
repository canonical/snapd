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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type memorySuiteBase struct {
	testutil.BaseTest

	rootDir         string
	mockCgroupsFile string
}

type memoryCgroupV1Suite struct {
	memorySuiteBase
}

type memoryCgroupV2Suite struct {
	memorySuiteBase
}

var _ = Suite(&memoryCgroupV1Suite{})
var _ = Suite(&memoryCgroupV2Suite{})

var (
	cgroupContentCommon = `#subsys_name	hierarchy	num_cgroups	enabled
cpuset	6	3	1
cpu	3	133	1
devices	10	135	1`
)

func (s *memorySuiteBase) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.AddCleanup(cgroup.MockVersion(cgroup.V1, nil))

	s.mockCgroupsFile = filepath.Join(s.rootDir, "/proc/cgroups")
	c.Assert(os.MkdirAll(filepath.Dir(s.mockCgroupsFile), 0o755), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(s.rootDir, "/sys/fs/cgroup"), 0o755), IsNil)
}

func (s *memoryCgroupV1Suite) SetUpTest(c *C) {
	s.memorySuiteBase.SetUpTest(c)

	s.AddCleanup(cgroup.MockVersion(cgroup.V1, nil))
}

func (s *memoryCgroupV1Suite) TestCheckMemoryCgroupV1_Happy(c *C) {
	extra := "memory	2	223	1"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, IsNil)
}

func (s *memoryCgroupV1Suite) TestCheckMemoryCgroupV1_Missing(c *C) {
	// note there is no file created for s.mockCgroupsFile

	err := cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cannot open cgroups file: open .*/cgroups: no such file or directory")
}

func (s *memoryCgroupV1Suite) TestCheckMemoryCgroupV1_NoMemoryEntry(c *C) {
	content := cgroupContentCommon

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cgroup memory controller is disabled on this system")
}

func (s *memoryCgroupV1Suite) TestCheckMemoryCgroupV1_InvalidMemoryEntry(c *C) {
	extra := "memory	invalid line"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, `cannot parse cgroups file: invalid line "memory\\tinvalid line"`)
}

func (s *memoryCgroupV1Suite) TestCheckMemoryCgroupV1_Disabled(c *C) {
	extra := "memory	2	223	0"
	content := cgroupContentCommon + "\n" + extra

	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)
	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cgroup memory controller is disabled on this system")
}

func (s *memoryCgroupV2Suite) SetUpTest(c *C) {
	s.memorySuiteBase.SetUpTest(c)

	s.AddCleanup(cgroup.MockVersion(cgroup.V2, nil))
}

func (s *memoryCgroupV2Suite) TestCheckMemoryCgroupV2_Disabled(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()

	content := cgroupContentCommon + "\n"

	// memory not mentioned in /proc/cgroups at all (like on 6.12+ kernels)
	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)

	v2ControllersFile := filepath.Join(s.rootDir, "/sys/fs/cgroup/cgroup.controllers")
	// no memory
	c.Assert(os.WriteFile(v2ControllersFile, []byte("foo bar baz other\n"), 0o644), IsNil)

	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, ErrorMatches, "cgroup memory controller is disabled on this system")
}

func (s *memoryCgroupV2Suite) TestCheckMemoryCgroupV2_Happy(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()

	content := cgroupContentCommon + "\n"

	// memory not mentioned in /proc/cgroups at all (like on 6.12+ kernels)
	err := os.WriteFile(s.mockCgroupsFile, []byte(content), 0o644)
	c.Assert(err, IsNil)

	v2ControllersFile := filepath.Join(s.rootDir, "/sys/fs/cgroup/cgroup.controllers")
	c.Assert(os.WriteFile(v2ControllersFile, []byte("foo bar baz memory other\n"), 0o644), IsNil)

	err = cgroup.CheckMemoryCgroup()
	c.Assert(err, IsNil)
}
