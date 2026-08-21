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
	"github.com/snapcore/snapd/systemd"
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
// loginctl.
//
// On systemd >= 245 it invokes "loginctl show-session auto -p Class", which
// resolves to the caller's own session if the process is inside one, and to
// the user's display session otherwise. On older systemd it falls back to
// resolving the user's display session via "loginctl show-user <uid> -p
// Display" followed by "loginctl show-session <id> -p Class", which is what
// "auto" resolves to for processes outside any session.
//
// Note: on systemd < 245, when invoked from inside a session, the class of
// the user's display session is returned instead of the class of the
// caller's own session. We could consult the XDG_SESSION_ID environment
// variable to replicate the "caller's own session" clause of "auto", but the
// environment may leak a stale session id of another user across "sudo"
// invocations, so we don't do this, and when executed by an ExecCondition
// the variable is never set in the first place.
//
// The primary user of this function runs as an ExecCondition of systemd user
// services, i.e. as a child of user@UID.service which is never part of a
// session scope, so on both paths the display session is what gets resolved.
// Only user- and greeter-class sessions are ever display-eligible, so
// sessions of other classes (background, and on systemd >= 256 the
// manager-class session of the user manager itself) cannot shadow it.
//
// An error is returned if loginctl fails, if no session for the current user
// could be found, or if the output is malformed. An empty class ("Class=") is
// valid and will be returned as "" without error.
func SessionClass(ctx context.Context) (string, error) {
	// The special session name "auto" for "loginctl show-session" and
	// friends was introduced in systemd 245; on older systemd, "auto" is
	// treated as a literal session id and never resolves to anything.
	if err := systemd.EnsureAtLeast(245); err != nil {
		if !systemd.IsSystemdTooOld(err) {
			// the systemd version could not be determined at all
			return "", err
		}
		return sessionClassFromUserDisplay(ctx)
	}
	return sessionClassFromAuto(ctx)
}

// sessionClassFromAuto returns the class of the session resolved by the
// special session name "auto" (systemd >= 245).
func sessionClassFromAuto(ctx context.Context) (string, error) {
	out, err := loginctlCmd(ctx, "show-session", "auto", "-p", "Class")
	if err != nil {
		return "", err
	}

	return parseProperty(string(out), "Class")
}

// sessionClassFromUserDisplay returns the class of the display session of
// the current user, resolved with "loginctl show-user" and "loginctl
// show-session" (works on all supported systemd versions).
func sessionClassFromUserDisplay(ctx context.Context) (string, error) {
	uid := os.Getuid()
	// Note: --value is not passed to loginctl as it is only available
	// since systemd 230, and the "Name=value" output is parsed instead.
	out, err := loginctlCmd(ctx, "show-user", strconv.Itoa(uid), "-p", "Display")
	if err != nil {
		return "", err
	}

	// On old systemd, using -p implies --all, so an empty "Display=" is
	// printed when the user has no display session, in which case no
	// session can be determined for the user.
	sessionID, err := parseProperty(string(out), "Display")
	if err != nil {
		return "", err
	}
	if sessionID == "" {
		return "", fmt.Errorf("cannot find session for the current user: %d", uid)
	}

	out, err = loginctlCmd(ctx, "show-session", sessionID, "-p", "Class")
	if err != nil {
		return "", err
	}

	return parseProperty(string(out), "Class")
}

// parseProperty parses the "Name=value" output of loginctl's -p option. A
// malformed output results in an error. An empty value (e.g. "Foo=") is not
// treated as an error.
func parseProperty(output string, name string) (string, error) {
	orig := strings.TrimSpace(output)
	propName, propValue, ok := strings.Cut(orig, "=")
	if !ok || propName != name || strings.Contains(propValue, "=") {
		return "", fmt.Errorf("cannot parse value from loginctl output for property %q: %q", name, orig)
	}

	return strings.TrimSpace(propValue), nil
}
