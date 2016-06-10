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

package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snapcore/snapd/integration-tests/testutils/config"
	"github.com/snapcore/snapd/integration-tests/testutils/testutils"

	"gopkg.in/check.v1"
)

const (
	execCmd       = "mycmd"
	execOutput    = "myoutput\n"
	defaultDir    = "/tmp"
	defaultEnvVar = "VAR"
	defaultEnvVal = "VAL"
	defaultEnv    = defaultEnvVar + "=" + defaultEnvVal
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type cliTestSuite struct {
	backExecCommand  func(string, ...string) *exec.Cmd
	helperProcess    string
	execArgs         []string
	backAdtArtifacts string
	tmpDir           string
	cmd              *exec.Cmd
	configFile       string
	targetCoverCmds  []string
}

var _ = check.Suite(&cliTestSuite{})

func (s *cliTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
	s.configFile = config.DefaultFileName
	s.targetCoverCmds = []string{"snappy", "snap", "snapd"}
}

func (s *cliTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
}

func (s *cliTestSuite) SetUpTest(c *check.C) {
	s.helperProcess = "TestHelperProcess"
	s.cmd = execCommand(execCmd)
	s.execArgs = []string{}
	s.backAdtArtifacts = os.Getenv("ADT_ARTIFACTS")

	var err error
	s.tmpDir, err = ioutil.TempDir("", "")
	c.Assert(err, check.IsNil)

	os.Setenv("ADT_ARTIFACTS", s.tmpDir)

	err = s.writeFromBranchConfig(true)
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TearDownTest(c *check.C) {
	os.Setenv("ADT_ARTIFACTS", s.backAdtArtifacts)
	os.Remove(s.tmpDir)
	s.cmd = execCommand(execCmd)
	// in these unit tests the config file is created under
	// integration-tests/testutils/cli/integration-tests/data/output, we
	// need to remove the integration-tests dir under cli
	err := os.RemoveAll(filepath.Dir(filepath.Dir(filepath.Dir(s.configFile))))
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) fakeExecCommand(command string, args ...string) *exec.Cmd {
	s.execArgs = args
	cs := []string{"-check.f=cliTestSuite." + s.helperProcess, "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func (s *cliTestSuite) TestHelperProcess(c *check.C) {
	baseHelperProcess(0, execOutput)
}

func (s *cliTestSuite) TestHelperProcessErr(c *check.C) {
	baseHelperProcess(1, execOutput)
}

func (s *cliTestSuite) TestHelperProcessCoverage(c *check.C) {
	baseHelperProcess(0, execOutput+"PASS\ncoverage: blabla\n")
}

func (s *cliTestSuite) TestHelperProcessCoverageEmptyOutput(c *check.C) {
	baseHelperProcess(0, "PASS\ncoverage: blabla\n")
}

func baseHelperProcess(exitValue int, output string) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	dir, _ := os.Getwd()
	env := []string{}
	val := os.Getenv(defaultEnvVar)
	if val != "" {
		env = []string{"GO_WANT_HELPER_PROCESS=1", defaultEnvVar + "=" + val}
	}
	fmt.Fprintf(os.Stdout, getParamOuput(output, &exec.Cmd{Dir: dir, Env: env}))
	os.Exit(exitValue)
}

func (s *cliTestSuite) TestExecCommand(c *check.C) {
	actualOutput := ExecCommand(c, execCmd)

	c.Assert(actualOutput, check.Equals, execOutput)
}

func (s *cliTestSuite) TestExecCommandAddsCoverageOptionsToTargetCmd(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		ExecCommand(c, cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) &&
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, true)
	}
}

func (s *cliTestSuite) TestExecCommandAddsCoverageOptionsToComplexTargetCmd(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		ExecCommand(c, "sudo", "TMPDIR=/var/tmp", cmd,
			"build", "--squashfs", "/var/tmp/snap-build-633342069")

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) &&
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, true)
	}
}

