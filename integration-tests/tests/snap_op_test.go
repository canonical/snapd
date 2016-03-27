// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&snapOpSuite{})

type snapOpSuite struct {
	common.SnappySuite
}

func (s *snapOpSuite) TestSnapInstallWorks(c *check.C) {
	installOutput := cli.ExecCommand(c, "sudo", "snap", "install", "hello-world.canonical")
	c.Assert(installOutput, check.Matches, `\n`)
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	listOutput := cli.ExecCommand(c, "snappy", "list")
	expected := "(?ms)" +
		"Name +Date +Version +Developer +\n" +
		".*" +
		"^hello-world .*\n" +
		".*"
	c.Assert(listOutput, check.Matches, expected)
}
