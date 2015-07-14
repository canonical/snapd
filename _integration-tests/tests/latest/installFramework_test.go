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
	"regexp"
	"time"

	. "../common"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&installFrameworkSuite{})

type installFrameworkSuite struct {
	SnappySuite
}

func (s *installFrameworkSuite) TearDownTest(c *check.C) {
	if !NeedsReboot() && CheckRebootMark("") {
		RemoveSnap(c, "docker")
	}
	// run cleanup last
	s.SnappySuite.TearDownTest(c)
}

func isDockerServiceRunning(c *check.C) bool {
	statusOutput := ExecCommand(
		c, "systemctl", "status", "docker_docker-daemon_*.service")

	expected := "(?ms)" +
		".* docker_docker-daemon_.*.service .*\n" +
		".*Loaded: loaded .*\n" +
		".*Active: active (running) .*\n" +
		".*"

	matched, err := regexp.MatchString(expected, statusOutput)
	c.Assert(err, check.IsNil)
	return matched
}

func (s *installFrameworkSuite) TestInstallFrameworkMustPrintPackageInformation(c *check.C) {
	installOutput := InstallSnap(c, "docker")

	expected := "(?ms)" +
		"Installing docker\n" +
		"Name +Date +Version +Developer \n" +
		".*" +
		"^docker +.* +.* +canonical \n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}

func (s *installFrameworkSuite) TestInstalledFrameworkServiceMustBeStarted(c *check.C) {
	InstallSnap(c, "docker")
	c.Assert(isDockerServiceRunning(c), check.Equals, true, "Docker service is not running")
}

func (s *installFrameworkSuite) TestFrameworkServiceMustBeStartedAfterReboot(c *check.C) {
	if BeforeReboot() {
		InstallSnap(c, "docker")
		Reboot(c)
	} else if AfterReboot(c) {
		RemoveRebootMark(c)
		// Give it time to start (i.e. avoid race between framework and ssh)
		timeout := 60
		i := 0
		for ; i < timeout; i++ {
			if isDockerServiceRunning(c) {
				break
			}
			time.Sleep(1 * time.Second)
		}
		c.Assert(i < timeout, check.Equals, true)
	}
}
