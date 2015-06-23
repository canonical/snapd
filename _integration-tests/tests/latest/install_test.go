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

func installSnap(c *C, packageName string) []byte {
	return ExecCommand(c, "sudo", "snappy", "install", packageName)
}

func (s *installSuite) TearDownTest(c *C) {
	ExecCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *installSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := installSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(string(installOutput), Matches, expected)
}

func (s *installSuite) TestCallBinaryFromInstalledSnap(c *C) {
	installSnap(c, "hello-world")

	echoOutput := ExecCommand(c, "hello-world.echo")

	c.Assert(string(echoOutput), Equals, "Hello World!\n")
}

func (s *installSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	installSnap(c, "hello-world")

	infoOutput := ExecCommand(c, "sudo", "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(string(infoOutput), Matches, expected)
}
