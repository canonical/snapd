// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"fmt"
	"os/exec"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&autoupdateMsgSuite{})

type autoupdateMsgSuite struct {
	common.SnappySuite
}

// Test that there is a proper message if autoupdate runs in the
// background
func (s *autoupdateMsgSuite) TestAutoUpdateMessageIsPrinted(c *check.C) {
	c.Skip("FIXME: port to snap")

	cli.ExecCommand(c, "sudo", "systemctl", "start", "snappy-autopilot")

	s.AddCleanup(func() {
		cli.ExecCommand(c, "sudo", "systemctl", "stop", "snappy-autopilot")
		// do not pollute the other tests with hello-world, in case it is installed
		_, err := exec.Command("sudo", "snappy", "remove", "hello-world").CombinedOutput()
		if err != nil {
			fmt.Println("hello-world didn't get installed")
		}
	})

	// FIXME: risk of race
	// (i.e. systemctl start finishes before install runs)
	snappyOutput, _ := exec.Command("sudo", "snappy", "install", "hello-world/edge").CombinedOutput()

	var expectedTxt string
	if common.Release(c) == "15.04" {
		expectedTxt = "another snappy is running, try again later"
	} else {
		expectedTxt = "Snappy is updating your system"
	}
	expectedPattern := "(?ms).*^" + expectedTxt + ".*\n.*"

	c.Assert(string(snappyOutput), check.Matches, expectedPattern)
}
