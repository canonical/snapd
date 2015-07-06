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
	"os/exec"

	. "../common"

	. "gopkg.in/check.v1"
)

var _ = Suite(&installSuite{})

type installSuite struct {
	SnappySuite
}

func (s *installSuite) TearDownTest(c *C) {
	RemoveSnap(c, "hello-world")
}

func (s *installSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := InstallSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"

	c.Assert(installOutput, Matches, expected)
}

func (s *installSuite) TestCallBinaryFromInstalledSnap(c *C) {
	InstallSnap(c, "hello-world")

	echoOutput := ExecCommand(c, "hello-world.echo")

	c.Assert(echoOutput, Equals, "Hello World!\n")
}

func (s *installSuite) TestCallBinaryWithPermissionDeniedMustPrintError(c *C) {
	InstallSnap(c, "hello-world")

	cmd := exec.Command("hello-world.evil")
	echoOutput, err := cmd.CombinedOutput()
	c.Assert(err, NotNil, Commentf("hello-world.evil did not fail"))

	expected := "" +
		"Hello Evil World!\n" +
		"This example demonstrates the app confinement\n" +
		"You should see a permission denied error next\n" +
		"/apps/hello-world.canonical/.*/bin/evil: \\d+: " +
		"/apps/hello-world.canonical/.*/bin/evil: " +
		"cannot create /var/tmp/myevil.txt: Permission denied\n"

	c.Assert(string(echoOutput), Matches, expected)
}

func (s *installSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	InstallSnap(c, "hello-world")

	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(infoOutput, Matches, expected)
}
