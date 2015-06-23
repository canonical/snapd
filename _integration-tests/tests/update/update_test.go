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
	"testing"

	. "../common"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&UpdateSuite{})

type UpdateSuite struct {
	CommonSuite
}

func rollback(c *C, packageName string, version int) {
	ExecCommand(c, "sudo", "snappy", "rollback", packageName, string(version))
	RebootWithMark(c, c.TestName()+"rollback")
}

func (s *UpdateSuite) SetUpTest(c *C) {
	SetSavedVersion(GetCurrentVersion(c))
}

func (s *UpdateSuite) TearDownTest(c *C) {
	if GetCurrentVersion(c) != GetSavedVersion() {
		rollback(c, "ubuntu-core", GetSavedVersion())
	}
}

func (s *UpdateSuite) TestUpdateMustInstallNewerVersion(c *C) {
	if BeforeReboot(c) {
		CallUpdate(c)
		Reboot(c)
	} else if AfterReboot(c) {
		RemoveRebootMark(c)
		c.Assert(GetCurrentVersion(c) > GetSavedVersion(), Equals, true)
	}
}
