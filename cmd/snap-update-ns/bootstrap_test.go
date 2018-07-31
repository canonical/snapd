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

// Check that ValidateSnapName rejects "/" and "..".
func (s *bootstrapSuite) TestValidateSnapName(c *C) {
	c.Assert(update.ValidateInstanceName("hello-world"), Equals, 0)
	c.Assert(update.ValidateInstanceName("hello/world"), Equals, -1)
	c.Assert(update.ValidateInstanceName("hello..world"), Equals, -1)
	c.Assert(update.ValidateInstanceName("hello-world_foo"), Equals, 0)
	c.Assert(update.ValidateInstanceName("foo_0123456789"), Equals, 0)
	c.Assert(update.ValidateInstanceName("foo_1234abcd"), Equals, 0)
	c.Assert(update.ValidateInstanceName("INVALID"), Equals, -1)
	c.Assert(update.ValidateInstanceName("-invalid"), Equals, -1)
	c.Assert(update.ValidateInstanceName(""), Equals, -1)
	c.Assert(update.ValidateInstanceName("hello-world_"), Equals, -1)
	c.Assert(update.ValidateInstanceName("_foo"), Equals, -1)
	c.Assert(update.ValidateInstanceName("foo_01234567890"), Equals, -1)
	c.Assert(update.ValidateInstanceName("foo_123_456"), Equals, -1)
	c.Assert(update.ValidateInstanceName("foo__456"), Equals, -1)
}

// Test various cases of command line handling.
func (s *bootstrapSuite) TestProcessArguments(c *C) {
	cases := []struct {
		cmdline     []string
		snapName    string
		shouldSetNs bool
		userFstab   bool
		errPattern  string
	}{
		// Corrupted buffer is dealt with.
		{[]string{}, "", false, false, "argv0 is corrupted"},
		// When testing real bootstrap is identified and disabled.
		{[]string{"argv0.test"}, "", false, false, "bootstrap is not enabled while testing"},
		// Snap name is mandatory.
		{[]string{"argv0"}, "", false, false, "snap name not provided"},
		// Snap name is parsed correctly.
		{[]string{"argv0", "snapname"}, "snapname", true, false, ""},
		{[]string{"argv0", "snapname_instance"}, "snapname_instance", true, false, ""},
		// Onlye one snap name is allowed.
		{[]string{"argv0", "snapone", "snaptwo"}, "", false, false, "too many positional arguments"},
		// Snap name is validated correctly.
		{[]string{"argv0", ""}, "", false, false, "snap name must contain at least one letter"},
		{[]string{"argv0", "in--valid"}, "", false, false, "snap name cannot contain two consecutive dashes"},
		{[]string{"argv0", "invalid-"}, "", false, false, "snap name cannot end with a dash"},
		{[]string{"argv0", "@invalid"}, "", false, false, "snap name must use lower case letters, digits or dashes"},
		{[]string{"argv0", "INVALID"}, "", false, false, "snap name must use lower case letters, digits or dashes"},
		{[]string{"argv0", "foo_01234567890"}, "", false, false, "instance key must be shorter than 10 characters"},
		// The option --from-snap-confine disables setns.
		{[]string{"argv0", "--from-snap-confine", "snapname"}, "snapname", false, false, ""},
		{[]string{"argv0", "snapname", "--from-snap-confine"}, "snapname", false, false, ""},
		// The option --user-mounts switches to the real uid
		{[]string{"argv0", "--user-mounts", "snapname"}, "snapname", false, true, ""},
		// Unknown options are reported.
		{[]string{"argv0", "-invalid"}, "", false, false, "unsupported option"},
		{[]string{"argv0", "--option"}, "", false, false, "unsupported option"},
		{[]string{"argv0", "--from-snap-confine", "-invalid", "snapname"}, "", false, false, "unsupported option"},
	}
	for _, tc := range cases {
		snapName, shouldSetNs, userFstab := update.ProcessArguments(tc.cmdline)
		err := update.BootstrapError()
		comment := Commentf("failed with cmdline %q, expected error pattern %q, actual error %q",
			tc.cmdline, tc.errPattern, err)
		if tc.errPattern != "" {
			c.Assert(err, ErrorMatches, tc.errPattern, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
		c.Check(snapName, Equals, tc.snapName, comment)
		c.Check(shouldSetNs, Equals, tc.shouldSetNs, comment)
		c.Check(userFstab, Equals, tc.userFstab, comment)
	}
}
