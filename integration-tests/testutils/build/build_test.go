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

package build

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type BuildSuite struct {
	execCalls       map[string]int
	execCallsDirs   map[string]string
	execReturnList  string
	backExecCommand func(*exec.Cmd) (string, error)

	mkDirCalls           map[string]int
	backPrepareTargetDir func(string)

	osRenameCalls map[string]int
	backOsRename  func(string, string) error

	osSetenvCalls map[string]int
	backOsSetenv  func(string, string) error

	osGetenvCalls map[string]int
	backOsGetenv  func(string) string

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
	s.execCallsDirs = make(map[string]string)
	s.mkDirCalls = make(map[string]int)
	s.osRenameCalls = make(map[string]int)
	s.osSetenvCalls = make(map[string]int)
	s.osGetenvCalls = make(map[string]int)
	s.environ = make(map[string]string)
	s.execReturnList = ""
}

func (s *BuildSuite) fakeExecCommand(cmd *exec.Cmd) (out string, err error) {
	execCall := strings.Join(cmd.Args, " ")
	s.execCalls[execCall]++
	s.execCallsDirs[execCall] = cmd.Dir

	if strings.HasPrefix(execCall, "go list") {
		fmt.Fprint(os.Stdout, s.execReturnList)
		out = s.execReturnList
	}
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
	Assets(nil)

	mkDirCall := s.mkDirCalls[testsBinDir]

	c.Assert(mkDirCall, check.Equals, 1,
		check.Commentf("Expected 1 call to mkDir with %s, got %d",
			testsBinDir, mkDirCall))
}

func (s *BuildSuite) TestAssetsBuildsTests(c *check.C) {
	Assets(nil)

	// not passing test build tags by default
	testBuildTags := ""
	cmd := fmt.Sprintf(buildTestCmdFmt, testBuildTags)
	buildCall := s.execCalls[cmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			cmd, buildCall))
}

func (s *BuildSuite) TestAssetsRenamesBuiltBinary(c *check.C) {
	Assets(nil)

	cmd := "tests.test " + testsBinDir + IntegrationTestName
	renameCall := s.osRenameCalls[cmd]

	c.Assert(renameCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Rename with %s, got %d",
			cmd, renameCall))
}

func (s *BuildSuite) TestAssetsSetsEnvironmentForGenericArch(c *check.C) {
	arch := "myarch"
	originalArch := "originalArch"
	s.environ["GOARCH"] = originalArch
	Assets(&Config{Arch: arch})

	setenvGOARCHFirstCall := s.osSetenvCalls["GOARCH "+arch]
	setenvGOARCHFinalCall := s.osSetenvCalls["GOARCH "+originalArch]

	c.Assert(setenvGOARCHFirstCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
			"GOARCH "+arch, setenvGOARCHFirstCall))
	c.Assert(setenvGOARCHFinalCall, check.Equals, 1,
		check.Commentf("Expected 1 call to os.Setenv with %s, got %d",
			"GOARCH "+originalArch, setenvGOARCHFinalCall))
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
	Assets(&Config{Arch: arch})

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
	Assets(nil)

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
	Assets(&Config{Arch: arch})

	setenvGOARMFirstCall := s.osSetenvCalls["GOARM "+defaultGoArm]
	setenvGOARMFinalCall := s.osSetenvCalls["GOARM "+os.Getenv("GOARM")]

	c.Assert(setenvGOARMFirstCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARM "+arch, setenvGOARMFirstCall))
	c.Assert(setenvGOARMFinalCall, check.Equals, 0,
		check.Commentf("Expected 0 calls to os.Setenv with %s, got %d",
			"GOARM "+os.Getenv("GOARCH"), setenvGOARMFinalCall))
}

func (s *BuildSuite) TestAssetsBuildsSnappyCliFromBranch(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	buildSnappyCliCmd := getBinaryBuildCmd("snappy", "-coverpkg")
	buildCall := s.execCalls[buildSnappyCliCmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			buildSnappyCliCmd, buildCall))
}

func (s *BuildSuite) TestAssetsDoesNotBuildSnappyCliFromBranchIfNotInstructedTo(c *check.C) {
	Assets(nil)

	buildSnappyCliCmd := getBinaryBuildCmd("snappy", "coverpkg")
	buildCall := s.execCalls[buildSnappyCliCmd]

	c.Assert(buildCall, check.Equals, 0,
		check.Commentf("Expected 0 call to execCommand with %s, got %d",
			buildSnappyCliCmd, buildCall))
}

func (s *BuildSuite) TestAssetsBuildsSnapdFromBranch(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	buildSnapdCmd := getBinaryBuildCmd("snapd", "-coverpkg")
	buildCall := s.execCalls[buildSnapdCmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			buildSnapdCmd, buildCall))
}

func (s *BuildSuite) TestAssetsDoesNotBuildSnapdFromBranchIfNotInstructedTo(c *check.C) {
	Assets(nil)

	buildSnapdCmd := getBinaryBuildCmd("snapd", "-coverpkg")
	buildCall := s.execCalls[buildSnapdCmd]

	c.Assert(buildCall, check.Equals, 0,
		check.Commentf("Expected 0 call to execCommand with %s, got %d",
			buildSnapdCmd, buildCall))
}

