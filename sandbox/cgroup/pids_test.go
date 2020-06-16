// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

type pidsSuite struct{}

var _ = Suite(&pidsSuite{})

func (s *pidsSuite) TestParsePid(c *C) {
	pid, err := cgroup.ParsePid("10")
	c.Assert(err, IsNil)
	c.Check(pid, Equals, 10)
	_, err = cgroup.ParsePid("")
	c.Assert(err, ErrorMatches, `cannot parse pid ""`)
	_, err = cgroup.ParsePid("-1")
	c.Assert(err, ErrorMatches, `cannot parse pid "-1"`)
	_, err = cgroup.ParsePid("foo")
	c.Assert(err, ErrorMatches, `cannot parse pid "foo"`)
	_, err = cgroup.ParsePid("12\x0034")
	c.Assert(err.Error(), Equals, "cannot parse pid \"12\\x0034\"")
	_, err = cgroup.ParsePid("ł")
	c.Assert(err, ErrorMatches, `cannot parse pid "ł"`)
	_, err = cgroup.ParsePid("1000000000000000000000000000000000000000000000")
	c.Assert(err, ErrorMatches, `cannot parse pid "1000000000000000000000000000000000000000000000"`)
}

func (s *cgroupSuite) TestPidsHappy(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "group1/group2"), 0755)
	c.Assert(err, IsNil)
	g2Pids := []byte(`123
234
567
`)
	allPids := append(g2Pids, []byte(`999
`)...)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "group1/cgroup.procs"), allPids, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "group1/group2/cgroup.procs"), g2Pids, 0755)
	c.Assert(err, IsNil)

	pids, err := cgroup.PidsInGroup(s.rootDir, "group1")
	c.Assert(err, IsNil)
	c.Assert(pids, DeepEquals, []int{123, 234, 567, 999})

	pids, err = cgroup.PidsInGroup(s.rootDir, "group1/group2")
	c.Assert(err, IsNil)
	c.Assert(pids, DeepEquals, []int{123, 234, 567})

	pids, err = cgroup.PidsInGroup(s.rootDir, "group.does.not.exist")
	c.Assert(err, IsNil)
	c.Assert(pids, IsNil)
}

func (s *cgroupSuite) TestPidsBadInput(c *C) {
	err := os.MkdirAll(filepath.Join(s.rootDir, "group1"), 0755)
	c.Assert(err, IsNil)
	gPids := []byte(`123
zebra
567
`)
	err = ioutil.WriteFile(filepath.Join(s.rootDir, "group1/cgroup.procs"), gPids, 0755)
	c.Assert(err, IsNil)

	pids, err := cgroup.PidsInGroup(s.rootDir, "group1")
	c.Assert(err, ErrorMatches, `cannot parse pid "zebra"`)
	c.Assert(pids, IsNil)
}
