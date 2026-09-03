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
	"os"
	"os/exec"
	"strconv"
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

// SessionClass returns the class of the display session of the current
// user as reported by loginctl.
//
// It first invokes "loginctl show-user <uid> --all -p Display" to resolve the
// display session of the current user, and then "loginctl show-session <id>
// --all -p Class" to get the class of that session, parsing the "Class=<value>"
// output.
//
// Note that the class of the display session of the current user is returned,
// which may be a different session than the one the current process is part of
// (if any). User sessions are display-eligible if and only if they are of
// class "user", "greeter", or "user-early" (on systemd >= 256), "user-light"
// or "user-early-light" (on systemd >= 259). User sessions of class
// "background" or "manager", for example, are never display-eligible.
//
// An error is returned if loginctl fails, if no session for the current
// user could be found, or if the output is malformed or class empty.
func SessionClass(ctx context.Context) (string, error) {
	uid := os.Getuid()
	// Note: --value is not passed to loginctl as it is only available
	// since systemd 230, and the "Name=value" output is parsed instead.
	out, err := loginctlCmd(ctx, "show-user", strconv.Itoa(uid), "--all", "-p", "Display")
	if err != nil {
		return "", err
	}

	// Using --all implies that empty properties are shown too, so an empty
	// "Display=" is printed when the user has no display session, in
	// which case no session can be determined for the user.
	sessionID, err := parseProperty(string(out), "Display", true)
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("cannot find display-eligible session for the current user: %d", uid)
	}

	out, err = loginctlCmd(ctx, "show-session", sessionID, "--all", "-p", "Class")
	if err != nil {
		return "", err
	}

	return parseProperty(string(out), "Class", false)
}

// parseProperty parses the "Name=value" output of loginctl's -p option. A
// malformed output results in an error. An empty value (e.g. "Foo=") is not
// treated as an error.
func parseProperty(output string, name string, allowEmpty bool) (string, error) {
	orig := strings.TrimSpace(output)
	propName, propValue, ok := strings.Cut(orig, "=")
	if !ok || propName != name || strings.Contains(propValue, "=") || !allowEmpty && propValue == "" {
		return "", fmt.Errorf("cannot parse value from loginctl output for property %q: %q", name, orig)
	}

	return strings.TrimSpace(propValue), nil
}
