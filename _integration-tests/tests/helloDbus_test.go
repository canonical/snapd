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
	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&helloDbusSuite{})

type helloDbusSuite struct {
	common.SnappySuite
}

func (s *helloDbusSuite) TestCmdOutput(c *check.C) {
	common.InstallSnap(c, "hello-dbus-fwk.canonical")
	defer common.RemoveSnap(c, "hello-dbus-fwk.canonical")

	common.InstallSnap(c, "hello-dbus-app.canonical")
	defer common.RemoveSnap(c, "hello-dbus-app.canonical")

	output := cli.ExecCommand(c, "hello-dbus-app.client")

	expected := "PASS\n"

	c.Assert(output, check.Equals, expected,
		check.Commentf("Expected output %s not found, %s", expected, output))
}
