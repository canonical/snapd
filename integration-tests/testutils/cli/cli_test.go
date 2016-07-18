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
	targetCmds       []string
}

var _ = check.Suite(&cliTestSuite{})

func (s *cliTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
	s.configFile = config.DefaultFileName
	s.targetCmds = []string{"snappy", "snap", "snapd"}
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

	ExecCommandWrapper(s.cmd)

	completeOutput, err := ioutil.ReadFile(tmp.Name())
	c.Assert(err, check.IsNil)

	expected := strings.Join(s.cmd.Args, " ") + "\n"
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
