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
	"strings"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/updates"
)

var _ = check.Suite(&failoverSuite{})

type failoverSuite struct {
	common.SnappySuite
}

// This is the logic common to all the failover tests. Each of them has to call this method
// with the snap that will be updated and the function that changes it to fail.
func (s *failoverSuite) testUpdateToBrokenVersion(c *check.C, snap string, changeFunc updates.ChangeFakeUpdateSnap) {
	snapName := strings.Split(snap, ".")[0]

	// FIXME: remove once the OS snap is fixed and has a working
	//        "snap booted" again
	cli.ExecCommand(c, "sudo", "snap", "booted")

	if common.BeforeReboot() {
		currentVersion := common.GetCurrentVersion(c, snapName)

		common.SetSavedVersion(c, currentVersion)
		updates.CallFakeUpdate(c, snap, changeFunc)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		currentVersion := common.GetCurrentVersion(c, snapName)

		common.RemoveRebootMark(c)
		c.Assert(currentVersion, check.Equals, common.GetSavedVersion(c),
			check.Commentf("Rebooted to the wrong version"))
	}
}
