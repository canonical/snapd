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

	"launchpad.net/snappy/_integration-tests/testutils/partition"
	"launchpad.net/snappy/_integration-tests/testutils/wait"
	. "launchpad.net/snappy/_integration-tests/testutils/common"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&rollbackSuite{})

type rollbackSuite struct {
	SnappySuite
}

func (s *rollbackSuite) TestRollbackMustRebootToOtherVersion(c *check.C) {
	if BeforeReboot() {
		CallFakeUpdate(c)
		Reboot(c)
	} else if CheckRebootMark(c.TestName()) {
		RemoveRebootMark(c)
		wait.ForFunction(c, "regular", partition.Mode)
		currentVersion := GetCurrentUbuntuCoreVersion(c)
		c.Assert(currentVersion > GetSavedVersion(c), check.Equals, true)
		ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core",
			strconv.Itoa(GetSavedVersion(c)))
		SetSavedVersion(c, currentVersion)
		RebootWithMark(c, c.TestName()+"-rollback")
	} else if CheckRebootMark(c.TestName() + "-rollback") {
		RemoveRebootMark(c)
		wait.ForFunction(c, "regular", partition.Mode)
		c.Assert(
			GetCurrentUbuntuCoreVersion(c) < GetSavedVersion(c), check.Equals, true)
	}
}
