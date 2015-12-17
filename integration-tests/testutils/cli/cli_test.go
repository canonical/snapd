// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"

	"gopkg.in/check.v1"
)

const execOutput = "myoutput"

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type cliTestSuite struct {
	backExecCommand func(string, ...string) *exec.Cmd
	helperProcess   string
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
	fmt.Fprintf(os.Stdout, execOutput)
	os.Exit(exitValue)
}

func (s *cliTestSuite) TestExecCommand(c *check.C) {
	actualOutput := ExecCommand(c, "mycmd")

	c.Assert(actualOutput, check.Equals, execOutput)
}

func (s *cliTestSuite) TestExecCommandToFile(c *check.C) {
	outputFile, err := ioutil.TempFile("", "snappy-exec")
	c.Assert(err, check.IsNil)
	outputFile.Close()
	defer os.Remove(outputFile.Name())

	ExecCommandToFile(c, outputFile.Name(), "mycmd")

	actualFileContents, err := ioutil.ReadFile(outputFile.Name())
	c.Assert(err, check.IsNil)
	c.Assert(string(actualFileContents), check.Equals, execOutput)
}

func (s *cliTestSuite) TestExecCommandErr(c *check.C) {
	actualOutput, err := ExecCommandErr("mycmd")

	c.Assert(actualOutput, check.Equals, execOutput)
	c.Assert(err, check.IsNil)
}

func (s *cliTestSuite) TestExecCommandErrWithError(c *check.C) {
	s.helperProcess = "TestHelperProcessErr"
	actualOutput, err := ExecCommandErr("mycmd")

	c.Assert(actualOutput, check.Equals, execOutput)
	c.Assert(err, check.NotNil)
}
