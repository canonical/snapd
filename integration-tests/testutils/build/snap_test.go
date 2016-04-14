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

package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"
)

type snapBuildTestSuite struct {
	execCalls       map[string]int
	execReturnValue string
	backExecCommand func(*check.C, ...string) string
	defaultSnapName string
}

var _ = check.Suite(&snapBuildTestSuite{})

func (s *snapBuildTestSuite) SetUpSuite(c *check.C) {
	s.backExecCommand = cliExecCommand
	cliExecCommand = s.fakeExecCommand
	s.defaultSnapName = "mySnapName"
}

func (s *snapBuildTestSuite) TearDownSuite(c *check.C) {
	cliExecCommand = s.backExecCommand
}

func (s *snapBuildTestSuite) SetUpTest(c *check.C) {
	s.execCalls = make(map[string]int)
	snapName := s.defaultSnapName + snapFilenameSufix
	path := filepath.Join(buildPath(s.defaultSnapName), snapName)
	s.execReturnValue = fmt.Sprintf("Generated '%s' snap\n", path)
}

func (s *snapBuildTestSuite) fakeExecCommand(c *check.C, args ...string) (output string) {
	s.execCalls[strings.Join(args, " ")]++
	return s.execReturnValue
}

func (s *snapBuildTestSuite) TestBuildPath(c *check.C) {
	path := buildPath(s.defaultSnapName)

	expected := buildPath(s.defaultSnapName)
	c.Assert(path, check.Equals, expected)
}

func (s *snapBuildTestSuite) TestLocalSnapCallsExecCommand(c *check.C) {
	_, err := LocalSnap(c, s.defaultSnapName)

	c.Assert(err, check.IsNil)
	path := buildPath(s.defaultSnapName)
	c.Assert(s.execCalls["snap build "+path+" -o "+path], check.Equals, 1)
}

func (s *snapBuildTestSuite) TestLocalSnapReturnsSnapPath(c *check.C) {
	snapPath, err := LocalSnap(c, s.defaultSnapName)

	c.Assert(err, check.IsNil)
	expected := filepath.Join(buildPath(s.defaultSnapName), s.defaultSnapName+snapFilenameSufix)
	c.Assert(snapPath, check.Equals, expected)
}

func (s *snapBuildTestSuite) TestLocalSnapReturnsError(c *check.C) {
	s.execReturnValue = "Wrong return value"
	snapPath, err := LocalSnap(c, s.defaultSnapName)

	c.Assert(err, check.NotNil)
	c.Assert(snapPath, check.Equals, "")
}
