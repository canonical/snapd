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
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installAppSuite{})

type installAppSuite struct {
	common.SnappySuite
}

func (s *installAppSuite) TestInstallAppMustPrintPackageInformation(c *check.C) {
	installOutput := common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	expected := "(?ms)" +
		"Installing hello-world\n" +
		"Name +Date +Version +Developer \n" +
		".*" +
		"^hello-world +.* +.* +canonical \n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}

func (s *installAppSuite) TestCallBinaryFromInstalledSnap(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	echoOutput := cli.ExecCommand(c, "hello-world.echo")

	c.Assert(echoOutput, check.Equals, "Hello World!\n")
}

func (s *installAppSuite) TestCallBinaryWithPermissionDeniedMustPrintError(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	cmd := exec.Command("hello-world.evil")
	echoOutput, err := cmd.CombinedOutput()
	c.Assert(err, check.NotNil, check.Commentf("hello-world.evil did not fail"))

	expected := "" +
		"Hello Evil World!\n" +
		"This example demonstrates the app confinement\n" +
		"You should see a permission denied error next\n" +
		"/apps/hello-world.canonical/.*/bin/evil: \\d+: " +
		"/apps/hello-world.canonical/.*/bin/evil: " +
		"cannot create /var/tmp/myevil.txt: Permission denied\n"

	c.Assert(string(echoOutput), check.Matches, expected)
}

func (s *installAppSuite) TestInstallUnexistingAppMustPrintError(c *check.C) {
	cmd := exec.Command("sudo", "snappy", "install", "unexisting.canonical")
	output, err := cmd.CombinedOutput()

	c.Assert(err, check.NotNil)
	c.Assert(string(output), check.Equals,
		"Installing unexisting.canonical\n"+
			"unexisting failed to install: snappy package not found\n")
}
