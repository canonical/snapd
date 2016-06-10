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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/integration-tests/testutils/build"
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
	"github.com/snapcore/snapd/integration-tests/testutils/data"
	"github.com/snapcore/snapd/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installSuite{})

type installSuite struct {
	common.SnappySuite
}

func (s *installSuite) TestInstallAppMustPrintPackageInformation(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	installOutput := common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicSnapName)

	expected := "(?ms)" +
		"Name +Version +Rev +Developer +Notes\n" +
		".*" +
		"^basic +.* *\n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}

func (s *installSuite) TestCallSuccessfulBinaryFromInstalledSnap(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	// Exec command does not fail.
	cli.ExecCommand(c, "basic-binaries.success")
}

func (s *installSuite) TestCallFailBinaryFromInstalledSnap(c *check.C) {
	c.Skip("port to snapd")

	snapPath, err := build.LocalSnap(c, data.BasicBinariesSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	_, err = cli.ExecCommandErr("basic-binaries.fail")
	c.Assert(err, check.NotNil, check.Commentf("The binary did not fail"))
}

func (s *installSuite) TestInstallUnexistingAppMustPrintError(c *check.C) {
	output, err := cli.ExecCommandErr("sudo", "snap", "install", "unexisting.canonical")

	c.Check(err, check.NotNil,
		check.Commentf("Trying to install an unexisting snap did not exit with an error"))
	c.Assert(string(output), testutil.Contains,
		"error: cannot perform the following tasks:\n"+
			"- Download snap \"unexisting.canonical\" from channel \"stable\" (snap not found)\n",
		check.Commentf("Wrong error message"))
}

// SNAP_INSTALL_001: --help - print detailed help text for the install command
func (s *installSuite) TestInstallShowHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] install.*\n` +
		"\n^The install command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "install", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_INSTALL_002: without snap name shows error
func (s *installSuite) TestInstallWithoutSnapNameMustPrintError(c *check.C) {
	expected := "error: the required argument `<snap>` was not provided\n"

	actual, err := cli.ExecCommandErr("snap", "install")

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

// SNAP_INSTALL_004: with already installed snap name and same version
func (s *installSuite) TestInstallWithAlreadyInstalledSnapAndSameVersionMustFail(c *check.C) {
	snapName := "hello-world"

	common.InstallSnap(c, snapName)
	defer common.RemoveSnap(c, snapName)

	expected := fmt.Sprintf(`error: cannot install "%s": snap "%[1]s" already installed\n`, snapName)
	actual, err := cli.ExecCommandErr("sudo", "snap", "install", snapName)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

// SNAP_INSTALL_008: from different channel other than default
func (s *installSuite) TestInstallFromDifferentChannels(c *check.C) {
	snapName := "hello-world"

	expected := "(?ms).*\n" +
		"Name +Version +Rev +Developer +Notes\n" +
		snapName + " .* canonical +-\n"

	for _, channel := range []string{"edge", "beta", "candidate", "stable"} {
		actual := cli.ExecCommand(c, "sudo", "snap", "install", snapName, "--channel="+channel)

		c.Check(actual, check.Matches, expected)

		common.RemoveSnap(c, snapName)
	}
}

// SNAP_INSTALL_009: with devmode option
func (s *installSuite) TestInstallWithDevmodeOption(c *check.C) {
	snapName := "hello-world"

	expected := "(?ms).*\n" +
		"Name +Version +Rev +Developer +Notes\n" +
		snapName + " .* canonical +devmode\n"

	actual := cli.ExecCommand(c, "sudo", "snap", "install", snapName, "--devmode")
	defer common.RemoveSnap(c, snapName)

	c.Assert(actual, check.Matches, expected)
}

func (s *installSuite) TestInstallsDesktopFile(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicDesktopSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicDesktopSnapName)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapDesktopFilesDir, "basic-desktop_echo.desktop"))
	c.Assert(err, check.IsNil)
	c.Assert(string(content), testutil.Contains, `[Desktop Entry]
Name=Echo
Comment=It echos stuff
Exec=/snap/bin/basic-desktop.echo
`)
}

// regression test for lp #1574829
func (s *installSuite) TestInstallsPointsToLoginWhenNotAuthenticated(c *check.C) {
	cli.ExecCommandErr("snap", "logout")

	expected := ".*snap login --help.*\n"

	actual, err := cli.ExecCommandErr("snap", "install", "hello-world")

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}
