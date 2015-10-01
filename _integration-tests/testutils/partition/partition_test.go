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

package partition

import (
	"fmt"
	"strings"

	"gopkg.in/check.v1"
)

const (
	path        = "mypath"
	writableCmd = "sudo mount -o remount,rw " + path
	readonlyCmd = "sudo mount -o remount,ro " + path
	waitCmd     = lsofIdlePattern
)

type partitionTestSuite struct {
	execCalls           map[string]int
	waitCalls           map[string]int
	execOutput          string
	backExecCommand     func(*check.C, ...string) string
	backWaitForFunction func(*check.C, string, func() (string, error)) error
	waitError           bool
}

var _ = check.Suite(&partitionTestSuite{})

func (s *partitionTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	s.backWaitForFunction = waitForFunction
	execCommand = s.fakeExecCommand
	waitForFunction = s.fakeWaitForFunction
}

func (s *partitionTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
	waitForFunction = s.backWaitForFunction
}

func (s *partitionTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	s.waitCalls = make(map[string]int)
	s.waitError = false
	s.execOutput = ""
}

func (s *partitionTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	s.execCalls[strings.Join(args, " ")]++
	return s.execOutput
}

func (s *partitionTestSuite) fakeWaitForFunction(c *check.C, pattern string, f func() (string, error)) (err error) {
	s.waitCalls[pattern]++

	if s.waitError {
		err = fmt.Errorf("Wait error!")
	}

	return
}

func (s *partitionTestSuite) TestMakeWritableCallsExecCommand(c *check.C) {
	err := MakeWritable(c, path)

	c.Assert(err, check.IsNil)
	c.Assert(s.execCalls[writableCmd], check.Equals, 1)
}

func (s *partitionTestSuite) TestMakeWritableWaitsForIdlePartition(c *check.C) {
	err := MakeWritable(c, path)

	c.Assert(err, check.IsNil)
	c.Assert(s.waitCalls[waitCmd], check.Equals, 1)
}

func (s *partitionTestSuite) TestMakeWritableReturnsWaitError(c *check.C) {
	s.waitError = true
	err := MakeWritable(c, path)

	c.Assert(err, check.NotNil)
	c.Assert(s.waitCalls[waitCmd], check.Equals, 1)
	c.Assert(s.execCalls[writableCmd], check.Equals, 0)
}

func (s *partitionTestSuite) TestMakeReadOnlyCallsExecCommand(c *check.C) {
	err := MakeReadonly(c, path)

	c.Assert(err, check.IsNil)
	c.Assert(s.execCalls[readonlyCmd], check.Equals, 1)
}

func (s *partitionTestSuite) TestMakeReadonlyWaitsForIdlePartition(c *check.C) {
	err := MakeReadonly(c, path)

	c.Assert(err, check.IsNil)
	c.Assert(s.waitCalls[waitCmd], check.Equals, 1)
}

func (s *partitionTestSuite) TestMakeReadonlyReturnsWaitError(c *check.C) {
	s.waitError = true
	err := MakeReadonly(c, path)

	c.Assert(err, check.NotNil)
	c.Assert(s.waitCalls[waitCmd], check.Equals, 1)
	c.Assert(s.execCalls[readonlyCmd], check.Equals, 0)
}

func (s *partitionTestSuite) TestCheckPartitionBusyFunc(c *check.C) {
	testCases := []struct {
		execCommandOutput string
		expected          string
	}{
		{`prg  4827 user  mem    REG    8,2      3339 10354893 /usr/share/prg
prg  4827 user   15w   REG    8,2    197132 12452026 /home/user/prg`, "15w"},
		{`prg  4827 user  mem    REG    8,2      3339 10354893 /usr/share/prg
prg  4827 user   15w   REG    8,2    197132 12452026 /home/user/prg
prg  4827 user   1w   REG    8,2    197132 12452026 /home/user/prg`, "15w"},
		{`prg  4827 user  mem    REG    8,2      3339 10354893 /usr/share/prg`, lsofIdlePattern},
		{`prg  4827 user  cwd    REG    8,2      3339 10354893 /usr/share/prg`, lsofIdlePattern},
	}

	for _, testCase := range testCases {
		s.execOutput = testCase.execCommandOutput
		f := checkPathBusyFunc(c, path)

		actual, err := f()
		c.Check(err, check.IsNil)
		c.Check(actual, check.Equals, testCase.expected,
			check.Commentf("input text %s, expected output %s, obtained %s",
				testCase.execCommandOutput, testCase.expected, actual))
	}
}
