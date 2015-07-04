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

package latest

import (
	"fmt"
	"os"
	"os/exec"

	. "../common"

	. "gopkg.in/check.v1"
)

const (
	baseSnapPath          = "_integration-tests/data/snaps"
	basicSnapName         = "basic"
	wrongYamlSnapName     = "wrong-yaml"
	missingReadmeSnapName = "missing-readme"
)

var _ = Suite(&buildSuite{})

type buildSuite struct {
	SnappySuite
}

func buildSnap(c *C, snapPath string) string {
	return ExecCommand(c, "snappy", "build", snapPath)
}

func (s *buildSuite) TestBuildBasicSnapOnSnappy(c *C) {
	// build basic snap and check output
	snapPath := baseSnapPath + "/" + basicSnapName
	buildOutput := buildSnap(c, snapPath)
	snapName := basicSnapName + "_1.0_all.snap"
	expected := fmt.Sprintf("Generated '%s' snap\n", snapName)
	c.Check(buildOutput, Equals, expected)
	defer os.Remove(snapPath + "/" + snapName)

	// install built snap and check output
	installOutput := InstallSnap(c, snapName)
	defer RemoveSnap(c, basicSnapName)
	expected = "" +
		"Installing " + snapName + "\n" +
		".*Signature check failed, but installing anyway as requested\n" +
		"Name          Date       Version Developer \n" +
		".*\n" +
		basicSnapName + "   .* .*  sideload  \n" +
		".*\n"

	c.Check(installOutput, Matches, expected)

	// teardown, remove snap file
	c.Assert(os.Remove(snapName), IsNil, Commentf("Error removing %s", snapName))
}

func (s *buildSuite) TestBuildWrongYamlSnapOnSnappy(c *C) {
	commonWrongTest(c, wrongYamlSnapName, "can not parse package.yaml:.*\n")
}

func (s *buildSuite) TestBuildMissingReadmeSnapOnSnappy(c *C) {
	commonWrongTest(c, missingReadmeSnapName, ".*readme.md: no such file or directory\n")
}

func commonWrongTest(c *C, testName, expected string) {
	// build wrong snap and check output
	cmd := exec.Command("snappy", "build", fmt.Sprintf("%s/%s", baseSnapPath, testName))
	echoOutput, err := cmd.CombinedOutput()
	c.Assert(err, NotNil, Commentf("%s should not be built", testName))

	c.Assert(string(echoOutput), Matches, expected)
}
