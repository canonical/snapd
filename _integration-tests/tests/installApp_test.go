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
	"net/http"
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/wait"

	check "gopkg.in/check.v1"
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

	echoOutput := common.ExecCommand(c, "hello-world.echo")

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

func (s *installAppSuite) TestInfoMustPrintInstalledPackageInformation(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	infoOutput := common.ExecCommand(c, "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(infoOutput, check.Matches, expected)
}

func (s *installAppSuite) TestAppNetworkingServiceMustBeStarted(c *check.C) {
	baseAppName := "xkcd-webserver"
	appName := baseAppName + ".canonical"
	common.InstallSnap(c, appName)
	s.AddCleanup(func() {
		common.RemoveSnap(c, appName)
	})

	err := wait.ForServerOnPort(c, 80)
	c.Assert(err, check.IsNil)

	resp, err := http.Get("http://localhost")
	c.Assert(err, check.IsNil)
	c.Check(resp.Status, check.Equals, "200 OK")
	c.Assert(resp.Proto, check.Equals, "HTTP/1.0")
}

func (s *installAppSuite) TestInstallUnexistingAppMustPrintError(c *check.C) {
	cmd := exec.Command("sudo", "snappy", "install", "unexisting.canonical")
	output, err := cmd.CombinedOutput()

	c.Assert(err, check.NotNil)
	c.Assert(string(output), check.Equals,
		"Installing unexisting.canonical\n"+
			"unexisting.canonical failed to install: snappy package not found\n")
}
