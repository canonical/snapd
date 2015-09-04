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

	. "launchpad.net/snappy/_integration-tests/testutils/common"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&initRAMFSSuite{})

type initRAMFSSuite struct {
	SnappySuite
}

func (s *initRAMFSSuite) TestFreeSpace(c *check.C) {
	cmd := exec.Command("sh", "_integration-tests/tests/get_unpartitioned_space")
	free, err := cmd.Output()
	freePercent := strings.TrimRight(strings.TrimSpace(string(free)), "%")
	freePercentFloat, err := strconv.ParseFloat(freePercent, 32)
	c.Assert(err, check.IsNil,
		check.Commentf("Error converting the free space percentage to float: %s", err))
	c.Assert(freePercentFloat < 10, check.Equals, true,
		check.Commentf("The free space at the end of the disk is greater than 10%"))
}