func (s *cliTestSuite) TestExecCommandDoesNotAddCoverageOptionsToNonTargetCmd(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		for _, badCmd := range []string{cmd + "mycmd", "mycmd" + cmd} {
			ExecCommand(c, badCmd)

			c.Check(checkPrefixInSlice("-test.run", s.execArgs) ||
				checkPrefixInSlice("-test.coverprofile", s.execArgs),
				check.Equals, false)
		}
	}
}

func (s *cliTestSuite) TestExecCommandDoesNotAddCoverageOptionsToTargetCmdWhenNotBuiltFromBranch(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		s.writeFromBranchConfig(false)
		s.execArgs = []string{}

		ExecCommand(c, cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) ||
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, false)

	}
}

func (s *cliTestSuite) TestExecCommandToFile(c *check.C) {
	outputFile, err := ioutil.TempFile("", "snappy-exec")
	c.Assert(err, check.IsNil)
	outputFile.Close()
	defer os.Remove(outputFile.Name())

	ExecCommandToFile(c, outputFile.Name(), execCmd)

	actualFileContents, err := ioutil.ReadFile(outputFile.Name())
	c.Assert(err, check.IsNil)
	c.Assert(string(actualFileContents), check.Equals, execOutput)
}

func (s *cliTestSuite) TestExecCommandToFileAddsCoverageOptionsToSnappyCmd(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		outputFile, err := ioutil.TempFile("", "snappy-exec")
		c.Check(err, check.IsNil)
		outputFile.Close()
		defer os.Remove(outputFile.Name())

		ExecCommandToFile(c, outputFile.Name(), cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) &&
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, true)
	}
}

func (s *cliTestSuite) TestExecCommandToFileDoesNotAddCoverageOptionsToNonSnappyCmd(c *check.C) {
	outputFile, err := ioutil.TempFile("", "snappy-exec")
	c.Assert(err, check.IsNil)
	outputFile.Close()
	defer os.Remove(outputFile.Name())

	ExecCommandToFile(c, outputFile.Name(), "mycmd")

	c.Assert(checkPrefixInSlice("-test.run", s.execArgs) ||
		checkPrefixInSlice("-test.coverprofile", s.execArgs),
		check.Equals, false)
}

func (s *cliTestSuite) TestExecCommandToFileDoesNotAddCoverageOptionsToSnappyCmdWhenNotBuiltFromBranch(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		s.writeFromBranchConfig(false)
		s.execArgs = []string{}

		outputFile, err := ioutil.TempFile("", "snappy-exec")
		c.Check(err, check.IsNil)
		outputFile.Close()
		defer os.Remove(outputFile.Name())

		ExecCommandToFile(c, outputFile.Name(), cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) ||
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, false)
	}
}

func (s *cliTestSuite) TestExecCommandErr(c *check.C) {
	actualOutput, err := ExecCommandErr(execCmd)

	c.Assert(actualOutput, check.Equals, execOutput)
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandErrWithError(c *check.C) {
	s.helperProcess = "TestHelperProcessErr"
	actualOutput, err := ExecCommandErr(execCmd)

	c.Assert(actualOutput, check.Equals, execOutput)
	c.Assert(err, check.NotNil)
}

func (s *cliTestSuite) TestExecCommandErrAddsCoverageOptionsToSnappyCmd(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		ExecCommandErr(cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) &&
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, true)
	}
}

func (s *cliTestSuite) TestExecCommandErrDoesNotAddCoverageOptionsToNonSnappyCmd(c *check.C) {
	ExecCommandErr("mycmd")

	c.Assert(checkPrefixInSlice("-test.run", s.execArgs) ||
		checkPrefixInSlice("-test.coverprofile", s.execArgs),
		check.Equals, false)
}

func (s *cliTestSuite) TestExecCommandErrDoesNotAddCoverageOptionsToSnappyCmdWhenNotBuiltFromBranch(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		s.writeFromBranchConfig(false)
		s.execArgs = []string{}

		ExecCommandErr(cmd)

		c.Check(checkPrefixInSlice("-test.run", s.execArgs) ||
			checkPrefixInSlice("-test.coverprofile", s.execArgs),
			check.Equals, false)
	}
}

