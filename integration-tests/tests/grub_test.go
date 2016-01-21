// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"fmt"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/partition"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&grubSuite{})

type grubSuite struct {
	common.SnappySuite
}

func (s *grubSuite) TestGrubBootDirMustNotContainKernelFiles(c *check.C) {
	bootSystem, err := partition.BootSystem()
	c.Assert(err, check.IsNil, check.Commentf("Error getting boot system: %s", err))

	if bootSystem != "grub" {
		c.Skip("This test checks properties of grub based systems")
	}

	bootDir := partition.BootDir(bootSystem)
	for _, targetFile := range []string{"vmlinuz", "initrd.img"} {
		output, err := cli.ExecCommandErr("find", bootDir, "-name", fmt.Sprintf(`"%s"`, targetFile))

		c.Check(err, check.IsNil)
		c.Check(output, check.Equals, "")
	}
}
