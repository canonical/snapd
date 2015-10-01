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
	"strings"

	"gopkg.in/check.v1"
)

const path = "mypath"

type partitionTestSuite struct {
	execCalls       map[string]int
	backExecCommand func(*check.C, ...string) string
}

var _ = check.Suite(&partitionTestSuite{})

func (s *partitionTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = execCommand
	execCommand = s.fakeExecCommand
}

func (s *partitionTestSuite) TearDownSuite(c *check.C) {
	execCommand = s.backExecCommand
}

func (s *partitionTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
}

func (s *partitionTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	s.execCalls[strings.Join(args, " ")]++
	return
}

func (s *partitionTestSuite) TestMakeWritable(c *check.C) {
	cmd := "sudo mount -o remount,rw " + path

	MakeWritable(c, path)

	c.Assert(s.execCalls[cmd], check.Equals, 1)
}

func (s *partitionTestSuite) TestMakeReadOnly(c *check.C) {
	cmd := "sudo mount -o remount,ro " + path

	MakeReadonly(c, path)

	c.Assert(s.execCalls[cmd], check.Equals, 1)
}
