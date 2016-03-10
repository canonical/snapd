// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package testutil

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	. "gopkg.in/check.v1"
)

// ExecTest is a test helper for tests that need to run external programs.
type ExecTest struct {
	BaseTest
	dirOnPath string
	logFiles  map[string]string
}

// SetUpTest adds a new temporary directory to PATH, in front of other entries.
func (s *ExecTest) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	PATH := os.Getenv("PATH")
	s.dirOnPath = c.MkDir()
	os.Setenv("PATH", s.dirOnPath+":"+PATH)
	s.BaseTest.AddCleanup(func() { os.Setenv("PATH", PATH) })
	s.logFiles = make(map[string]string)
}

// MockExecutable creates a fake executable that always succeeds.
func (s *ExecTest) MockExecutable(c *C, fname string) {
	s.createExecutable(c, fname, 0)
}

// MockFalilingExecutable creates a fake executable that always fails.
func (s *ExecTest) MockFailingExecutable(c *C, fname string, exitCode int) {
	s.createExecutable(c, fname, exitCode)
}

// CallsToExecutable returns a list of calls that were made to the given executable.
func (s *ExecTest) CallsToExecutable(c *C, fname string) []string {
	logFname, ok := s.logFiles[fname]
	if !ok {
		c.Fatalf("executable %q was not faked", fname)
	}
	calls, err := ioutil.ReadFile(logFname)
	c.Assert(err, IsNil)
	text := string(calls)
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

func (s *ExecTest) createExecutable(c *C, fname string, exitCode int) {
	execFname := path.Join(s.dirOnPath, fname)
	logFname := path.Join(s.dirOnPath, fname+".log")
	ioutil.WriteFile(execFname, []byte(fmt.Sprintf(""+
		"#!/bin/sh\n"+
		"echo \"$@\" >> %q\n"+
		"exit %d\n", logFname, exitCode)), 0700)
	s.logFiles[fname] = logFname
}
