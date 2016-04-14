// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&classicDimensionSuite{})

type classicDimensionSuite struct {
	common.SnappySuite
}

func (s *classicDimensionSuite) SetUpTest(c *check.C) {
	c.Skip("FIXME: re-enable once snap classic is back")
}

func (s *classicDimensionSuite) enableClassic(c *check.C) {
	output := cli.ExecCommand(c, "sudo", "env", "https_proxy="+os.Getenv("https_proxy"), "snap", "enable-classic")

	expected := "(?ms)" +
		".*" +
		"Classic dimension enabled on this snappy system.\n" +
		"Use \"snap shell classic\" to enter the classic dimension.\n"
	c.Assert(output, check.Matches, expected)
}

func (s *classicDimensionSuite) destroyClassic(c *check.C) {
	output := cli.ExecCommand(c, "sudo", "snap", "destroy-classic")

	expected := "Classic dimension destroyed on this snappy system.\n"
	c.Assert(output, check.Equals, expected)
}

func (s *classicDimensionSuite) TestClassicShell(c *check.C) {
	c.Skip("Skipping until LP: #1563193 is fixed")
	s.enableClassic(c)
	defer s.destroyClassic(c)

	enteringOutput := cli.ExecCommand(c, "snap", "shell", "classic")
	expectedEnteringOutput := "Entering classic dimension\n" +
		"\n" +
		"\n" +
		"The home directory is shared between snappy and the classic dimension.\n" +
		"Run \"exit\" to leave the classic shell.\n" +
		"\n"
	c.Assert(enteringOutput, check.Equals, expectedEnteringOutput)
}

func (s *classicDimensionSuite) TestDestroyUnexistingClassicMustPrintError(c *check.C) {
	output, err := cli.ExecCommandErr("sudo", "snap", "destroy-classic")

	c.Check(err, check.NotNil,
		check.Commentf("Trying to destroy unexisting classic dimension did not exit with an error"))
	c.Assert(string(output), check.Equals,
		"error: Classic dimension is not enabled.\n",
		check.Commentf("Wrong error message"))
}

func (s *classicDimensionSuite) TestReEnableClassicMustPrintError(c *check.C) {
	c.Skip("Skipping until LP: #1563193 is fixed")
	s.enableClassic(c)
	defer s.destroyClassic(c)
	output, err := cli.ExecCommandErr("sudo", "snap", "enable-classic")

	c.Check(err, check.NotNil,
		check.Commentf("Trying to re-enable classic dimension did not exit with an error"))
	c.Assert(string(output), check.Equals,
		"Classic dimension is already enabled.\n",
		check.Commentf("Wrong error message"))
}
