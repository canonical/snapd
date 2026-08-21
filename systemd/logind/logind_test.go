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

package logind_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/logind"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type logindSuite struct{}

var _ = Suite(&logindSuite{})

func (s *logindSuite) TestSessionClass(c *C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	// All known session classes from systemd's logind-session.h
	for _, class := range []string{
		"user",
		"user-early",
		"user-incomplete",
		"user-light",
		"user-early-light",
		"greeter",
		"lock-screen",
		"background",
		"background-light",
		"manager",
		"manager-early",
		"none",
	} {
		restore := logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte("Class=" + class + "\n"), nil
		})
		defer restore()

		got, err := logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)

		// Try without trailing \n
		restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte("Class=" + class), nil
		})
		defer restore()

		got, err = logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)
	}
}

func (s *logindSuite) TestSessionClassNoSession(c *C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	var loginctlErr *logind.Error
	loginctlErr = &logind.Error{}
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("No session for PID"))

	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return nil, loginctlErr
	})
	defer restore()

	_, err := logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "loginctl command .* failed with exit status 1: No session for PID")
}

func (s *logindSuite) TestSessionClassEmptyOutput(c *C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return []byte(""), nil
	})
	defer restore()

	_, err := logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Class": .*`)
}

func (s *logindSuite) TestSessionClassMalformedOutput(c *C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	for _, output := range []string{"", "unexpected-no-equals\n", "foo=user\n", "Class=foo=\n"} {
		restore := logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte(output), nil
		})
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Class": .*`)
	}
}

func (s *logindSuite) TestSessionClassWithWhitespace(c *C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return []byte("  Class=user  \n"), nil
	})
	defer restore()

	got, err := logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "user")
}

func (s *logindSuite) TestSessionClassVersionBoundary(c *C) {
	// with systemd >= 245 the session is resolved via the special name
	// "auto", independently of the exact version
	for _, version := range []int{245, 252, 255, 256} {
		restore := systemd.MockSystemdVersion(version, nil)
		defer restore()

		restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte("Class=user\n"), nil
		})
		defer restore()

		got, err := logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, "user")
	}

	// with systemd < 245 "auto" is not understood, the user's display
	// session is resolved instead
	for _, version := range []int{229, 237, 244} {
		restore := systemd.MockSystemdVersion(version, nil)
		defer restore()

		restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "show-user":
				c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
				return []byte("Display=c5\n"), nil
			case "show-session":
				c.Check(args, DeepEquals, []string{"show-session", "c5", "-p", "Class"})
				return []byte("Class=user\n"), nil
			}
			return nil, nil
		})
		defer restore()

		got, err := logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, "user")
	}
}

func (s *logindSuite) TestSessionClassFromUserDisplay(c *C) {
	restore := systemd.MockSystemdVersion(237, nil)
	defer restore()

	var output string

	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "show-user":
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
			return []byte(output), nil
		case "show-session":
			c.Check(args, DeepEquals, []string{"show-session", "c7", "-p", "Class"})
			return []byte("Class=greeter\n"), nil
		}
		return nil, nil
	})
	defer restore()

	// the display session class is returned, with the full call sequence
	output = "Display=c7\n"
	got, err := logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "greeter")

	// no trailing newline on the display session id
	output = "Display=c7"
	got, err = logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "greeter")

	// whitespace around the display session id is tolerated
	output = "  Display=c7  \n"
	got, err = logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "greeter")

	// the class output of the resolved session is malformed
	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "show-user":
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
			return []byte("Display=c7\n"), nil
		case "show-session":
			c.Check(args, DeepEquals, []string{"show-session", "c7", "-p", "Class"})
			return []byte("unexpected-no-equals\n"), nil
		}
		return nil, nil
	})
	defer restore()

	_, err = logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Class": .*`)
}

func (s *logindSuite) TestSessionClassFromUserDisplayNoSession(c *C) {
	restore := systemd.MockSystemdVersion(237, nil)
	defer restore()

	// on old systemd -p implies --all, so an empty Display is printed
	// when the user has no display session
	for _, displayOutput := range []string{"Display=\n", "Display="} {
		restore := logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
			return []byte(displayOutput), nil
		})
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, ErrorMatches, "cannot find session for the current user: .*")
	}
}

func (s *logindSuite) TestSessionClassFromUserDisplayMalformedOutput(c *C) {
	restore := systemd.MockSystemdVersion(237, nil)
	defer restore()

	for _, displayOutput := range []string{"", "unexpected-no-equals\n", "foo=c5\n", "Display=c5=x\n"} {
		restore := logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
			return []byte(displayOutput), nil
		})
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Display": .*`)
	}
}

func (s *logindSuite) TestSessionClassFromUserDisplayErrors(c *C) {
	restore := systemd.MockSystemdVersion(237, nil)
	defer restore()

	// show-user fails, e.g. when the user is not tracked by logind
	loginctlErr := &logind.Error{}
	loginctlErr.SetCmd([]string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("Failed to look up user"))

	calls := 0
	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		return nil, loginctlErr
	})
	defer restore()

	_, err := logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "loginctl command .* failed with exit status 1: Failed to look up user")
	c.Check(calls, Equals, 1)

	// show-session fails, e.g. when the display session vanished between
	// the two calls
	loginctlErr = &logind.Error{}
	loginctlErr.SetCmd([]string{"show-session", "c5", "-p", "Class"})
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("No session 'c5' known"))

	calls = 0
	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		switch args[0] {
		case "show-user":
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "-p", "Display"})
			return []byte("Display=c5\n"), nil
		case "show-session":
			return nil, loginctlErr
		}
		return nil, nil
	})
	defer restore()

	_, err = logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "loginctl command .* failed with exit status 1: No session 'c5' known")
	c.Check(calls, Equals, 2)
}

func (s *logindSuite) TestSessionClassVersionDeterminationError(c *C) {
	versionErr := errors.New("cannot read systemd version")
	restore := systemd.MockSystemdVersion(0, versionErr)
	defer restore()

	calls := 0
	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		return nil, nil
	})
	defer restore()

	_, err := logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "cannot read systemd version")
	c.Check(calls, Equals, 0)
}

func (s *logindSuite) TestError(c *C) {
	e := &logind.Error{}
	e.SetExitCode(2)
	e.SetMsg([]byte("some error"))
	c.Check(e.ExitCode(), Equals, 2)
	c.Check(string(e.Msg()), Equals, "some error")
	c.Check(e.Error(), Equals, "loginctl command [] failed with exit status 2: some error")
	e.SetCmd([]string{"foo", "bar", "--baz"})
	c.Check(e.Error(), Equals, "loginctl command [foo bar --baz] failed with exit status 2: some error")
}
