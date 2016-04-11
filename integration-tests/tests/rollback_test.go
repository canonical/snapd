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
	c.Skip("port to snapd")

	if common.BeforeReboot() {
		// here we upgrade
		updates.CallFakeOSUpdate(c)
		common.Reboot(c)
	} else if common.CheckRebootMark(c.TestName()) {
		// after the first reboot we check that it actually booted
		// a newer version than before
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		savedVersion := common.GetSavedVersion(c)
		c.Assert(snappy.VersionCompare(currentVersion, savedVersion), check.Equals, 1,
			check.Commentf("First reboot to the wrong version: %s <= %s", currentVersion, savedVersion))
		// now we rollback to the previous version
		cli.ExecCommand(c, "sudo", "snappy", "rollback", partition.OSSnapName(c),
			common.GetSavedVersion(c))
		common.RebootWithMark(c, c.TestName()+"-rollback")
	} else if common.CheckRebootMark(c.TestName() + "-rollback") {
		// and on the second reboot we check that the rollback
		// did indeed rolled us back to the previous version
		common.RemoveRebootMark(c)
		currentVersion := common.GetCurrentUbuntuCoreVersion(c)
		savedVersion := common.GetSavedVersion(c)
		c.Assert(currentVersion, check.Equals, savedVersion,
			check.Commentf("Second reboot to the wrong version: %s != %s", currentVersion, savedVersion))
	}
}
