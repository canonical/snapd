// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package latest

import (
	. "../common"

	. "gopkg.in/check.v1"
)

var _ = Suite(&listSuite{})

type listSuite struct {
	SnappySuite
}

func (s *listSuite) TestListMustPrintAppVersion(c *C) {
	InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		RemoveSnap(c, "hello-world")
	})

	listOutput := ExecCommand(c, "snappy", "list")
	expected := "(?ms)" +
		"Name +Date +Version +Developer *\n" +
		".*" +
		"^hello-world +.* (\\d+)(\\.\\d+)* +.* +.* *\n" +
		".*"

	c.Assert(listOutput, Matches, expected)
}
