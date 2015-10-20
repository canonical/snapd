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
	"os"
	"regexp"

	"launchpad.net/snappy/_integration-tests/testutils/build"
	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/data"
	"launchpad.net/snappy/_integration-tests/testutils/wait"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&serviceSuite{})

type serviceSuite struct {
	common.SnappySuite
}

func (s *serviceSuite) TearDownTest(c *check.C) {
	if !common.NeedsReboot() && common.CheckRebootMark("") {
		common.RemoveSnap(c, data.BasicServiceSnapName)
	}
	// run cleanup last
	s.SnappySuite.TearDownTest(c)
}

func isServiceRunning(c *check.C) bool {
	packageVersion := common.GetCurrentVersion(c, data.BasicServiceSnapName)
	service := fmt.Sprintf("%s_service_%s.service", data.BasicServiceSnapName, packageVersion)

	err := wait.ForActiveService(c, service)
	c.Assert(err, check.IsNil)

	statusOutput := cli.ExecCommand(c, "systemctl", "status", service)

	expected := "(?ms)" +
		fmt.Sprintf(".* %s_service_.*\\.service .*\n", data.BasicServiceSnapName) +
		".*Loaded: loaded .*\n" +
		".*Active: active \\(running\\) .*\n" +
		".*"

	matched, err := regexp.MatchString(expected, statusOutput)
	c.Assert(err, check.IsNil)
	return matched
}

func installSnapWithService(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicServiceSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
}

func (s *serviceSuite) TestInstalledServiceMustBeStarted(c *check.C) {
	installSnapWithService(c)
	c.Assert(isServiceRunning(c), check.Equals, true, check.Commentf("Service is not running"))
}

func (s *serviceSuite) TestServiceMustBeStartedAfterReboot(c *check.C) {
	if common.BeforeReboot() {
		installSnapWithService(c)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		c.Assert(isServiceRunning(c), check.Equals, true, check.Commentf("Service is not running"))
	}
}

func (s *serviceSuite) TestServiceMustBeStartedAfterUpdate(c *check.C) {
	if common.BeforeReboot() {
		installSnapWithService(c)
		common.CallFakeUpdate(c)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		c.Assert(isServiceRunning(c), check.Equals, true, check.Commentf("Service is not running"))
	}
}
