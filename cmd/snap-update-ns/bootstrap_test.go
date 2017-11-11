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
	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// Check that if there is only one argument we return nil.
func (s *bootstrapSuite) TestFindSnapName1(c *C) {
	args := []string{"arg0"}
	result := update.FindSnapName(args)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if there are multiple arguments we return the 2nd one.
func (s *bootstrapSuite) TestFindSnapName2(c *C) {
	args := []string{"arg0", "arg1", "arg2"}
	result := update.FindSnapName(args)
	c.Assert(result, Not(Equals), (*string)(nil))
	c.Assert(*result, Equals, "arg1")
}

// Check that if the 2nd argument an empty string we return NULL.
func (s *bootstrapSuite) TestFindSnapName5(c *C) {
	args := []string{"arg0", ""}
	result := update.FindSnapName(args)
	c.Assert(result, Equals, (*string)(nil))
}

// Check that if the 1st argument is an option then it is skipped.
func (s *bootstrapSuite) TestFindSnapName6(c *C) {
	args := []string{"arg0", "--option", "snap"}
	result := update.FindSnapName(args)
	c.Assert(result, NotNil)
	c.Assert(*result, Equals, "snap")
}

// Check that if many options are skipped.
func (s *bootstrapSuite) TestFindSnapName7(c *C) {
	args := []string{"arg0", "--option", "--another", "snap"}
	result := update.FindSnapName(args)
	c.Assert(result, NotNil)
	c.Assert(*result, Equals, "snap")
}

// Check that if there are no options we just return false
func (s *bootstrapSuite) TestHasOption1(c *C) {
	for _, args := range [][]string{
		{""},
		{"arg0"},
		{"arg0", "arg1", "--other-opt"},
	} {
		c.Assert(update.HasOption(args, "-o1"), Equals, false)
	}
}

// Check that if there are are options we return the first one.
func (s *bootstrapSuite) TestHasOption2(c *C) {
	for _, args := range [][]string{
		{"", "-o1"},
		{"arg0", "-o1"},
		{"arg0", "-o1", "arg1"},
		{"arg0", "-o1", "-o2"},
		{"arg0", "-o1", "-o2", "arg1"},
	} {
		c.Assert(update.HasOption(args, "-o1"), Equals, true)
	}
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
		cmdline     []string
		snapName    string
		shouldSetNs bool
		errPattern  string
	}{
		// Corrupted buffer is dealt with.
		{[]string{}, "", false, "argv0 is corrupted"},
		// When testing real bootstrap is identified and disabled.
		{[]string{"argv0.test"}, "", false, "bootstrap is not enabled while testing"},
		// Snap name is mandatory.
		{[]string{"argv0"}, "", false, "snap name not provided"},
		{[]string{"argv0", ""}, "", false, "snap name not provided"},
		// Snap name is parsed correctly.
		{[]string{"argv0", "snapname"}, "snapname", true, ""},
		// Snap name is validated correctly.
		{[]string{"argv0", "in--valid"}, "", false, "snap name cannot contain two consecutive dashes"},
		{[]string{"argv0", "invalid-"}, "", false, "snap name cannot end with a dash"},
		{[]string{"argv0", "@invalid"}, "", false, "snap name must use lower case letters, digits or dashes"},
		{[]string{"argv0", "INVALID"}, "", false, "snap name must use lower case letters, digits or dashes"},
		// The option --from-snap-confine disables setns.
		{[]string{"argv0", "--from-snap-confine", "snapname"}, "snapname", false, ""},
	}
	for _, tc := range cases {
		snapName, shouldSetNs := update.ProcessArguments(tc.cmdline)
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
