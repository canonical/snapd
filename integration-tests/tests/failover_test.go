// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

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
	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
)

var _ = check.Suite(&failoverSuite{})

type failoverSuite struct {
	common.SnappySuite
}

// The types that implement this interface can be used in the test logic
type failer interface {
	// Sets the failure conditions
	set(c *check.C)
	// Unsets the failure conditions
	unset(c *check.C)
}

// This is the logic common to all the failover tests. Each of them has define a
// type implementing the failer interface and call this function with an instance
// of it
func commonFailoverTest(c *check.C, f failer) {
	currentVersion := common.GetCurrentUbuntuCoreVersion(c)

	if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		f.unset(c)
		c.Assert(common.GetSavedVersion(c), check.Equals, currentVersion,
			check.Commentf("Rebooted to the wrong version"))
	} else {
		common.SetSavedVersion(c, currentVersion-1)
		common.CallFakeUpdate(c)
		f.set(c)
		common.Reboot(c)
	}
}
