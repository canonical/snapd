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

package update

import (
	"strconv"
	"testing"

	. "../common"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&updateSuite{})

type updateSuite struct {
	SnappySuite
}

func rollback(c *C) {
	savedVersion := GetSavedVersion(c)
	if GetCurrentVersion(c) != savedVersion {
		c.Log("Calling snappy rollback...")
		ExecCommand(c, "sudo", "snappy", "rollback", "ubuntu-core", strconv.Itoa(savedVersion))
		RebootWithMark(c, c.TestName()+"-rollback")
	}
}

func (s *updateSuite) TestUpdateMustInstallNewerVersion(c *C) {
	if BeforeReboot() {
		CallUpdate(c)
		Reboot(c)
	} else if AfterReboot(c) {
		defer rollback(c)
		c.Assert(GetCurrentVersion(c) > GetSavedVersion(c), Equals, true)
	}
}
