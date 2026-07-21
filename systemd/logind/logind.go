// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package logind

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// loginctlCmd calls loginctl with the given args, returning its standard
// output (and wrapped error)
var loginctlCmd = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "loginctl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitCode, runErr := osutil.ExitCode(err)
		return nil, &Error{cmd: args, exitCode: exitCode, runErr: runErr,
			msg: osutil.CombineStdOutErr(stdout.Bytes(), stderr.Bytes())}
	}
	return stdout.Bytes(), nil
}

// MockLoginctl allows to mock the loginctl invocations.
// The provided function will be called when loginctl would be invoked.
// The function can return the output and an error.
func MockLoginctl(f func(ctx context.Context, args ...string) ([]byte, error)) func() {
	oldLoginctlCmd := loginctlCmd
	loginctlCmd = f
	return func() {
		loginctlCmd = oldLoginctlCmd
	}
}

// Error is returned if the loginctl action failed
type Error struct {
	cmd      []string
	msg      []byte
	exitCode int
	runErr   error
}

func (e *Error) Msg() []byte {
	return e.msg
}

func (e *Error) ExitCode() int {
	return e.exitCode
}

func (e *Error) Error() string {
	var msg string
	if len(e.msg) > 0 {
		msg = fmt.Sprintf(": %s", e.msg)
	}
	if e.runErr != nil {
		return fmt.Sprintf("loginctl command %v failed with: %v%s", e.cmd, e.runErr, msg)
	}
	return fmt.Sprintf("loginctl command %v failed with exit status %d%s", e.cmd, e.exitCode, msg)
}

// SessionClass returns the class of the current session as reported by
// loginctl. It invokes "loginctl show-session auto -p Class" and parses
// the "Class=<value>" output. An error is returned if loginctl fails,
// for example when there is no session for the current process.
func SessionClass(ctx context.Context) (string, error) {
	out, err := loginctlCmd(ctx, "show-session", "auto", "-p", "Class")
	if err != nil {
		return "", err
	}

	// strip the "Class=" prefix from the output
	orig := strings.TrimSpace(string(out))
	before, after, ok := strings.Cut(orig, "=")
	if !ok || before != "Class" || after == "" || strings.Contains(after, "=") {
		return "", fmt.Errorf("invalid property format from loginctl for Class: %q", orig)
	}

	return strings.TrimSpace(after), nil
}
