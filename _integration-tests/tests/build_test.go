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
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/build"
	"launchpad.net/snappy/_integration-tests/testutils/common"
	"launchpad.net/snappy/_integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&buildSuite{})

type buildSuite struct {
	common.SnappySuite
}

func (s *buildSuite) TestBuildBasicSnapOnSnappy(c *check.C) {
	// build basic snap and check output
	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))

	// install built snap and check output
	installOutput := common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicSnapName)
	expected := "(?ms)" +
		"Installing " + snapPath + "\n" +
		".*Signature check failed, but installing anyway as requested\n" +
		"Name +Date +Version +Developer \n" +
		".*\n" +
		data.BasicSnapName + " +.* +.* +sideload  \n" +
		".*\n"

	c.Check(installOutput, check.Matches, expected)
}

func (s *buildSuite) TestBuildWrongYamlSnapOnSnappy(c *check.C) {
	commonWrongTest(c, data.WrongYamlSnapName,
		"(?msi).*Can not parse.*yaml: line 2: mapping values are not allowed in this context.*")
}

func (s *buildSuite) TestBuildMissingReadmeSnapOnSnappy(c *check.C) {
	commonWrongTest(c, data.MissingReadmeSnapName,
		".*readme.md: no such file or directory\n")
}

func commonWrongTest(c *check.C, testName, expected string) {
	// build wrong snap and check error
	cmd := exec.Command("snappy", "build", fmt.Sprintf("%s/%s", data.BaseSnapPath, testName))
	echoOutput, err := cmd.CombinedOutput()
	c.Assert(err, check.NotNil, check.Commentf("%s should not be built", testName))

	c.Assert(string(echoOutput), check.Matches, expected)
}
