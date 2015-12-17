// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

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

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"

	"gopkg.in/check.v1"
)

type rcLocalCrash struct{}

func (rcLocalCrash) set(c *check.C) {
	partition.MakeWritable(c, common.BaseAltPartitionPath)
	defer partition.MakeReadonly(c, common.BaseAltPartitionPath)
	targetFile := fmt.Sprintf("%s/etc/rc.local", common.BaseAltPartitionPath)
	cli.ExecCommand(c, "sudo", "chmod", "a+xw", targetFile)

	cli.ExecCommandToFile(c, targetFile,
		"sudo", "echo", "#!bin/sh\nprintf c > /proc/sysrq-trigger")
}

func (rcLocalCrash) unset(c *check.C) {
	partition.MakeWritable(c, common.BaseAltPartitionPath)
	defer partition.MakeReadonly(c, common.BaseAltPartitionPath)
	cli.ExecCommand(c, "sudo", "rm", fmt.Sprintf("%s/etc/rc.local", common.BaseAltPartitionPath))
}

/*
TODO: uncomment when bug https://bugs.launchpad.net/snappy/+bug/1476129 is fixed
(fgimenez 20150728)

func (s *failoverSuite) TestRCLocalCrash(c *check.C) {
	commonFailoverTest(c, rcLocalCrash{})
}
*/
