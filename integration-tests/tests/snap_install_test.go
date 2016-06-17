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
