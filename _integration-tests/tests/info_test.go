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
	"fmt"
	"os"

	"launchpad.net/snappy/_integration-tests/testutils/build"
	"launchpad.net/snappy/_integration-tests/testutils/cli"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&infoSuite{})

type infoSuite struct {
	common.SnappySuite
}

func (s *infoSuite) TestInfoMustPrintReleaseAndChannel(c *check.C) {
	// skip test when having a remote testbed (we can't know which the
	// release and channels are)
	if common.Cfg.RemoteTestbed {
		c.Skip(fmt.Sprintf(
			"Skipping %s while testing in remote testbed",
			c.TestName()))
	}

	infoOutput := cli.ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		fmt.Sprintf("^release: ubuntu-core/%s/%s\n", common.Cfg.Release, common.Cfg.Channel) +
		".*"

	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledApps(c *check.C) {
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

func (s *infoSuite) TestInfoMustPrintInstalledFrameworks(c *check.C) {
	common.InstallSnap(c, "hello-dbus-fwk.canonical")
	s.AddCleanup(func() {
		common.RemoveSnap(c, "hello-dbus-fwk.canonical")
	})
	infoOutput := cli.ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^frameworks: .*hello-dbus-fwk.*\n" +
		".*"
	c.Assert(infoOutput, check.Matches, expected)
}
