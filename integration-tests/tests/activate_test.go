// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"
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

func (s *activateSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	c.Skip("FIXME: port to snap")
	if common.Release(c) == "15.04" {
		c.Skip("activate CLI command not available on 15.04, reenable the test when present")
	}
	var err error
	s.snapPath, err = build.LocalSnap(c, activateSnapName)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, s.snapPath)
}

func (s *activateSuite) TearDownTest(c *check.C) {
	os.Remove(s.snapPath)
	common.RemoveSnap(c, activateSnapName)
	s.SnappySuite.TearDownTest(c)
}

func (s *activateSuite) TestDeactivateRemovesBinary(c *check.C) {
	cli.ExecCommand(c, "sudo", "snappy", "deactivate", activateSnapName)
	defer cli.ExecCommand(c, "sudo", "snappy", "activate", activateSnapName)
	output, err := cli.ExecCommandErr(activateBinName)

	c.Assert(err, check.NotNil, check.Commentf("Deactivated snap binary did not exit with an error"))
	c.Assert(output, check.Not(check.Equals), activateEchoOutput,
		check.Commentf("Deactivated snap binary was not removed"))

	list := cli.ExecCommand(c, "snappy", "list", "-v")

	c.Assert(list, check.Matches, deActivatedPattern)
}

func (s *activateSuite) TestActivateBringsBinaryBack(c *check.C) {
	cli.ExecCommand(c, "sudo", "snappy", "deactivate", activateSnapName)
	cli.ExecCommand(c, "sudo", "snappy", "activate", activateSnapName)
	output := cli.ExecCommand(c, activateBinName)

	c.Assert(output, check.Equals, activateEchoOutput,
		check.Commentf("Wrong output from active snap binary"))

	list := cli.ExecCommand(c, "snappy", "list", "-v")

	c.Assert(list, check.Matches, activatedPattern)
}
