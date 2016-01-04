// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package wait

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"
)

const (
	outputPattern = "myOutputPattern"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type waitTestSuite struct {
	myFuncCalls       int
	myFuncReturnValue string
	myFuncError       bool

	outputPattern string

	initTime time.Time
	delay    time.Duration

	backInterval       time.Duration
	backMaxWaitRetries int
}

var _ = check.Suite(&waitTestSuite{})

func (s *waitTestSuite) SetUpTest(c *check.C) {
	s.myFuncCalls = 0
	s.myFuncReturnValue = outputPattern
	s.myFuncError = false

	s.backInterval = interval
	s.backMaxWaitRetries = maxWaitRetries

	// save the starting time to be able to measure delays
	s.initTime = time.Now()
	// reset delay, each test will set it up if needed
	s.delay = 0

	interval = 1
}

func (s *waitTestSuite) TearDownTest(c *check.C) {
	interval = s.backInterval
	maxWaitRetries = s.backMaxWaitRetries
}

func (s *waitTestSuite) myFunc() (output string, err error) {
	s.myFuncCalls++

	// after the given delay (0 by default) this method will return the value specified at
	// myFuncReturnValue, before it all the calls will return an empty string
	if time.Since(s.initTime) >= s.delay {
		output = s.myFuncReturnValue
		if s.myFuncError {
			err = fmt.Errorf("Error!")
		}
	}
	return
}

func (s *waitTestSuite) TestForFunctionExists(c *check.C) {
	err := ForFunction(c, outputPattern, s.myFunc)

	c.Assert(err, check.IsNil, check.Commentf("Got error %s", err))
}

func (s *waitTestSuite) TestForFunctionCallsGivenFunction(c *check.C) {
	ForFunction(c, outputPattern, s.myFunc)

	c.Assert(s.myFuncCalls, check.Equals, 1,
		check.Commentf("Expected 1 call to ExecCommand with 'mycmd mypar', got %d", s.myFuncCalls))
}

func (s *waitTestSuite) TestForFunctionReturnsFunctionError(c *check.C) {
	s.myFuncError = true

	err := ForFunction(c, outputPattern, s.myFunc)

	c.Assert(err, check.NotNil, check.Commentf("Didn't get expected error"))
}

func (s *waitTestSuite) TestForFunctionReturnsFunctionErrorOnRetry(c *check.C) {
	s.myFuncError = true
	s.delay = 1 * time.Millisecond

	err := ForFunction(c, outputPattern, s.myFunc)

	c.Assert(err, check.NotNil, check.Commentf("Didn't get expected error"))
}

func (s *waitTestSuite) TestForFunctionFailsOnUnmatchedOutput(c *check.C) {
	maxWaitRetries = 0

	s.myFuncReturnValue = "anotherPattern"
	err := ForFunction(c, outputPattern, s.myFunc)

	c.Assert(err, check.NotNil, check.Commentf("Didn't get expected error"))
}

func (s *waitTestSuite) TestForFunctionRetriesCalls(c *check.C) {
	maxWaitRetries = 2

	s.delay = 2 * time.Millisecond

	err := ForFunction(c, outputPattern, s.myFunc)

	c.Assert(err, check.IsNil, check.Commentf("Got error %s", err))
	c.Assert(s.myFuncCalls > 0, check.Equals, true)
}

func (s *waitTestSuite) TestForFunctionHonoursMaxWaitRetries(c *check.C) {
	maxWaitRetries = 3

	s.myFuncReturnValue = "anotherPattern"

	ForFunction(c, outputPattern, s.myFunc)

	// the first call is not actually a retry
	actualRetries := s.myFuncCalls - 1

	c.Assert(actualRetries, check.Equals, maxWaitRetries,
		check.Commentf("Actual number of retries %d does not match max retries %d",
			actualRetries, maxWaitRetries))
}

func (s *waitTestSuite) TestForActiveServiceCallsForCommand(c *check.C) {
	backForCommand := ForCommand
	defer func() { ForCommand = backForCommand }()
	var called string
	ForCommand = func(c *check.C, pattern string, cmds ...string) (err error) {
		called = fmt.Sprintf("ForCommand called with pattern '%s' and cmds '%s'",
			pattern, strings.Join(cmds, " "))
		return
	}

	ForActiveService(c, "myservice")

	expectedCalled := "ForCommand called with pattern 'ActiveState=active\n' and cmds 'systemctl show -p ActiveState myservice'"
	c.Assert(called, check.Equals, expectedCalled, check.Commentf("Expected call to ForCommand didn't happen"))
}

func (s *waitTestSuite) TestForServerOnPortCallsForCommand(c *check.C) {
	backForCommand := ForCommand
	defer func() { ForCommand = backForCommand }()
	var called string
	ForCommand = func(c *check.C, pattern string, cmds ...string) (err error) {
		called = fmt.Sprintf("ForCommand called with pattern '%s' and cmds '%s'",
			pattern, strings.Join(cmds, " "))
		return
	}

	ForServerOnPort(c, "tcp", 1234)

	expectedCalled := `ForCommand called with pattern '(?sU)^.*tcp .*:1234\s*(0\.0\.0\.0|::):\*\s*LISTEN.*' and cmds 'netstat -tapn'`
	c.Assert(called, check.Equals, expectedCalled, check.Commentf("Expected call to ForCommand didn't happen"))
}

func (s *waitTestSuite) TestForCommandCallsForFunction(c *check.C) {
	backForFunction := ForFunction
	defer func() { ForFunction = backForFunction }()
	var called string

	ForFunction = func(c *check.C, pattern string, inputFunc func() (string, error)) (err error) {
		called = fmt.Sprintf("ForFunction called with pattern '%s' and execCommand wrapper", pattern)
		return
	}

	ForCommand(c, "pattern", "ls")

	expectedCalled := `ForFunction called with pattern 'pattern' and execCommand wrapper`
	c.Assert(called, check.Equals, expectedCalled, check.Commentf("Expected call to ForFunction didn't happen"))
}

func (s *waitTestSuite) TestForCommandUsesExecCommand(c *check.C) {
	maxWaitRetries = 0

	backExecCommand := execCommand
	defer func() { execCommand = backExecCommand }()
	var called string

	execCommand = func(c *check.C, cmds ...string) (output string) {
		called = fmt.Sprintf("execCommand called with cmds %s", cmds)
		return
	}

	ForCommand(c, "pattern", "ls")

	expectedCalled := `execCommand called with cmds [ls]`
	c.Assert(called, check.Equals, expectedCalled, check.Commentf("Expected call to execCommand didn't happen"))
}