func (s *BuildSuite) TestAssetsBuildsSnapFromBranch(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	buildSnapCliCmd := getBinaryBuildCmd("snap", "-coverpkg")
	buildCall := s.execCalls[buildSnapCliCmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			buildSnapCliCmd, buildCall))
}

func (s *BuildSuite) TestAssetsDoesNotBuildSnapFromBranchIfNotInstructedTo(c *check.C) {
	Assets(nil)

	buildSnapCliCmd := getBinaryBuildCmd("snap", "-coverpkg")
	buildCall := s.execCalls[buildSnapCliCmd]

	c.Assert(buildCall, check.Equals, 0,
		check.Commentf("Expected 0 call to execCommand with %s, got %d",
			buildSnapCliCmd, buildCall))
}

func (s *BuildSuite) TestAssetsHonoursBuildTags(c *check.C) {
	testBuildTags := "mybuildtag"
	Assets(&Config{TestBuildTags: testBuildTags})

	tagBuildTestCmd := fmt.Sprintf(buildTestCmdFmt, " -tags=mybuildtag")
	buildCall := s.execCalls[tagBuildTestCmd]

	c.Assert(buildCall, check.Equals, 1,
		check.Commentf("Expected 1 call to execCommand with %s, got %d",
			tagBuildTestCmd, buildCall))
}

func (s *BuildSuite) TestBuildCmdIncludesTestCommand(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	cmdsFound := s.checkBuildCmd(`go test .*cmd\/snap[py|d]?`)

	c.Assert(cmdsFound, check.Equals, true)
}

func (s *BuildSuite) TestBuildCmdIncludesCoverpkg(c *check.C) {
	var items = []struct {
		returnList string
		expected   string
	}{
		{"pkg1\npkg2", "-coverpkg pkg1,pkg2"},
		{"dom1.com/user/pkg1\ndom1.com/user/pkg2\ndom1.com/user/pkg3", "-coverpkg dom1.com/user/pkg1,dom1.com/user/pkg2,dom1.com/user/pkg3"},
		{"pkg1", `\-coverpkg pkg1`},
	}

	for _, item := range items {
		s.execCalls = make(map[string]int)
		s.execReturnList = item.returnList

		Assets(&Config{UseSnappyFromBranch: true})

		cmdsFound := s.checkBuildCmd(".* " + item.expected)
		c.Check(s.execCalls[listCmd], check.Equals, 1)
		c.Check(cmdsFound, check.Equals, true)
	}
}

func (s *BuildSuite) TestBuildCmdCallsListCommandFromProjectRoot(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	c.Assert(s.execCalls[listCmd], check.Equals, 1)
	c.Assert(s.execCallsDirs[listCmd], check.Equals,
		filepath.Join(os.Getenv("GOPATH"), projectSrcPath))
}

func (s *BuildSuite) TestBuildCmdDoesNotIncludeFilteredPkgs(c *check.C) {
	var items = []struct {
		returnList string
		expected   string
	}{
		{"integration-tests/pkg1\nanotherpkg/pkg2\ninitialpkg/integration-tests/pkg3", "-coverpkg anotherpkg/pkg2"},
		{"elper/pkg1\ninitialpkg/helper/pkg3\nanotherpkg/pkg2", "-coverpkg elper/pkg1,anotherpkg/pkg2"},
		{"mypkg/pkg4\nosuti/pkg1\ninitialpkg/osutil/pkg3\nanotherone/pkg5", "-coverpkg mypkg/pkg4,osuti/pkg1,anotherone/pkg5"},
		{"mypkg/pkg\nprogres/pkg1\ninitialpkg/helper/pkg3\nanotherone/pk4", "-coverpkg mypkg/pkg,progres/pkg1,anotherone/pk4"},
	}

	for _, item := range items {
		s.execCalls = make(map[string]int)
		s.execReturnList = item.returnList
		Assets(&Config{UseSnappyFromBranch: true})

		cmdsFound := s.checkBuildCmd(".* " + item.expected)
		c.Check(cmdsFound, check.Equals, true)
	}
}

func (s *BuildSuite) TestBuildCmdExecutesBuildCommandsFromGOPATH(c *check.C) {
	Assets(&Config{UseSnappyFromBranch: true})

	for _, bin := range []string{"snappy", "snapd", "snap"} {
		cmd := getBinaryBuildCmd(bin, "-coverpkg")

		c.Check(s.execCalls[cmd], check.Equals, 1)
		c.Check(s.execCallsDirs[cmd], check.Equals,
			filepath.Join(os.Getenv("GOPATH"), projectSrcPath))
	}
}

func (s *BuildSuite) checkBuildCmd(pattern string) bool {
	re := regexp.MustCompile(pattern)
	buildTestCmd := fmt.Sprintf(buildTestCmdFmt, "")
	cmdsFound := true
	for cmd := range s.execCalls {
		if cmd != buildTestCmd && cmd != listCmd {
			cmdsFound = cmdsFound && (re.FindStringIndex(cmd) != nil)
		}
	}
	return cmdsFound
}
