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

package cmd

import (
	. "../common"
	check "gopkg.in/check.v1"
)

var _ = check.Suite(&updateSuite{})

type updateSuite struct {
	SnappySuite
}

// Test that the update to the same release and channel must install a newer
// version. If there is no update available, the channel version will be
// modified to fake an update. If there is a version available, the image will
// be up-to-date after running this test.
func (s *updateSuite) TestUpdateToSameReleaseAndChannel(c *check.C) {
	if BeforeReboot() {
		CallFakeUpdate(c)
		Reboot(c)
	} else if AfterReboot(c) {
		RemoveRebootMark(c)
		c.Assert(GetCurrentUbuntuCoreVersion(c) > GetSavedVersion(c),
			check.Equals, true)
	}
}
