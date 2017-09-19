// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	"strings"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

func (s *bootstrapSuite) TestReadCmdLine(c *C) {
	buf := make([]byte, 1024)
	numRead := update.ReadCmdline(buf)
	c.Assert(numRead, Not(Equals), -1)
	c.Assert(numRead, Not(Equals), 1)
	// Individual arguments are separated with NUL byte.
	argv := strings.Split(string(buf[0:numRead]), "\x00")
	// Smoke test, the actual value looks like
	// "/tmp/go-build020699516/github.com/snapcore/snapd/cmd/snap-update-ns/_test/snap-update-ns.test"
	c.Assert(strings.HasSuffix(argv[0], "snap-update-ns.test"), Equals, true, Commentf("argv[0] is %q", argv[0]))
}

// Check that if there is only one argument we return nil.
func (s *bootstrapSuite) TestFindSnapName1(c *C) {
	buf := []byte("arg0\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if there are multiple arguments we return the 2nd one.
func (s *bootstrapSuite) TestFindSnapName2(c *C) {
	buf := []byte("arg0\x00arg1\x00arg2\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, Not(Equals), (*string)(nil))
	c.Assert(*result, Equals, "arg1")
}

// Check that if the 1st argument in the buffer is not terminated we don't crash.
func (s *bootstrapSuite) TestFindSnapName3(c *C) {
	buf := []byte("arg0")
	result := update.FindSnapName(buf)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if the 2nd argument in the buffer is not terminated we don't crash.
func (s *bootstrapSuite) TestFindSnapName4(c *C) {
	buf := []byte("arg0\x00arg1")
	result := update.FindSnapName(buf)
	c.Assert(result, Not(Equals), (*string)(nil))
	c.Assert(*result, Equals, "arg1")
}

// Check that if the 2nd argument an empty string we return NULL.
func (s *bootstrapSuite) TestFindSnapName5(c *C) {
	buf := []byte("arg0\x00\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if the 1st argument is an option then it is skipped.
func (s *bootstrapSuite) TestFindSnapName6(c *C) {
	buf := []byte("arg0\x00--option\x00snap\x00\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, NotNil)
	c.Assert(*result, Equals, "snap")
}

// Check that if many options are skipped.
func (s *bootstrapSuite) TestFindSnapName7(c *C) {
	buf := []byte("arg0\x00--option\x00--another\x00snap\x00\x00")
	result := update.FindSnapName(buf)
	c.Assert(result, NotNil)
	c.Assert(*result, Equals, "snap")
}

// Check that ValidateSnapName rejects "/" and "..".
func (s *bootstrapSuite) TestValidateSnapName(c *C) {
	c.Assert(update.ValidateSnapName("hello-world"), Equals, 0)
	c.Assert(update.ValidateSnapName("hello/world"), Equals, -1)
	c.Assert(update.ValidateSnapName("hello..world"), Equals, -1)
	c.Assert(update.ValidateSnapName("INVALID"), Equals, -1)
	c.Assert(update.ValidateSnapName("-invalid"), Equals, -1)
}

// Test various cases of command line handling.
func (s *bootstrapSuite) TestProcessArguments(c *C) {
	cases := []struct {
		cmdline     string
		snapName    string
		shouldSetNs bool
		errPattern  string
	}{
		// Corrupted buffer is dealt with.
		{"", "", false, "argv0 is corrupted"},
		// When testing real bootstrap is identified and disabled.
		{"argv0.test\x00", "", false, "bootstrap is not enabled while testing"},
		// Snap name is mandatory.
		{"argv0\x00", "", false, "snap name not provided"},
		// Snap name is parsed correctly.
		{"argv0\x00snapname\x00", "snapname", true, ""},
		// Snap name is validated correctly.
		{"argv0\x00in--valid\x00", "", false, "snap name cannot contain two consecutive dashes"},
		{"argv0\x00invalid-\x00", "", false, "snap name cannot end with a dash"},
		{"argv0\x00@invalid\x00", "", false, "snap name must use lower case letters, digits or dashes"},
		{"argv0\x00INVALID\x00", "", false, "snap name must use lower case letters, digits or dashes"},
		// The option --from-snap-confine disables setns.
		{"argv0\x00--from-snap-confine\x00snapname\x00", "snapname", false, ""},
		// Unknown options are reported.
		{"argv0\x00-invalid\x00", "", false, "unsupported option"},
		{"argv0\x00--option\x00", "", false, "unsupported option"},
	}
	for _, tc := range cases {
		buf := []byte(tc.cmdline)
		snapName, shouldSetNs := update.ProcessArguments(buf)
		err := update.BootstrapError()
		comment := Commentf("failed with cmdline %q, expected error pattern %q, actual error %q",
			tc.cmdline, tc.errPattern, err)
		if tc.errPattern != "" {
			c.Assert(err, ErrorMatches, tc.errPattern, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
		c.Check(snapName, Equals, tc.snapName)
		c.Check(shouldSetNs, Equals, tc.shouldSetNs)
	}
}
