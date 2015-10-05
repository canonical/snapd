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

package tests

import (
	"fmt"
	"os"
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/build"
	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const (
	snapName          = "dev-kmsg"
	binName           = snapName + ".reader"
	installedSnapName = snapName + ".sideload"
	hwName            = "/dev/kmsg"
	hwAssignError     = "dd: failed to open ‘" + hwName + "’: Permission denied\n"
)

var _ = check.Suite(&hwAssignSuite{})

type hwAssignSuite struct {
	common.SnappySuite
	snapPath string
}

func (s *hwAssignSuite) SetUpTest(c *check.C) {
	s.SetUpTest(c)
	var err error
	s.snapPath, err = build.LocalSnap(c, snapName)
	c.Assert(err, check.IsNil)
	common.InstallSnap(c, s.snapPath)
}

func (s *hwAssignSuite) TearDownTest(c *check.C) {
	s.TearDownTest(c)
	os.Remove(s.snapPath)
	common.RemoveSnap(c, snapName)
}

func (s *hwAssignSuite) TestErrorWithoutHwAssign(c *check.C) {
	cmd := exec.Command(binName)
	output, err := cmd.CombinedOutput()

	c.Assert(err, check.NotNil)
	c.Assert(string(output), check.Equals, hwAssignError)
}

func (s *hwAssignSuite) TestSuccessAfterHwAssign(c *check.C) {
	assign(c, snapName, hwName)
	defer unassign(c, snapName, hwName)

	cmd := exec.Command(binName)
	output, _ := cmd.CombinedOutput()

	c.Assert(string(output), check.Not(check.Equals), hwAssignError)
}

func (s *hwAssignSuite) TestErrorAfterHwUnAssign(c *check.C) {
	assign(c, snapName, hwName)
	unassign(c, snapName, hwName)

	cmd := exec.Command(binName)
	output, err := cmd.CombinedOutput()

	c.Assert(err, check.NotNil)
	c.Assert(string(output), check.Equals, hwAssignError)
}

func assign(c *check.C, snap, hw string) {
	cmd := exec.Command("sudo", "snappy", "hw-assign", installedSnapName, hwName)
	output, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Assert(string(output), check.Equals,
		fmt.Sprintf("'%s' is now allowed to access '%s'\n", installedSnapName, hwName))
}

func unassign(c *check.C, snap, hw string) {
	cmd := exec.Command("sudo", "snappy", "hw-unassign", installedSnapName, hwName)
	output, err := cmd.CombinedOutput()
	c.Assert(err, check.IsNil)
	c.Assert(string(output), check.Equals,
		fmt.Sprintf("'%s' is no longer allowed to access '%s'\n", installedSnapName, hwName))

}
