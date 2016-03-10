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

	"gopkg.in/check.v1"
)

// MockCmd allows mocking commands for testing.
type MockCmd struct {
	binDir  string
	exeFile string
	logFile string
}

// MockCommand adds a mocked command to PATH.
//
// The command logs all invocations to a dedicated log file and exits with the
// specified code.
func MockCommand(c *check.C, basename string, status int) *MockCmd {
	binDir := c.MkDir()
	exeFile := path.Join(binDir, basename)
	logFile := path.Join(binDir, basename+".log")
	ioutil.WriteFile(exeFile, []byte(fmt.Sprintf(""+
		"#!/bin/sh\n"+
		"echo \"$@\" >> %q\n"+
		"exit %d\n", logFile, status)), 0700)
	os.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return &MockCmd{binDir: binDir, exeFile: exeFile, logFile: logFile}
}

// Restore removes the mocked command from PATH
func (cmd *MockCmd) Restore() {
	entries := strings.Split(os.Getenv("PATH"), ":")
	for i, entry := range entries {
		if entry == cmd.binDir {
			entries = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	os.Setenv("PATH", strings.Join(entries, ":"))
}

// Calls returns a list of calls that were made to the mock command.
func (cmd *MockCmd) Calls() []string {
	calls, err := ioutil.ReadFile(cmd.logFile)
	if err != nil {
		panic(err)
	}
	text := string(calls)
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}
