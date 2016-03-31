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
	"fmt"
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const (
	snapName          = "dev-kmsg"
	binName           = snapName + ".reader"
	installedSnapName = snapName
	hwName            = "/dev/kmsg"
	hwAssignError     = "dd: failed to open '" + hwName + "': Permission denied\n"
)

var _ = check.Suite(&hwAssignSuite{})

type hwAssignSuite struct {
	common.SnappySuite
	snapPath string
}

func (s *hwAssignSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)
	var err error
	s.snapPath, err = build.LocalSnap(c, snapName)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, s.snapPath)
}

func (s *hwAssignSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)
	os.Remove(s.snapPath)
	common.RemoveSnap(c, snapName)
}

func (s *hwAssignSuite) TestErrorWithoutHwAssign(c *check.C) {
	output, err := cli.ExecCommandErr(binName)

	c.Assert(err, check.NotNil,
		check.Commentf("The snap binary without hardware assigned did not exit with an error"))
	c.Assert(output, check.Equals, hwAssignError,
		check.Commentf("Wrong error message"))
}

func (s *hwAssignSuite) TestSuccessAfterHwAssign(c *check.C) {
	assign(c, snapName, hwName)
	defer unassign(c, snapName, hwName)

	output, _ := cli.ExecCommandErr(binName)

	c.Assert(string(output), check.Not(check.Equals), hwAssignError,
		check.Commentf("The snap binary with hardware assigned printed a permission denied error"))
}

func (s *hwAssignSuite) TestErrorAfterHwUnAssign(c *check.C) {
	assign(c, snapName, hwName)
	unassign(c, snapName, hwName)

	output, err := cli.ExecCommandErr(binName)

	c.Assert(err, check.NotNil,
		check.Commentf("The snap binary without hardware assigned did not exit with an error"))
	c.Assert(output, check.Equals, hwAssignError,
		check.Commentf("Wrong error message"))
}

func (s *hwAssignSuite) TestHwInfo(c *check.C) {
	output, err := cli.ExecCommandErr("sudo", "snappy", "hw-info", installedSnapName)
	c.Assert(err, check.IsNil)

	expected := fmt.Sprintf("'%s:' is not allowed to access additional hardware\n", installedSnapName)
	c.Assert(output, check.Equals, expected,
		check.Commentf(`Expected "%s", obtained "%s"`, expected, output))

	assign(c, snapName, hwName)
	defer unassign(c, snapName, hwName)

	output, err = cli.ExecCommandErr("sudo", "snappy", "hw-info", installedSnapName)
	c.Assert(err, check.IsNil)

	expected = fmt.Sprintf("%s: %s\n", installedSnapName, hwName)
	c.Assert(output, check.Equals, expected,
		check.Commentf(`Expected "%s", obtained "%s"`, expected, output))
}

func assign(c *check.C, snap, hw string) {
	output, err := cli.ExecCommandErr("sudo", "snappy", "hw-assign", installedSnapName, hwName)

	c.Assert(err, check.IsNil, check.Commentf("Error assigning hardware: %s", err))
	c.Assert(output, check.Equals,
		fmt.Sprintf("'%s' is now allowed to access '%s'\n", installedSnapName, hwName),
		check.Commentf("Wrong message after assigning hardware"))
}

func unassign(c *check.C, snap, hw string) {
	output, err := cli.ExecCommandErr("sudo", "snappy", "hw-unassign", installedSnapName, hwName)

	c.Assert(err, check.IsNil, check.Commentf("Error unassigning hardware: %s", err))
	c.Assert(output, check.Equals,
		fmt.Sprintf("'%s' is no longer allowed to access '%s'\n", installedSnapName, hwName),
		check.Commentf("Wrong message after unassigning hardware"))
}
