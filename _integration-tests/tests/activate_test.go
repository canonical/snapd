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
	"os"

	"gopkg.in/check.v1"

	"launchpad.net/snappy/_integration-tests/testutils/build"
	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/data"
)

const (
	activateSnapName    = data.BasicBinariesSnapName
	activateBinName     = activateSnapName + ".echo"
	activateEchoOutput  = "From basic-binaries snap\n"
	baseActivatePattern = "(?msU).*" + activateSnapName + `\s*.*\s*.*sideload`
	activatedPattern    = baseActivatePattern + `\*\s*\n.*`
	deActivatedPattern  = baseActivatePattern + `\s*\n.*`
)

var _ = check.Suite(&activateSuite{})

type activateSuite struct {
	common.SnappySuite
	snapPath string
}

func (s *activateSuite) SetUpSuite(c *check.C) {
	s.SnappySuite.SetUpSuite(c)
	var err error
	s.snapPath, err = build.LocalSnap(c, activateSnapName)
	c.Assert(err, check.IsNil)
	common.InstallSnap(c, s.snapPath)
}

func (s *activateSuite) TearDownSuite(c *check.C) {
	os.Remove(s.snapPath)
	common.RemoveSnap(c, activateSnapName)
}

func (s *activateSuite) TestDeactivateRemovesBinary(c *check.C) {
	cli.ExecCommand(c, "sudo", "snappy", "deactivate", activateSnapName)
	defer cli.ExecCommand(c, "sudo", "snappy", "activate", activateSnapName)
	output, err := cli.ExecCommandErr(activateBinName)

	c.Assert(err, check.NotNil)
	c.Assert(output, check.Not(check.Equals), activateEchoOutput)

	list := cli.ExecCommand(c, "snappy", "list", "-v")

	c.Assert(list, check.Matches, deActivatedPattern)
}

func (s *activateSuite) TestActivateBringsBinaryBack(c *check.C) {
	cli.ExecCommand(c, "sudo", "snappy", "deactivate", activateSnapName)
	cli.ExecCommand(c, "sudo", "snappy", "activate", activateSnapName)
	output := cli.ExecCommand(c, activateBinName)

	c.Assert(output, check.Equals, activateEchoOutput)

	list := cli.ExecCommand(c, "snappy", "list", "-v")

	c.Assert(list, check.Matches, activatedPattern)
}
