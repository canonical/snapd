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
	"fmt"

	. "launchpad.net/snappy/_integration-tests/testutils/common"

	check "gopkg.in/check.v1"
)

type rcLocalCrash struct{}

func (rcLocalCrash) set(c *check.C) {
	MakeWritable(c, BaseAltPartitionPath)
	defer MakeReadonly(c, BaseAltPartitionPath)
	targetFile := fmt.Sprintf("%s/etc/rc.local", BaseAltPartitionPath)
	ExecCommand(c, "sudo", "chmod", "a+xw", targetFile)
	ExecCommandToFile(c, targetFile,
		"sudo", "echo", "#!bin/sh\nprintf c > /proc/sysrq-trigger")
}

func (rcLocalCrash) unset(c *check.C) {
	MakeWritable(c, BaseAltPartitionPath)
	defer MakeReadonly(c, BaseAltPartitionPath)
	ExecCommand(c, "sudo", "rm", fmt.Sprintf("%s/etc/rc.local", BaseAltPartitionPath))
}

func (s *failoverSuite) TestRCLocalCrash(c *check.C) {
	commonFailoverTest(c, rcLocalCrash{})
}
