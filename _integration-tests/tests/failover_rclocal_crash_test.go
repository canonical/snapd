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

	. "gopkg.in/check.v1"
)

type rcLocalCrash struct{}

func (rcLocalCrash) set(c *C) {
	makeWritable(c, baseOtherPath)
	targetFile := fmt.Sprintf("%s/etc/rc.local", baseOtherPath)
	execCommand(c, "sudo", "chmod", "a+xw", targetFile)
	execCommandToFile(c, targetFile,
		"sudo", "echo", "#!bin/sh\nprintf c > /proc/sysrq-trigger")
	makeReadonly(c, baseOtherPath)
}

func (rcLocalCrash) unset(c *C) {
	makeWritable(c, baseOtherPath)
	execCommand(c, "sudo", "rm", fmt.Sprintf("%s/etc/rc.local", baseOtherPath))
	makeReadonly(c, baseOtherPath)
}

func (s *FailoverSuite) TestRCLocalCrash(c *C) {
	commonFailoverTest(c, rcLocalCrash{})
}
