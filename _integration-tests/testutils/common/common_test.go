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

package common

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

// testing a testsuite - thats so meta
type MetaTestSuite struct {
}

var _ = check.Suite(&MetaTestSuite{})

// test trivial cleanup
func (m *MetaTestSuite) TestCleanupSimple(c *check.C) {
	canary := "not-called"
	s := SnappySuite{}

	s.AddCleanup(func() {
		canary = "was-called"
	})
	s.TearDownTest(c)

	c.Assert(canary, check.Equals, "was-called")
}

// a mock method that takes a parameter
func mockCleanupMethodWithParameters(s *string) {
	*s = "was-called"
}

// test that whle AddCleanup() does not take any parameters itself,
// functions that need parameters can be passed by creating an
// anonymous function as a wrapper
func (m *MetaTestSuite) TestCleanupWithParameters(c *check.C) {
	canary := "not-called"
	s := SnappySuite{}

	s.AddCleanup(func() {
		mockCleanupMethodWithParameters(&canary)
	})
	s.TearDownTest(c)

	c.Assert(canary, check.Equals, "was-called")
}

type CommonTestSuite struct {
	execCalls       map[string]int
	execReturnValue string
	backExecCommand func(*check.C, ...string) string

	initTime time.Time
	delay    time.Duration
}

var _ = check.Suite(&CommonTestSuite{})

func (s *CommonTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = ExecCommand
	ExecCommand = s.fakeExecCommand
}

func (s *CommonTestSuite) TearDownSuite(c *check.C) {
	ExecCommand = s.backExecCommand
}

func (s *CommonTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.initTime = time.Now()
	s.delay = 0
}

func (s *CommonTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	s.execCalls[strings.Join(args, " ")]++
	if time.Since(s.initTime) >= s.delay {
		output = s.execReturnValue
	}
	return
}

func (s *CommonTestSuite) TestWaitForCommandExists(c *check.C) {
	cmd := "mycommand"
	outputPattern := "myOutput"
	s.execReturnValue = "myOutput"

	err := WaitForCommand(c, outputPattern, cmd)

	c.Assert(err, check.IsNil, check.Commentf("Got error %s", err))
}

func (s *CommonTestSuite) TestWaitForCommandCallsGivenCommand(c *check.C) {
	cmd := []string{"mycmd", "mypar"}

	WaitForCommand(c, "", cmd...)

	execCalls := s.execCalls["mycmd mypar"]

	c.Assert(execCalls, check.Equals, 1,
		check.Commentf("Expected 1 call to ExecCommand with 'mycmd mypar', got %d", execCalls))
}

func (s *CommonTestSuite) TestWaitForCommandFailsOnUnmatchedOutput(c *check.C) {
	cmd := []string{"mycmd", "mypar"}
	outputPattern := "myOutput"
	s.execReturnValue = "anotherOutput"

	err := WaitForCommand(c, outputPattern, cmd...)

	c.Assert(err, check.NotNil, check.Commentf("Didn't get expected error"))
}
