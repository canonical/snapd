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
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/sandbox/cgroup"
)

type pidsSuite struct{}

var _ = Suite(&pidsSuite{})

func (s *pidsSuite) TestParsePid(c *C) {
	pid := mylog.Check2(cgroup.ParsePid("10"))

	c.Check(pid, Equals, 10)
	_ = mylog.Check2(cgroup.ParsePid(""))
	c.Assert(err, ErrorMatches, `cannot parse pid ""`)
	_ = mylog.Check2(cgroup.ParsePid("-1"))
	c.Assert(err, ErrorMatches, `cannot parse pid "-1"`)
	_ = mylog.Check2(cgroup.ParsePid("foo"))
	c.Assert(err, ErrorMatches, `cannot parse pid "foo"`)
	_ = mylog.Check2(cgroup.ParsePid("12\x0034"))
	c.Assert(err.Error(), Equals, "cannot parse pid \"12\\x0034\"")
	_ = mylog.Check2(cgroup.ParsePid("ł"))
	c.Assert(err, ErrorMatches, `cannot parse pid "ł"`)
	_ = mylog.Check2(cgroup.ParsePid("1000000000000000000000000000000000000000000000"))
	c.Assert(err, ErrorMatches, `cannot parse pid "1000000000000000000000000000000000000000000000"`)
}
