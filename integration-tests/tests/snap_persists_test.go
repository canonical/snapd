// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&snapPersistsSuite{})

type snapPersistsSuite struct {
	common.SnappySuite
}

func (s *snapPersistsSuite) TestSnapPersistAfterReboot(c *check.C) {
	if common.BeforeReboot() {
		snapPath, err := build.LocalSnap(c, data.BasicSnapName)
		defer os.Remove(snapPath)
		c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
		common.InstallSnap(c, snapPath)

		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		defer common.RemoveSnap(c, data.BasicSnapName)

		output, err := cli.ExecCommandErr("snap", "list")
		c.Assert(err, check.IsNil)

		expected := "(?ms)Name .*\n" +
			".*" +
			"^basic +.* *\n" +
			".*"
		c.Assert(output, check.Matches, expected)
	}
}
