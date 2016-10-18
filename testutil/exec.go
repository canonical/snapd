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

var scriptTpl = `#!/bin/sh
echo "$(basename "$0")" >> %[1]q
for arg in "$@"; do
    echo "$arg" >> %[1]q
done
echo >> %[1]q
%s
`

// MockCommand adds a mocked command to PATH.
//
// The command logs all invocations to a dedicated log file. If script is
// non-empty then it is used as is and the caller is responsible for how the
// script behaves (exit code and any extra behavior). If script is empty then
// the command exits successfully without any other side-effect.
func MockCommand(c *check.C, basename, script string) *MockCmd {
	binDir := c.MkDir()
	exeFile := path.Join(binDir, basename)
	logFile := path.Join(binDir, basename+".log")
	err := ioutil.WriteFile(exeFile, []byte(fmt.Sprintf(scriptTpl, logFile, script)), 0700)
	if err != nil {
		panic(err)
	}
	os.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return &MockCmd{binDir: binDir, exeFile: exeFile, logFile: logFile}
}

// Also mock this command, using the same bindir and log
// Useful when you want to check the ordering of things.
func (cmd *MockCmd) Also(basename, script string) *MockCmd {
	exeFile := path.Join(cmd.binDir, basename)
	err := ioutil.WriteFile(exeFile, []byte(fmt.Sprintf(scriptTpl, cmd.logFile, script)), 0700)
	if err != nil {
		panic(err)
	}
	return &MockCmd{binDir: cmd.binDir, exeFile: exeFile, logFile: cmd.logFile}
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
// of the form:
// [][]string{
//     {"cmd", "arg1", "arg2"}, // first invocation of "cmd"
//     {"cmd", "arg1", "arg2"}, // second invocation of "cmd"
// }
func (cmd *MockCmd) Calls() [][]string {
	raw, err := ioutil.ReadFile(cmd.logFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		panic(err)
	}
	logContent := strings.TrimSuffix(string(raw), "\n")

	allCalls := [][]string{}
	calls := strings.Split(logContent, "\n\n")
	for _, call := range calls {
		call = strings.TrimSuffix(call, "\n")
		allCalls = append(allCalls, strings.Split(call, "\n"))
	}
	return allCalls
}

// ForgetCalls purges the list of calls made so far
func (cmd *MockCmd) ForgetCalls() {
	err := os.Remove(cmd.logFile)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		panic(err)
	}
}

// BinDir returns the location of the directory holding overridden commands.
func (cmd *MockCmd) BinDir() string {
	return cmd.binDir
}
