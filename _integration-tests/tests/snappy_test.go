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
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&InstallSuite{})

type InstallSuite struct{}

func (s *InstallSuite) installSnap(c *C, packageName string) string {
	return s.execCommand(c, "sudo", "snappy", "install", packageName)
}

func (s *InstallSuite) execCommand(c *C, cmds ...string) string {
	cmd := exec.Command(cmds[0], cmds[1:]...)
	output, err := cmd.CombinedOutput()
	stringOutput := string(output)
	c.Assert(err, IsNil, Commentf("Error: %v", stringOutput))
	return stringOutput
}

func (s *InstallSuite) SetUpSuite(c *C) {
	s.execCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot.timer")
}

func (s *InstallSuite) TearDownTest(c *C) {
	s.execCommand(c, "sudo", "snappy", "remove", "hello-world")
}

func (s *InstallSuite) TestInstallSnapMustPrintPackageInformation(c *C) {
	installOutput := s.installSnap(c, "hello-world")

	expected := "" +
		"Installing hello-world\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		"hello-world   .* .*  canonical \n" +
		".*\n"
	c.Assert(installOutput, Matches, expected)
}

func (s *InstallSuite) TestCallBinaryFromInstalledSnap(c *C) {
	s.installSnap(c, "hello-world")

	echoOutput := s.execCommand(c, "hello-world.echo")

	c.Assert(echoOutput, Equals, "Hello World!\n")
}

func (s *InstallSuite) TestInfoMustPrintInstalledPackageInformation(c *C) {
	s.installSnap(c, "hello-world")

	infoOutput := s.execCommand(c, "snappy", "info")

	expected := "(?ms).*^apps: hello-world\n"
	c.Assert(infoOutput, Matches, expected)
}
