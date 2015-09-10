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

func (s *initRAMFSSuite) TestFreeSpaceWithResize(c *check.C) {
	if common.BeforeReboot() {
		bootDir := getCurrentBootDir(c)
		writablePercent := "85"
		cmd := exec.Command("sh", "_integration-tests/scrpts/install-test-initramfs", bootDir, writablePercent)
		err := cmd.Run()
		c.Assert(err, check.IsNil, check.Commentf("Error installing the test initrafms: %s", err))
		common.Reboot(c)
	} else if AfterReboot(c) {		
		freeSpace := getFreeSpacePercent(c)
		c.Assert(freeSpace < 10, check.Equals, true,
			check.Commentf("The free space at the end of the disk is greater than 10%"))
	}
}
