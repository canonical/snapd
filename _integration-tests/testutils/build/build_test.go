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

package build

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type BuildSuite struct {
	execCalls        map[string]int
	execReturnValues []string
	execReturnIndex  int
	backExecCommand  func(...string) error

	mkDirCalls           map[string]int
	backPrepareTargetDir func(string)

	osRenameCalls map[string]int
	backOsRename  func(string, string) error

	osSetenvCalls map[string]int
	backOsSetenv  func(string, string) error

	osGetenvCalls map[string]int
	backOsGetenv  func(string) string

	useSnappyFromBranch bool
	arch                string

	environ map[string]string
}

var _ = check.Suite(&BuildSuite{})

func (s *BuildSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	s.backPrepareTargetDir = prepareTargetDir
	s.backOsRename = osRename
	s.backOsSetenv = osSetenv
	s.backOsGetenv = osGetenv

	execCommand = s.fakeExecCommand
	prepareTargetDir = s.fakePrepareTargetDir
	osRename = s.fakeOsRename
	osSetenv = s.fakeOsSetenv
	osGetenv = s.fakeOsGetenv
}

func (s *BuildSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
	prepareTargetDir = s.backPrepareTargetDir
	osRename = s.backOsRename
	osSetenv = s.backOsSetenv
	osGetenv = s.backOsGetenv
}

func (s *BuildSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.mkDirCalls = make(map[string]int)
	s.osRenameCalls = make(map[string]int)
	s.osSetenvCalls = make(map[string]int)
	s.osGetenvCalls = make(map[string]int)
	s.environ = make(map[string]string)
}

func (s *BuildSuite) fakeExecCommand(args ...string) (err error) {
	s.execCalls[strings.Join(args, " ")]++
	return
}

func (s *BuildSuite) fakePrepareTargetDir(path string) {
	s.mkDirCalls[path]++
}

func (s *BuildSuite) fakeOsRename(orig, dest string) (err error) {
	s.osRenameCalls[orig+" "+dest]++
	return
}

func (s *BuildSuite) fakeOsSetenv(key, value string) (err error) {
	s.osSetenvCalls[key+" "+value]++
	s.environ[key] = value
	return
}

func (s *BuildSuite) fakeOsGetenv(key string) (value string) {
	s.osGetenvCalls[key]++
	return s.environ[key]
}

func (s *BuildSuite) TestAssetsCallsPrepareDir(c *check.C) {
	Assets(s.useSnappyFromBranch, s.arch)

	mkDirCall := s.mkDirCalls[testsBinDir]

	c.Assert(mkDirCall, check.Equals, 1,
		check.Commentf("Expected 1 call to mkDir with %s, got %d",
			testsBinDir, mkDirCall))
}

func (s *BuildSuite) TestAssetsBuildsTests(c *check.C) {
	Assets(s.useSnappyFromBranch, s.arch)

	buildCall := s.execCalls[buildTestCmd]

	fmt.Println(s.execCalls)
	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			buildTestCmd, buildCall))
}

func (s *BuildSuite) TestAssetsRenamesBuiltBinary(c *check.C) {
	Assets(s.useSnappyFromBranch, s.arch)

	cmd := "tests.test " + testsBinDir + IntegrationTestName
	renameCall := s.osRenameCalls[cmd]

	c.Assert(renameCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Rename with %s, got %d",
			cmd, renameCall))
}

func (s *BuildSuite) TestAssetsSetsEnvironmentForGenericArch(c *check.C) {
	arch := "myarch"
	s.environ["GOARCH"] = os.Getenv("GOARCH")
	Assets(s.useSnappyFromBranch, arch)

	setenvGOARCHFirstCall := s.osSetenvCalls["GOARCH "+arch]
	setenvGOARCHFinalCall := s.osSetenvCalls["GOARCH "+os.Getenv("GOARCH")]

	c.Assert(setenvGOARCHFirstCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
			"GOARCH "+arch, setenvGOARCHFirstCall))
	c.Assert(setenvGOARCHFinalCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
			"GOARCH "+os.Getenv("GOARCH"), setenvGOARCHFinalCall))
}

var armEnvironmentTests = []struct {
	envVar string
	value  string
}{
	{"GOARM", defaultGoArm},
	{"CGO_ENABLED", "1"},
	{"CC", "arm-linux-gnueabihf-gcc"},
}

func (s *BuildSuite) TestAssetsSetsEnvironmentForArm(c *check.C) {
	arch := "arm"
	for _, t := range armEnvironmentTests {
		s.environ[t.envVar] = "original" + t.envVar
	}
	Assets(s.useSnappyFromBranch, arch)

	for _, t := range armEnvironmentTests {
		firstCall := fmt.Sprintf("%s %s", t.envVar, t.value)
		setenvFirstCall := s.osSetenvCalls[firstCall]
		finalCall := fmt.Sprintf("%s %s", t.envVar, "original"+t.envVar)
		setenvFinalCall := s.osSetenvCalls[finalCall]

		c.Assert(setenvFirstCall, check.Equals, 1,
			check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
				firstCall, setenvFirstCall))
		c.Assert(setenvFinalCall, check.Equals, 1,
			check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
				finalCall, setenvFinalCall))
	}
}

func (s *BuildSuite) TestAssetsDoesNotSetEnvironmentForEmptyArch(c *check.C) {
	Assets(s.useSnappyFromBranch, s.arch)

	setenvGOARCHFirstCall := s.osSetenvCalls["GOARCH "]
	setenvGOARCHFinalCall := s.osSetenvCalls["GOARCH "+os.Getenv("GOARCH")]

	c.Assert(setenvGOARCHFirstCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARCH ", setenvGOARCHFirstCall))
	c.Assert(setenvGOARCHFinalCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARCH "+os.Getenv("GOARCH"), setenvGOARCHFinalCall))
}

func (s *BuildSuite) TestAssetsDoesNotSetEnvironmentForNonArm(c *check.C) {
	arch := "not-arm"
	Assets(s.useSnappyFromBranch, arch)

	setenvGOARMFirstCall := s.osSetenvCalls["GOARM "+defaultGoArm]
	setenvGOARMFinalCall := s.osSetenvCalls["GOARM "+os.Getenv("GOARM")]

	c.Assert(setenvGOARMFirstCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARM "+arch, setenvGOARMFirstCall))
	c.Assert(setenvGOARMFinalCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARM "+os.Getenv("GOARCH"), setenvGOARMFinalCall))
}

func (s *BuildSuite) TestAssetsBuildsSnappyFromBranch(c *check.C) {
	buildSnappyFromBranch := true
	Assets(buildSnappyFromBranch, s.arch)

	buildCall := s.execCalls[buildSnappyCmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			buildSnappyCmd, buildCall))
}

func (s *BuildSuite) TestAssetsDoesNotBuildSnappyFromBranchIfNotInstructedTo(c *check.C) {
	Assets(s.useSnappyFromBranch, s.arch)

	buildCall := s.execCalls[buildSnappyCmd]

	c.Assert(buildCall, check.Equals, 0,
		check.Commentf("Expected 0 call to execCommand with %s, got %d",
			buildSnappyCmd, buildCall))
}
