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
	"os/exec"

	"github.com/ubuntu-core/snappy/_integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&autoupdateMsgSuite{})

type autoupdateMsgSuite struct {
	common.SnappySuite
}

// Test that there is a proper message if autoupdate runs in the
// background
func (s *autoupdateMsgSuite) TestAutoUpdateMessageIsPrinted(c *check.C) {
	cli.ExecCommand(c, "sudo", "systemctl", "start", "snappy-autopilot")

	// do not pollute the other tests with the now installed hello-world
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	// FIXME: risk of race
	// (i.e. systemctl start finishes before install runs)
	snappyOutput, _ := exec.Command("sudo", "snappy", "install", "hello-world").CombinedOutput()

	expected := "(?ms)" +
		".*" +
		"^snappy autoupdate is updating your system.*\n" +
		".*"
	c.Assert(string(snappyOutput), check.Matches, expected)
}
