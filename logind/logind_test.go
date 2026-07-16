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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logind"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type logindSuite struct{}

var _ = Suite(&logindSuite{})

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
		restore := logind.MockLoginctl(func(args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte("Class=" + class + "\n"), nil
		})
		defer restore()

		got, err := logind.SessionClass()
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)

		// Try without trailing \n
		restore = logind.MockLoginctl(func(args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte("Class=" + class), nil
		})
		defer restore()

		got, err = logind.SessionClass()
		c.Assert(err, IsNil)
		c.Check(got, Equals, class)
	}
}

func (s *logindSuite) TestSessionClassNoSession(c *C) {
	var loginctlErr *logind.Error
	loginctlErr = &logind.Error{}
	loginctlErr.SetExitCode(1)
	loginctlErr.SetMsg([]byte("No session for PID"))

	restore := logind.MockLoginctl(func(args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return nil, loginctlErr
	})
	defer restore()

	_, err := logind.SessionClass()
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "loginctl command .* failed with exit status 1: No session for PID")
}

func (s *logindSuite) TestSessionClassEmptyOutput(c *C) {
	restore := logind.MockLoginctl(func(args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return []byte(""), nil
	})
	defer restore()

	_, err := logind.SessionClass()
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid property format from loginctl for Class .*")
}

func (s *logindSuite) TestSessionClassMalformedOutput(c *C) {
	for _, output := range []string{"", "unexpected-no-equals\n", "foo=user\n", "Class=\n"} {
		restore := logind.MockLoginctl(func(args ...string) ([]byte, error) {
			c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
			return []byte(output), nil
		})
		defer restore()

		_, err := logind.SessionClass()
		c.Assert(err, NotNil)
		c.Check(err, ErrorMatches, "invalid property format from loginctl for Class .*")
	}
}

func (s *logindSuite) TestSessionClassWithWhitespace(c *C) {
	restore := logind.MockLoginctl(func(args ...string) ([]byte, error) {
		c.Check(args, DeepEquals, []string{"show-session", "auto", "-p", "Class"})
		return []byte("  Class=user  \n"), nil
	})
	defer restore()

	got, err := logind.SessionClass()
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