func (s *cliTestSuite) TestExecCommandRemovesCoverageLines(c *check.C) {
	s.helperProcess = "TestHelperProcessCoverage"
	for _, cmd := range s.targetCoverCmds {
		actualOutput := ExecCommand(c, cmd)

		c.Check(actualOutput, check.Equals, execOutput)
	}
}

func (s *cliTestSuite) TestExecCommandRemovesCoverageLinesForEmptyOutput(c *check.C) {
	s.helperProcess = "TestHelperProcessCoverageEmptyOutput"
	for _, cmd := range s.targetCoverCmds {
		actualOutput := ExecCommand(c, cmd)

		c.Check(actualOutput, check.Equals, "\n")
	}
}

func (s *cliTestSuite) TestExecCommandCreatesCoverageDir(c *check.C) {
	s.helperProcess = "TestHelperProcessCoverage"
	for _, cmd := range s.targetCoverCmds {
		ExecCommand(c, cmd)

		expectedDir := filepath.Join(s.tmpDir, "coverage")
		_, err := os.Stat(expectedDir)
		c.Check(err, check.IsNil)
	}
}

func (s *cliTestSuite) TestExecCommandReturnsCreateCoverageDirError(c *check.C) {
	targetDir := filepath.Join(s.tmpDir, "existingDir")
	err := os.Mkdir(targetDir, os.ModeDir)
	c.Assert(err, check.IsNil)

	os.Setenv("ADT_ARTIFACTS", targetDir)

	s.helperProcess = "TestHelperProcessCoverage"
	for _, cmd := range s.targetCoverCmds {
		_, err = ExecCommandErr(cmd)

		c.Check(err, check.NotNil)
		c.Check(err, check.FitsTypeOf, &os.PathError{})
	}
}

func (s *cliTestSuite) TestExecCommandCoverageFilesDontDuplicateCoverageDir(c *check.C) {
	s.helperProcess = "TestHelperProcessCoverage"
	for _, cmd := range s.targetCoverCmds {
		ExecCommand(c, cmd)

		coverFile := strings.Split(s.execArgs[1], "=")[1]

		c.Check(strings.Count(coverFile, getCoveragePath()), check.Equals, 1)
	}
}

func (s *cliTestSuite) TestExecCommandCreatesOneCoverageFilePerCall(c *check.C) {
	s.helperProcess = "TestHelperProcessCoverage"

	expectedTotal := 3

	for _, cmd := range s.targetCoverCmds {
		coverFileNames := []string{}

		for i := 0; i < expectedTotal; i++ {
			ExecCommand(c, cmd)
			// s.execArgs is of the form:
			// [-test.run=^TestRunMain$ -test.coverprofile=/tmp/832295861/coverage/coverage.out]
			coverFile := strings.Split(s.execArgs[1], "=")[1]
			coverFileNames = append(coverFileNames, filepath.Base(coverFile))
		}
		c.Check(coverFileNames[0] != coverFileNames[1], check.Equals, true)
		c.Check(coverFileNames[1] != coverFileNames[2], check.Equals, true)
		c.Check(coverFileNames[0] != coverFileNames[2], check.Equals, true)
	}
}

