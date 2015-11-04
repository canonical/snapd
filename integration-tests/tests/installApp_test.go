// -*- Mode: Go; indent-tabs-mode: t -*-
// +build integration

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
	"os/exec"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

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
		fmt.Sprintf("Installing %s\n", snapPath) +
		".*Signature check failed, but installing anyway as requested\n" +
		"Name +Date +Version +Developer \n" +
		".*" +
		"^basic +.* +.* +sideload *\n" +
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
	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	_, err = cli.ExecCommandErr("basic-binaries.fail")
	c.Assert(err, check.NotNil, check.Commentf("The binary did not fail"))
}

func (s *installAppSuite) TestInstallUnexistingAppMustPrintError(c *check.C) {
	cmd := exec.Command("sudo", "snappy", "install", "unexisting.canonical")
	output, err := cmd.CombinedOutput()

	c.Check(err, check.NotNil,
		check.Commentf("Trying to install an unexisting snap did not exit with an error"))
	c.Assert(string(output), check.Equals,
		"Installing unexisting.canonical\n"+
			"unexisting failed to install: snappy package not found\n",
		check.Commentf("Wrong error message"))
}
