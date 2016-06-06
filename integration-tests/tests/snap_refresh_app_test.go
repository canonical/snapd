// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!excludereboots,!classic

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
	"io/ioutil"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/config"
	"github.com/snapcore/snapd/integration-tests/testutils/refresh"
	"github.com/snapcore/snapd/integration-tests/testutils/store"
)

var _ = check.Suite(&snapRefreshAppSuite{})

type snapRefreshAppSuite struct {
	common.SnappySuite
}

func (s *snapRefreshAppSuite) TestAppRefresh(c *check.C) {
	snap := "hello-world"

	// install edge version from the store (which is squashfs)
	cli.ExecCommand(c, "sudo", "snap", "install", snap)
	defer cli.ExecCommand(c, "sudo", "snap", "remove", snap)

	// make a fakestore and make it available to snapd

	// use /var/tmp is not a tempfs for space reasons
	blobDir, err := ioutil.TempDir("/var/tmp", "snap-fake-store-blobs-")
	c.Assert(err, check.IsNil)
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", blobDir)

	fakeStore := store.NewStore(blobDir)
	err = fakeStore.Start()
	c.Assert(err, check.IsNil)
	defer fakeStore.Stop()

	env := fmt.Sprintf(`SNAPPY_FORCE_CPI_URL=%s`, fakeStore.URL())
	cfg, _ := config.ReadConfig(config.DefaultFileName)

	tearDownSnapd(c)
	defer setUpSnapd(c, cfg.FromBranch, "")
	setUpSnapd(c, cfg.FromBranch, env)
	defer tearDownSnapd(c)

	// run the fake refresh
	output := refresh.CallFakeSnapRefreshForSnap(c, snap, refresh.NoOp, fakeStore)
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*fake1.*")
}
