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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

func (s *cgroupSuite) mockPidCgroup(c *C, text string) int {
	f := filepath.Join(s.rootDir, "proc/333/cgroup")
	c.Assert(os.MkdirAll(filepath.Dir(f), 0755), IsNil)
	c.Assert(os.WriteFile(f, []byte(text), 0755), IsNil)
	return 333
}

func (s *cgroupSuite) TestV1SnapNameFromPidHappy(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	pid := s.mockPidCgroup(c, string(mockCgroup)) // defined in cgroup_test.go
	name := mylog.Check2(cgroup.SnapNameFromPid(pid))

	c.Check(name, Equals, "hello-world")
}

func (s *cgroupSuite) TestV1SnapNameFromPidUnhappy(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	pid := s.mockPidCgroup(c, "1:freezer:/\n")
	name := mylog.Check2(cgroup.SnapNameFromPid(pid))
	c.Assert(err, ErrorMatches, "cannot find a snap for pid .*")
	c.Check(name, Equals, "")
}

func (s *cgroupSuite) TestV1SnapNameFromPidEmptyName(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	pid := s.mockPidCgroup(c, "1:freezer:/snap./\n")
	name := mylog.Check2(cgroup.SnapNameFromPid(pid))
	c.Assert(err, ErrorMatches, "snap name in cgroup path is empty")
	c.Check(name, Equals, "")
}

func (s *cgroupSuite) TestSnapNameFromPidTracking(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	pid := s.mockPidCgroup(c, "1:name=systemd:/user.slice/user-1000.slice/user@1000.service/apps.slice/snap.foo.bar.00000-1111-3333.service\n")
	name := mylog.Check2(cgroup.SnapNameFromPid(pid))

	c.Check(name, Equals, "foo")
}

func (s *cgroupSuite) TestSnapNameFromPidWithoutSources(c *C) {
	restore := cgroup.MockVersion(cgroup.V2, nil)
	defer restore()

	pid := s.mockPidCgroup(c, "")
	name := mylog.Check2(cgroup.SnapNameFromPid(pid))
	c.Assert(err, ErrorMatches, "not supported")
	c.Check(name, Equals, "")
}
