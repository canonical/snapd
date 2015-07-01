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
	"testing"

	. "../common"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&installSuite{})

type installSuite struct {
	CommonSuite
}

func installSnap(c *C, packageName string) string {
	return ExecCommand(c, "sudo", "snappy", "install", packageName)
}

func (s *installSuite) TearDownTest(c *C) {
	ExecCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *installSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := installSnap(c, "hello-world")

	expected := "(?ms)" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*" +
		"^hello-world   .* .*  canonical \n" +
		".*"

	c.Assert(installOutput, Matches, expected)
}

func (s *installSuite) TestCallBinaryFromInstalledSnap(c *C) {
	installSnap(c, "hello-world")

	echoOutput := ExecCommand(c, "hello-world.echo")

	c.Assert(echoOutput, Equals, "Hello World!\n")
}

func (s *installSuite) TestCallBinaryWithPermissionDeniedMustPrintError(c *C) {
	installSnap(c, "hello-world")

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
	installSnap(c, "hello-world")

	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(infoOutput, Matches, expected)
}
