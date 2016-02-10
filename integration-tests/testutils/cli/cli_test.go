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
	"strings"
	"testing"

	"gopkg.in/check.v1"
)

const (
	execCmd       = "mycmd"
	execOutput    = "myoutput"
	defaultDir    = "/tmp"
	defaultEnvVar = "VAR"
	defaultEnvVal = "VAL"
	defaultEnv    = defaultEnvVar + "=" + defaultEnvVal
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type cliTestSuite struct {
	backExecCommand func(string, ...string) *exec.Cmd
	helperProcess   string
	cmd             *exec.Cmd
}

var _ = check.Suite(&cliTestSuite{})

func (s *cliTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
}

func (s *cliTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
}

func (s *cliTestSuite) SetUpTest(c *check.C) {
	s.helperProcess = "TestHelperProcess"
	s.cmd = execCommand(execCmd)
}

func (s *cliTestSuite) fakeExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-check.f=cliTestSuite." + s.helperProcess, "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

func (s *cliTestSuite) TestHelperProcess(c *check.C) {
	baseHelperProcess(0)
}

func (s *cliTestSuite) TestHelperProcessErr(c *check.C) {
	baseHelperProcess(1)
}

func baseHelperProcess(exitValue int) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	dir, _ := os.Getwd()
	env := []string{}
	val := os.Getenv(defaultEnvVar)
	if val != "" {
		env = []string{"GO_WANT_HELPER_PROCESS=1", defaultEnvVar + "=" + val}
	}
	fmt.Fprintf(os.Stdout, getParamOuput(&exec.Cmd{Dir: dir, Env: env}))
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

func (s *cliTestSuite) TestExecCommandWrapperHonoursDir(c *check.C) {
	s.cmd.Dir = defaultDir
	actualOutput, err := ExecCommandWrapper(s.cmd)

	c.Assert(actualOutput, check.Equals, getParamOuput(s.cmd))
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandWrapperHonoursEnv(c *check.C) {
	s.cmd.Env = append(s.cmd.Env, defaultEnv)
	actualOutput, err := ExecCommandWrapper(s.cmd)

	c.Assert(actualOutput, check.Equals, getParamOuput(s.cmd))
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandWrapperReturnsErr(c *check.C) {
	s.helperProcess = "TestHelperProcessErr"

	_, err := ExecCommandWrapper(s.cmd)

	c.Assert(err, check.IsNil)
}

func getParamOuput(cmd *exec.Cmd) string {
	output := execOutput
	if len(cmd.Env) == 1 && cmd.Env[0] == defaultEnv {
		output += "\nEnv variables: " + strings.Join(cmd.Env, ", ")
	}
	dir := cmd.Dir
	if dir == defaultDir {
		output += "\nDir: " + dir
	}

	return output
}
