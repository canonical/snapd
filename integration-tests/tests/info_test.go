// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&infoSuite{})

type infoSuite struct {
	common.SnappySuite
}

func (s *infoSuite) TestInfoMustPrintReleaseAndChannel(c *check.C) {
	c.Skip("port to snapd")

	// skip test when having a remote testbed (we can't know which the
	// release and channels are)
	if common.Cfg.RemoteTestbed {
		c.Skip(fmt.Sprintf(
			"Skipping %s while testing in remote testbed",
			c.TestName()))
	}

	infoOutput := cli.ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		fmt.Sprintf("^release: .*core/%s.*\n", common.Cfg.Release) +
		".*"

	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledApps(c *check.C) {
	c.Skip("port to snapd")

	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicSnapName)

	infoOutput := cli.ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^apps: .*" + data.BasicSnapName + "\\.sideload.*\n"
	c.Assert(infoOutput, check.Matches, expected)
}
