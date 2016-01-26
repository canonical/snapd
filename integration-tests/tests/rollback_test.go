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
	"github.com/ubuntu-core/snappy/snappy"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/updates"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&rollbackSuite{})

type rollbackSuite struct {
	common.SnappySuite
}

func (s *rollbackSuite) TestRollbackMustRebootToOtherVersion(c *check.C) {
	c.Skip("KNOWN BUG FIXME: https://bugs.launchpad.net/snappy/+bug/1534029")

	if common.BeforeReboot() {
		updates.CallFakeOSUpdate(c)
		common.Reboot(c)
	} else if common.CheckRebootMark(c.TestName()) {
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		c.Assert(snappy.VersionCompare(currentVersion, common.GetSavedVersion(c)), check.Equals, -1,
			check.Commentf("Rebooted to the wrong version: %s", currentVersion))
		cli.ExecCommand(c, "sudo", "snappy", "rollback", partition.OSSnapName(c),
			common.GetSavedVersion(c))
		common.RebootWithMark(c, c.TestName()+"-rollback")
	} else if common.CheckRebootMark(c.TestName() + "-rollback") {
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		c.Assert(snappy.VersionCompare(currentVersion, common.GetSavedVersion(c)), check.Equals, 0,
			check.Commentf("Rebooted to the wrong version: %s", currentVersion))
	}
}