func checkPrefixInSlice(prefix string, elements []string) bool {
	for _, item := range elements {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}

func (s *cliTestSuite) TestExecCommandWrapperHonoursDir(c *check.C) {
	s.cmd.Dir = defaultDir
	actualOutput, err := ExecCommandWrapper(s.cmd)

	c.Assert(actualOutput, check.Equals, getParamOuput(execOutput, s.cmd))
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandWrapperHonoursEnv(c *check.C) {
	s.cmd.Env = append(s.cmd.Env, defaultEnv)
	actualOutput, err := ExecCommandWrapper(s.cmd)

	c.Assert(actualOutput, check.Equals, getParamOuput(execOutput, s.cmd))
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandWrapperReturnsErr(c *check.C) {
	s.helperProcess = "TestHelperProcessErr"

	_, err := ExecCommandWrapper(s.cmd)

	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestAddOptionsToSnappyCommand(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		cmdsIn := []string{cmd, "subcommand"}

		cmdsOut, err := AddOptionsToCommand(cmdsIn)

		c.Check(err, check.IsNil)
		c.Check(len(cmdsOut), check.Equals, 4)
		c.Check(cmdsOut[0], check.Equals, cmd)
		c.Check(cmdsOut[3], check.Equals, "subcommand")
		c.Check(cmdsOut[1], check.Equals, "-test.run=^TestRunMain$")
		c.Check(strings.HasPrefix(cmdsOut[2], "-test.coverprofile="), check.Equals, true)
	}
}

func (s *cliTestSuite) TestAddOptionsDoesNotModifyOriginalCmds(c *check.C) {
	for _, cmd := range s.targetCoverCmds {
		cmdsIn := []string{cmd, "subcommand1", "subcommand2"}

		_, err := AddOptionsToCommand(cmdsIn)

		c.Check(err, check.IsNil)
		c.Check(len(cmdsIn), check.Equals, 3)
		c.Check(cmdsIn[0], check.Equals, cmd)
		c.Check(cmdsIn[1], check.Equals, "subcommand1")
		c.Check(cmdsIn[2], check.Equals, "subcommand2")
	}
}

func (s *cliTestSuite) TestExecCommandWrapperDoesNotWriteVerboseOutputByDefault(c *check.C) {
	backStdout := os.Stdout
	defer func() { os.Stdout = backStdout }()
	tmp, err := ioutil.TempFile("", "")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmp.Name())

	os.Stdout = tmp

	_, err = ExecCommandWrapper(s.cmd)
	c.Assert(err, check.IsNil)

	completeOutput, err := ioutil.ReadFile(tmp.Name())
	c.Assert(err, check.IsNil)

	c.Assert(string(completeOutput), check.Equals, "")
}

func (s *cliTestSuite) TestExecCommandWrapperHonoursVerboseFlag(c *check.C) {
	s.writeVerboseConfig(true)

	backStdout := os.Stdout
	defer func() { os.Stdout = backStdout }()
	tmp, err := ioutil.TempFile("", "")
	c.Assert(err, check.IsNil)
	defer os.Remove(tmp.Name())

	os.Stdout = tmp

	cmdOutput, err := ExecCommandWrapper(s.cmd)
	c.Assert(err, check.IsNil)

	completeOutput, err := ioutil.ReadFile(tmp.Name())
	c.Assert(err, check.IsNil)

	sentCmd, err := AddOptionsToCommand(s.cmd.Args)

	expected := fmt.Sprintf("%s\n%s", strings.Join(sentCmd, " "), cmdOutput)
	c.Assert(string(completeOutput), check.Equals, expected)
}

func getParamOuput(output string, cmd *exec.Cmd) string {
	if len(cmd.Env) == 1 && cmd.Env[0] == defaultEnv {
		output += "\nEnv variables: " + strings.Join(cmd.Env, ", ")
	}
	dir := cmd.Dir
	if dir == defaultDir {
		output += "\nDir: " + dir
	}

	return output
}

func (s *cliTestSuite) writeFromBranchConfig(fromBranch bool) error {
	testutils.PrepareTargetDir(filepath.Dir(s.configFile))

	cfg := config.Config{
		FileName:   s.configFile,
		FromBranch: fromBranch,
	}
	cfg.Write()

	return nil
}

func (s *cliTestSuite) writeVerboseConfig(verbose bool) {
	testutils.PrepareTargetDir(filepath.Dir(s.configFile))

	cfg := config.Config{
		FileName: s.configFile,
		Verbose:  verbose,
	}
	cfg.Write()
}
