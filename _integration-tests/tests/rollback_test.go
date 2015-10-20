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
	"strconv"

	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&rollbackSuite{})

type rollbackSuite struct {
	common.SnappySuite
}

func (s *rollbackSuite) TestRollbackMustRebootToOtherVersion(c *check.C) {
	if common.BeforeReboot() {
		common.CallFakeUpdate(c)
		common.Reboot(c)
	} else if common.CheckRebootMark(c.TestName()) {
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		c.Assert(currentVersion > common.GetSavedVersion(c), check.Equals, true,
			check.Commentf("Rebooted to the wrong version: %d", currentVersion))
		cli.ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core",
			strconv.Itoa(common.GetSavedVersion(c)))
		common.SetSavedVersion(c, currentVersion)
		common.RebootWithMark(c, c.TestName()+"-rollback")
	} else if common.CheckRebootMark(c.TestName() + "-rollback") {
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		c.Assert(currentVersion < common.GetSavedVersion(c), check.Equals, true,
			check.Commentf("Rebooted to the wrong version: %d", currentVersion))
	}
}
