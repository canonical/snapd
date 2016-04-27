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
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"
	"github.com/ubuntu-core/snappy/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installAppSuite{})

type installAppSuite struct {
	common.SnappySuite
}

func (s *installAppSuite) TestInstallAppMustPrintPackageInformation(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	installOutput := common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicSnapName)

	expected := "(?ms)" +
		"Name +Version +Developer\n" +
		".*" +
		"^basic +.* *\n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}

func (s *installAppSuite) TestCallSuccessfulBinaryFromInstalledSnap(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	// Exec command does not fail.
	cli.ExecCommand(c, "basic-binaries.success")
}

func (s *installAppSuite) TestCallFailBinaryFromInstalledSnap(c *check.C) {
	c.Skip("port to snapd")

	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	_, err = cli.ExecCommandErr("basic-binaries.fail")
	c.Assert(err, check.NotNil, check.Commentf("The binary did not fail"))
}

func (s *installAppSuite) TestInstallUnexistingAppMustPrintError(c *check.C) {
	output, err := cli.ExecCommandErr("sudo", "snap", "install", "unexisting.canonical")

	c.Check(err, check.NotNil,
		check.Commentf("Trying to install an unexisting snap did not exit with an error"))
	c.Assert(string(output), testutil.Contains,
		"error: cannot perform the following tasks:\n"+
			"- Download snap \"unexisting.canonical\" from channel \"stable\" (snap not found)\n",
		check.Commentf("Wrong error message"))
}

func (s *installAppSuite) TestInstallFromStoreMetadata(c *check.C) {
	c.Skip("FIXME: enable when we have snap info")

	common.InstallSnap(c, "hello-world")
	defer common.RemoveSnap(c, "hello-world")

	output := cli.ExecCommand(c, "snap", "info", "hello-world")
	c.Check(string(output), check.Matches, "(?ms)^channel: edge")
}
