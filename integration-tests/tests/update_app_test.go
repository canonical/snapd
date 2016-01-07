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

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/store"
)

var _ = check.Suite(&updateAppSuite{})

type updateAppSuite struct {
	common.SnappySuite
}

func (s *updateAppSuite) TestAppUpdate(c *check.C) {
	// install base
	cli.ExecCommand(c, "sudo", "snappy", "install", "hello-world.canonical")
	defer cli.ExecCommand(c, "sudo", "snappy", "remove", "hello-world.canonical")

	// make mock-update
	blobDir := c.MkDir()

	// and provide it in the store
	store := store.NewStore(blobDir)
	store.Start()
	defer store.Stop()

	common.MakeFakeUpdateForSnap(c, "/apps/hello-world.canonical/current/", blobDir)

	// run the update and ensure we got a new faked version
	output := cli.ExecCommand(c, "sudo", fmt.Sprintf("SNAPPY_FORCE_CPI_URL=%s", store.URL()), "snappy", "update")
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*fake1.*")
}
