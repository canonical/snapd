// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots

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

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/updates"
)

var _ = check.Suite(&updateAppSuite{})

type updateAppSuite struct {
	common.SnappySuite
}

func (s *updateAppSuite) TestAppUpdate(c *check.C) {
	snap := "hello-world.canonical"
	storeSnap := fmt.Sprintf("%s/edge", snap)

	// install edge version from the store (which is squshfs)
	cli.ExecCommand(c, "sudo", "snappy", "install", storeSnap)
	defer cli.ExecCommand(c, "sudo", "snappy", "remove", snap)

	output := updates.CallFakeUpdate(c, snap, updates.NoOp)
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*fake1.*")
}
