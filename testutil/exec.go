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
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

var shellcheckPath string

func init() {
	if p := mylog.Check2(exec.LookPath("shellcheck")); err == nil {
		shellcheckPath = p
	}
}

var (
	shellchecked   = make(map[string]bool, 16)
	shellcheckedMu sync.Mutex
)

func shellcheckSeenAlready(script string) bool {
	shellcheckedMu.Lock()
	defer shellcheckedMu.Unlock()
	if shellchecked[script] {
		return true
	}
	shellchecked[script] = true
	return false
}

var pristineEnv = os.Environ()

func maybeShellcheck(c *check.C, script string, wholeScript io.Reader) {
	// MockCommand is used sometimes in SetUptTest, so it adds up
	// even for the empty script, don't recheck the essentially same
	// thing again and again!
	if shellcheckSeenAlready(script) {
		return
	}
	c.Logf("using shellcheck: %q", shellcheckPath)
	if shellcheckPath == "" {
		// no shellcheck, nothing to do
		return
	}
	cmd := exec.Command(shellcheckPath, "-s", "bash", "-")
	cmd.Env = pristineEnv
	cmd.Stdin = wholeScript
	out := mylog.Check2(cmd.CombinedOutput())
	c.Check(err, check.IsNil, check.Commentf("shellcheck failed:\n%s", string(out)))
}

// MockCmd allows mocking commands for testing.
type MockCmd struct {
	binDir  string
	exeFile string
	logFile string
}

// The top of the script generate the output to capture the
// command that was run and the arguments used. To support
// mocking commands that need "\n" in their args (like zenity)
// we use the following convention:
// - generate \0 to separate args
// - generate \0\0 to separate commands
var scriptTpl = `#!/bin/bash
###LOCK###
printf "%%s" "$(basename "$0")" >> %[1]q
printf '\0' >> %[1]q

for arg in "$@"; do
     printf "%%s" "$arg" >> %[1]q
     printf '\0'  >> %[1]q
done

printf '\0' >> %[1]q
%s
`

// Wrap the script in flock to serialize the calls to the script and prevent the
// call log from getting corrupted. Workaround 14.04 flock(1) weirdness, that
// keeps the script file open for writing and execve() fails with ETXTBSY.
var selfLock = `if [ "${FLOCKER}" != "$0" ]; then exec env FLOCKER="$0" flock -e "$(dirname "$0")" "$0" "$@" ; fi`

func mockCommand(c *check.C, basename, script, template string) *MockCmd {
	var wholeScript bytes.Buffer
	var binDir, exeFile, logFile string
	var newpath string
	if filepath.IsAbs(basename) {
		binDir = filepath.Dir(basename)
		mylog.Check(os.MkdirAll(binDir, 0755))

		exeFile = basename
		logFile = basename + ".log"
	} else {
		binDir = c.MkDir()
		exeFile = path.Join(binDir, basename)
		logFile = path.Join(binDir, basename+".log")
		newpath = binDir + ":" + os.Getenv("PATH")
	}
	fmt.Fprintf(&wholeScript, template, logFile, script)
	mylog.Check(os.WriteFile(exeFile, wholeScript.Bytes(), 0700))

	maybeShellcheck(c, script, &wholeScript)

	if newpath != "" {
		os.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	}

	return &MockCmd{binDir: binDir, exeFile: exeFile, logFile: logFile}
}

// MockCommand adds a mocked command. If the basename argument is a command it
// is added to PATH. If it is an absolute path it is just created there, along
// with the full prefix. The caller is responsible for the cleanup in this case.
//
// The command logs all invocations to a dedicated log file. If script is
// non-empty then it is used as is and the caller is responsible for how the
// script behaves (exit code and any extra behavior). If script is empty then
// the command exits successfully without any other side-effect.
func MockCommand(c *check.C, basename, script string) *MockCmd {
	return mockCommand(c, basename, script, strings.Replace(scriptTpl, "###LOCK###", "", 1))
}

// MockLockedCommand is the same as MockCommand(), but the script uses flock to
// enforce exclusive locking, preventing the call tracking from being corrupted.
// Thus it is safe to be called in parallel.
func MockLockedCommand(c *check.C, basename, script string) *MockCmd {
	return mockCommand(c, basename, script, strings.Replace(scriptTpl, "###LOCK###", selfLock, 1))
}

// Also mock this command, using the same bindir and log.
// Useful when you want to check the ordering of things.
func (cmd *MockCmd) Also(basename, script string) *MockCmd {
	exeFile := path.Join(cmd.binDir, basename)
	mylog.Check(os.WriteFile(exeFile, []byte(fmt.Sprintf(scriptTpl, cmd.logFile, script)), 0700))

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
//
//	[][]string{
//	    {"cmd", "arg1", "arg2"}, // first invocation of "cmd"
//	    {"cmd", "arg1", "arg2"}, // second invocation of "cmd"
//	}
func (cmd *MockCmd) Calls() [][]string {
	raw := mylog.Check2(os.ReadFile(cmd.logFile))
	if os.IsNotExist(err) {
		return nil
	}

	logContent := strings.TrimSuffix(string(raw), "\000")

	allCalls := [][]string{}
	calls := strings.Split(logContent, "\000\000")
	for _, call := range calls {
		call = strings.TrimSuffix(call, "\000")
		allCalls = append(allCalls, strings.Split(call, "\000"))
	}
	return allCalls
}

// ForgetCalls purges the list of calls made so far
func (cmd *MockCmd) ForgetCalls() {
	mylog.Check(os.Remove(cmd.logFile))
	if os.IsNotExist(err) {
		return
	}
}

// BinDir returns the location of the directory holding overridden commands.
func (cmd *MockCmd) BinDir() string {
	return cmd.binDir
}

// Exe return the full path of the mock binary
func (cmd *MockCmd) Exe() string {
	return filepath.Join(cmd.exeFile)
}
