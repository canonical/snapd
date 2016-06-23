// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/config"
	"github.com/snapcore/snapd/integration-tests/testutils/refresh"
	"github.com/snapcore/snapd/integration-tests/testutils/store"
	"github.com/snapcore/snapd/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&revertAppSuite{})

type revertAppSuite struct {
	common.SnappySuite
}

func (s *revertAppSuite) TestInstallUpdateRevert(c *check.C) {
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

	// run the fake update
	output := refresh.CallFakeSnapRefreshForSnap(c, snap, refresh.NoOp, fakeStore)
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*fake1.*")

	// NOW do the revert
	output = cli.ExecCommand(c, "sudo", "snap", "revert", snap)
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*")
	c.Assert(output, check.Not(testutil.Contains), "fake1")

	// and ensure data/prev version is still there
	output = cli.ExecCommand(c, "snap", "list")
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*")

	output = cli.ExecCommand(c, "ls", "/snap/hello-world")
	c.Assert(output, testutil.Contains, "current")

	output = cli.ExecCommand(c, "ls", "/var/snap/hello-world")
	c.Assert(output, testutil.Contains, "current")

	// revert again and it errors
	output, err = cli.ExecCommandErr("sudo", "snap", "revert", snap)
	c.Assert(err, check.NotNil)
	c.Assert(output, check.Matches, "error:.*: no revision to revert to\n")

	// do a `refresh all` and ensure we do not upgrade to the version
	// we just rolled back from
	output = cli.ExecCommand(c, "sudo", "snap", "refresh")
	c.Check(output, check.Matches, "(?ms).*^hello-world.*")
	c.Check(output, check.Not(testutil.Contains), "fake1")

	// do a `refresh hello-world` and ensure that an explicit
	// refresh will work
	output = cli.ExecCommand(c, "sudo", "snap", "refresh", snap)
	c.Check(output, check.Matches, "(?ms).*^hello-world.*")
	c.Check(output, testutil.Contains, "fake1")

	// and revert again (after the refresh) is fine
	output = cli.ExecCommand(c, "sudo", "snap", "revert", snap)
	c.Assert(output, check.Matches, "(?ms).*^hello-world.*")
	c.Assert(output, check.Not(testutil.Contains), "fake1")
}
