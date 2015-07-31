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

	. "launchpad.net/snappy/_integration-tests/testutils/common"

	check "gopkg.in/check.v1"
)

var _ = check.Suite(&infoSuite{})

type infoSuite struct {
	SnappySuite
}

func (s *infoSuite) TestInfoMustPrintReleaseAndChannel(c *check.C) {
	// skip test when having a remote testbed (we can't know which the
	// release and channels are)
	if Cfg.RemoteTestbed {
		c.Skip(fmt.Sprintf(
			"Skipping %s while testing in remote testbed",
			c.TestName()))
	}

	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		fmt.Sprintf("^release: ubuntu-core/%s/%s\n", Cfg.Release, Cfg.Channel) +
		".*"

	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledApps(c *check.C) {
	InstallSnap(c, "hello-world")
	s.AddCleanup(func() {
		RemoveSnap(c, "hello-world")
	})
	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^apps: .*hello-world.*\n"
	c.Assert(infoOutput, check.Matches, expected)
}

func (s *infoSuite) TestInfoMustPrintInstalledFrameworks(c *check.C) {
	InstallSnap(c, "hello-dbus-fwk.canonical")
	s.AddCleanup(func() {
		RemoveSnap(c, "hello-dbus-fwk.canonical")
	})
	infoOutput := ExecCommand(c, "snappy", "info")

	expected := "(?ms)" +
		".*" +
		"^frameworks: .*hello-dbus-fwk.*\n" +
		".*"
	c.Assert(infoOutput, check.Matches, expected)
}
