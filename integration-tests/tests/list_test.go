// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package tests

import (
	"fmt"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&listSuite{})

type listSuite struct {
	common.SnappySuite
}

// FIXME: hardcoded for now
var verRegexp = "16.04.*"

func (s *listSuite) TestListMustPrintCoreVersion(c *check.C) {
	listOutput := cli.ExecCommand(c, "snappy", "list")

	expected := "(?ms)" +
		"Name +Date +Version +Developer *\n" +
		".*" +
		fmt.Sprintf("^ubuntu-core +.* +%s +canonical *\n", verRegexp) +
		".*"
	c.Assert(listOutput, check.Matches, expected)
}

func (s *listSuite) TestListMustPrintAppVersion(c *check.C) {
	common.InstallSnap(c, "hello-world/edge")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	listOutput := cli.ExecCommand(c, "snappy", "list")
	expected := "(?ms)" +
		"Name +Date +Version +Developer *\n" +
		".*" +
		"^hello-world +.* +(\\d+)(\\.\\d+)* +.* +.* *\n" +
		".*"

	c.Assert(listOutput, check.Matches, expected)
}
