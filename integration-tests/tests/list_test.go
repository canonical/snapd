// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!classic

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
	"github.com/snapcore/snapd/integration-tests/testutils/partition"
	"github.com/snapcore/snapd/integration-tests/testutils/refresh"
	"github.com/snapcore/snapd/integration-tests/testutils/store"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&listSuite{})

type listSuite struct {
	common.SnappySuite
}

var verRegexp = `(\d{2}\.\d{2}.*|\w{12})`

func (s *listSuite) TestListMustPrintCoreVersion(c *check.C) {
	listOutput := cli.ExecCommand(c, "snap", "list")

	expected := "(?ms)" +
		"Name +Version +Rev +Developer +Notes *\n" +
		".*" +
		fmt.Sprintf("^%s +.* +%s +[0-9]+ +canonical +- *\n", partition.OSSnapName(c), verRegexp) +
		".*"
	c.Assert(listOutput, check.Matches, expected)
}

func (s *listSuite) TestListMustPrintAppVersion(c *check.C) {
	common.InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-world")
	})

	listOutput := cli.ExecCommand(c, "snap", "list")
	expected := "(?ms)" +
		"Name +Version +Rev +Developer +Notes *\n" +
		".*" +
		"^hello-world +(\\d+)(\\.\\d+)* +[0-9]+ +\\S+ +-\n" +
		".*"

	c.Assert(listOutput, check.Matches, expected)
}

func (s *listSuite) TestRefreshListSimple(c *check.C) {
	snap := "hello-world"

	common.InstallSnap(c, snap)
	s.AddCleanup(func() {
		common.RemoveSnap(c, snap)
	})

	// fake refresh
	blobDir, err := ioutil.TempDir("", "snap-fake-store-blobs-")
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

	refresh.MakeFakeRefreshForSnap(c, snap, blobDir, refresh.NoOp)

	listOutput := cli.ExecCommand(c, "snap", "refresh", "--list")
	expected := "(?ms)" +
		"Name +Version +Developer +Notes +Summary *\n" +
		".*" +
		"^hello-world +(\\d+)(\\.\\d+)\\+fake1 +canonical +- .*\n" +
		".*"

	c.Assert(listOutput, check.Matches, expected)
}
