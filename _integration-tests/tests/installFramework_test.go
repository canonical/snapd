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
	"regexp"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installFrameworkSuite{})

type installFrameworkSuite struct {
	common.SnappySuite
}

func (s *installFrameworkSuite) TearDownTest(c *check.C) {
	if !common.NeedsReboot() && common.CheckRebootMark("") {
		common.RemoveSnap(c, "docker")
	}
	// run cleanup last
	s.SnappySuite.TearDownTest(c)
}

func isDockerServiceRunning(c *check.C) bool {
	dockerVersion := common.GetCurrentVersion(c, "docker")
	dockerService := fmt.Sprintf("docker_docker-daemon_%s.service", dockerVersion)

	err := wait.ForActiveService(c, dockerService)
	c.Assert(err, check.IsNil)

	statusOutput := cli.ExecCommand(
		c, "systemctl", "status",
		dockerService)

	expected := "(?ms)" +
		".* docker_docker-daemon_.*\\.service .*\n" +
		".*Loaded: loaded .*\n" +
		".*Active: active \\(running\\) .*\n" +
		".*"

	matched, err := regexp.MatchString(expected, statusOutput)
	c.Assert(err, check.IsNil)
	return matched
}

func (s *installFrameworkSuite) TestInstallFrameworkMustPrintPackageInformation(c *check.C) {
	installOutput := common.InstallSnap(c, "docker")

	expected := "(?ms)" +
		"Installing docker\n" +
		"Name +Date +Version +Developer \n" +
		".*" +
		"^docker +.* +.* +canonical \n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}

func (s *installFrameworkSuite) TestInstalledFrameworkServiceMustBeStarted(c *check.C) {
	common.InstallSnap(c, "docker")
	c.Assert(isDockerServiceRunning(c), check.Equals, true)
}

func (s *installFrameworkSuite) TestFrameworkServiceMustBeStartedAfterReboot(c *check.C) {
	if common.BeforeReboot() {
		common.InstallSnap(c, "docker")
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		c.Assert(isDockerServiceRunning(c), check.Equals, true)
	}
}
