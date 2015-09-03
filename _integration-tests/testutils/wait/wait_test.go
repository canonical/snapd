// -*- Mode: Go; indent-tabs-mode: t -*-

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

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type WaitTestSuite struct {
	execCalls       map[string]int
	execReturnValue string
	backExecCommand func(*check.C, ...string) string

	initTime time.Time
	delay    time.Duration
}

var _ = check.Suite(&WaitTestSuite{})

func (s *WaitTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
}

func (s *WaitTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
}

func (s *WaitTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.execReturnValue = ""
	// save the starting time to be able to measure delays
	s.initTime = time.Now()
	// reset delay, each test will set it up if needed
	s.delay = 0
}

func (s *WaitTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	s.execCalls[strings.Join(args, " ")]++

	// after the given delay (0 by default) this method will return the value specified at
	// execReturnValue, before it all the calls will return an empty string
	if time.Since(s.initTime) >= s.delay {
		output = s.execReturnValue
	}

	return
}

func (s *WaitTestSuite) TestForCommandExists(c *check.C) {
	cmd := "mycommand"
	outputPattern := "myOutput"
	s.execReturnValue = "myOutput"

	err := ForCommand(c, outputPattern, cmd)

	c.Assert(err, check.IsNil, check.Commentf("Got error %s", err))
}

func (s *WaitTestSuite) TestForCommandCallsGivenCommand(c *check.C) {
	cmd := []string{"mycmd", "mypar"}
	outputPattern := "myOutput"
	s.execReturnValue = "myOutput"

	ForCommand(c, outputPattern, cmd...)

	execCalls := s.execCalls["mycmd mypar"]

	c.Assert(execCalls, check.Equals, 1,
		check.Commentf("Expected 1 call to ExecCommand with 'mycmd mypar', got %d", execCalls))
}

func (s *WaitTestSuite) TestForCommandFailsOnUnmatchedOutput(c *check.C) {
	cmd := []string{"mycmd", "mypar"}
	outputPattern := "myOutput"
	s.execReturnValue = "anotherOutput"

	backMaxWaitRetries := maxWaitRetries
	defer func() { maxWaitRetries = backMaxWaitRetries }()
	maxWaitRetries = 0

	err := ForCommand(c, outputPattern, cmd...)

	c.Assert(err, check.NotNil, check.Commentf("Didn't get expected error"))
}

func (s *WaitTestSuite) TestForCommandRetriesCalls(c *check.C) {
	cmd := []string{"mycmd", "mypar"}
	outputPattern := "myOutput"
	s.execReturnValue = "myOutput"

	s.delay = 10 * time.Millisecond

	err := ForCommand(c, outputPattern, cmd...)

	c.Assert(err, check.IsNil, check.Commentf("Got error %s", err))
}

func (s *WaitTestSuite) TestForCommandHonoursMaxWaitRetries(c *check.C) {
	cmd := []string{"mycmd", "mypar"}
	outputPattern := "myOutput"

	backMaxWaitRetries := maxWaitRetries
	defer func() { maxWaitRetries = backMaxWaitRetries }()
	maxWaitRetries = 3

	ForCommand(c, outputPattern, cmd...)

	// the first call is not actually a retry
	actualRetries := s.execCalls["mycmd mypar"] - 1

	c.Assert(actualRetries, check.Equals, maxWaitRetries,
		check.Commentf("Actual number of retries %d does not match max retries %d",
			actualRetries, maxWaitRetries))
}

func (s *WaitTestSuite) TestForActiveServiceCallsForCommand(c *check.C) {
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
