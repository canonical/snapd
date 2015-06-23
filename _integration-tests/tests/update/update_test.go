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
	originalVersion int
}

func rollback(c *C, packageName string, version int) {
	ExecCommand(c, "sudo", "snappy", "rollback", packageName, string(version))
	Reboot(c)
}

func (s *UpdateSuite) SetUpTest(c *C) {
	s.originalVersion = GetCurrentVersion(c)
}

func (s *UpdateSuite) TearDownTest(c *C) {
	if GetCurrentVersion(c) != s.originalVersion {
		rollback(c, "ubuntu-core", s.originalVersion)
	}
}

func (s *UpdateSuite) TestUpdateMustInstallNewerVersion(c *C) {
	if !AfterReboot(c) {
		CallUpdate(c)
		Reboot(c)
	} else {
		c.Assert(GetCurrentVersion(c) < s.originalVersion, Equals, true)
	}
}
