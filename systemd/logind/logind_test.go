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
	"os"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd/logind"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type logindSuite struct{}

var _ = Suite(&logindSuite{})

// sessionClassLoginctlMock returns a loginctl mock for logind.SessionClass,
// responding to the "show-user <uid> -p Display" invocation with
// displayOutput, and to the "show-session c5 -p Class" invocation with
// classOutput.
func sessionClassLoginctlMock(c *C, displayOutput, classOutput string) func(ctx context.Context, args ...string) ([]byte, error) {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "show-user":
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "--all", "-p", "Display"})
			return []byte(displayOutput), nil
		case "show-session":
			c.Check(args, DeepEquals, []string{"show-session", "c5", "--all", "-p", "Class"})
			return []byte(classOutput), nil
		}
		return nil, nil
	}
}

func (s *logindSuite) TestSessionClass(c *C) {
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
		restore := logind.MockLoginctl(sessionClassLoginctlMock(c, "Display=c5\n", "Class="+class+"\n"))
		defer restore()

		got, err := logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)

		// Try without trailing \n on the show-session output
		restore = logind.MockLoginctl(sessionClassLoginctlMock(c, "Display=c5\n", "Class="+class))
		defer restore()

		got, err = logind.SessionClass(context.Background())
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)
	}
}

func (s *logindSuite) TestSessionClassNoSession(c *C) {
	var loginctlErr *logind.Error
	loginctlErr = &logind.Error{}
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("Failed to look up user"))

	restore := logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "--all", "-p", "Display"})
		return nil, loginctlErr
	})
	defer restore()

	_, err := logind.SessionClass(context.Background())
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "loginctl command .* failed with exit status 1: Failed to look up user")

	// an empty "Display=" means the user has no display session
	for _, displayOutput := range []string{"Display=\n", "Display="} {
		restore := logind.MockLoginctl(sessionClassLoginctlMock(c, displayOutput, "Class=user\n"))
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, ErrorMatches, "cannot find display-eligible session for the current user: .*")
	}

	// the display session may vanish between the two calls
	loginctlErr = &logind.Error{}
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("No session 'c5' known"))

	calls := 0
	restore = logind.MockLoginctl(func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		switch args[0] {
		case "show-user":
			c.Check(args, DeepEquals, []string{"show-user", strconv.Itoa(os.Getuid()), "--all", "-p", "Display"})
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

func (s *logindSuite) TestSessionClassMalformedOutput(c *C) {
	for _, output := range []string{"", "unexpected-no-equals\n", "foo=c5\n", "Display=foo=x\n"} {
		restore := logind.MockLoginctl(sessionClassLoginctlMock(c, output, "Class=user\n"))
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Display": .*`)
	}

	for _, output := range []string{"", "unexpected-no-equals\n", "foo=user\n", "Class=foo=\n", "Class=", "Class=\n"} {
		restore := logind.MockLoginctl(sessionClassLoginctlMock(c, "Display=c5\n", output))
		defer restore()

		_, err := logind.SessionClass(context.Background())
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, `cannot parse value from loginctl output for property "Class": .*`)
	}
}

func (s *logindSuite) TestSessionClassWithWhitespace(c *C) {
	restore := logind.MockLoginctl(sessionClassLoginctlMock(c, "  Display=c5  \n", "Class=user\n"))
	defer restore()

	got, err := logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "user")

	// no trailing \n on the display session id
	restore = logind.MockLoginctl(sessionClassLoginctlMock(c, "Display=c5", "Class=user\n"))
	defer restore()

	got, err = logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "user")

	restore = logind.MockLoginctl(sessionClassLoginctlMock(c, "Display=c5\n", "  Class=user  \n"))
	defer restore()

	got, err = logind.SessionClass(context.Background())
	c.Assert(err, IsNil)
	c.Check(got, Equals, "user")
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
