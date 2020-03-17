// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
)

func (s *cgroupSuite) TestSnapNameFromPidHappy(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), mockCgroup, 0755)
	c.Assert(err, IsNil)

	name, err := cgroup.SnapNameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "hello-world")
}

func (s *cgroupSuite) TestSnapNameFromPidUnhappy(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), []byte("1:freezer:/\n"), 0755)
	c.Assert(err, IsNil)

	name, err := cgroup.SnapNameFromPid(333)
	c.Assert(err, ErrorMatches, "cannot find a snap for pid 333")
	c.Check(name, Equals, "")
}

func (s *cgroupSuite) TestSnapNameFromPidEmptyName(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "proc/333"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "proc/333/cgroup"), []byte("1:freezer:/snap./\n"), 0755)
	c.Assert(err, IsNil)

	name, err := cgroup.SnapNameFromPid(333)
	c.Assert(err, ErrorMatches, "snap name in cgroup path is empty")
	c.Check(name, Equals, "")
}

func (s *cgroupSuite) TestSnapNameFromPidCgroupV2(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	name, err := cgroup.SnapNameFromPid(333)
	c.Assert(err, ErrorMatches, "not supported")
	c.Check(name, Equals, "")
}
