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
	"path"
	"strconv"
	"strings"

	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/partition"	

	"gopkg.in/check.v1"
)

var _ = check.Suite(&initRAMFSSuite{})

type initRAMFSSuite struct {
	common.SnappySuite
}

func getFreeSpacePercent(c *check.C) float64 {
	cmd := exec.Command("sh", "_integration-tests/scripts/get_unpartitioned_space")
	free, err := cmd.Output()
	c.Assert(err, check.IsNil, check.Commentf("Error running the script to get the free space: %s", err))
	freePercent := strings.TrimRight(strings.TrimSpace(string(free)), "%")
	freePercentFloat, err := strconv.ParseFloat(freePercent, 32)
	c.Assert(err, check.IsNil,
		check.Commentf("Error converting the free space percentage to float: %s", err))
	return freePercentFloat
}

func getCurrentBootDir(c *check.C) string {
	system, err := partition.BootSystem()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the boot system: %s", err))
	bootDir := partition.BootDir(system)
	current, err := partition.CurrentPartition()
	c.Assert(err, check.IsNil, check.Commentf("Error getting the current partition: %s", err))
	return path.Join(bootDir, current)
}

func (s *initRAMFSSuite) TestFreeSpaceWithoutResize(c *check.C) {
  writablePercent := "95"	
	if common.BeforeReboot() {
		bootDir := getCurrentBootDir(c)
		common.ExecCommand(
			c, "sh", "-x", "_integration-tests/scripts/install-test-initramfs", bootDir, writablePercent)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)		
		freeSpace := getFreeSpacePercent(c)
		c.Assert(freeSpace, check.Equals, 5,
			check.Commentf("The writable partition was resized"))
	}	
}

func (s *initRAMFSSuite) TestFreeSpaceWithResize(c *check.C) {
	if common.BeforeReboot() {
		bootDir := getCurrentBootDir(c)
		writablePercent := "85"
		common.ExecCommand(
			c, "sh", "-x", "_integration-tests/scripts/install-test-initramfs", bootDir, writablePercent)
		common.Reboot(c)
	} else if common.AfterReboot(c) {
		common.RemoveRebootMark(c)
		freeSpace := getFreeSpacePercent(c)
		c.Assert(freeSpace < 10, check.Equals, true,
			check.Commentf("The writable partition was not resized"))
	}
}
