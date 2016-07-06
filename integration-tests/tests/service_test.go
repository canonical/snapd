// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
	"github.com/snapcore/snapd/integration-tests/testutils/wait"
	"github.com/snapcore/snapd/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&serviceSuite{})

type serviceSuite struct {
	common.SnappySuite
}

func (s *serviceSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)
}

func (s *serviceSuite) TearDownTest(c *check.C) {
	if !common.IsInRebootProcess() {
		common.RemoveSnap(c, data.BasicServiceSnapName)
	}
	// run cleanup last
	s.SnappySuite.TearDownTest(c)
}

func isServiceRunning(c *check.C) bool {
	service := fmt.Sprintf("snap.%s.service.service", data.BasicServiceSnapName)
	err := wait.ForActiveService(c, service)
	c.Assert(err, check.IsNil)

	statusOutput := cli.ExecCommand(c, "systemctl", "status", service)

	expected := "(?ms)" +
		fmt.Sprintf(".* %s .*\n", service) +
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

	output := common.InstallSnap(c, snapPath)
	c.Assert(output, testutil.Contains, data.BasicServiceSnapName)
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
