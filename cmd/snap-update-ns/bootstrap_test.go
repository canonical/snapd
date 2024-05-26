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

	"github.com/ddkwork/golibrary/mylog"
	update "github.com/snapcore/snapd/cmd/snap-update-ns"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// Check that ValidateSnapName rejects "/" and "..".
func (s *bootstrapSuite) TestValidateInstanceName(c *C) {
	validNames := []string{
		"aa",
		"aa_a",
		"hello-world",
		"a123456789012345678901234567890123456789",
		"a123456789012345678901234567890123456789_0123456789",
		"hello-world_foo",
		"foo_0123456789",
		"foo_1234abcd",
		"a123456789012345678901234567890123456789",
		"a123456789012345678901234567890123456789_0123456789",
	}
	for _, name := range validNames {
		c.Check(update.ValidateInstanceName(name), Equals, 0, Commentf("name %q should be valid but is not", name))
	}

	invalidNames := []string{
		"",
		"a",
		"a_a",
		"a123456789012345678901234567890123456789_01234567890",
		"hello/world",
		"hello..world",
		"INVALID",
		"-invalid",
		"hello-world_",
		"_foo",
		"foo_01234567890",
		"foo_123_456",
		"foo__456",
		"foo_",
		"hello-world_foo_foo",
		"foo01234567890012345678900123456789001234567890",
		"foo01234567890012345678900123456789001234567890_foo",
		"a123456789012345678901234567890123456789_0123456789_",
	}
	for _, name := range invalidNames {
		c.Check(update.ValidateInstanceName(name), Equals, -1, Commentf("name %q should be invalid but is valid", name))
	}
}

// Test various cases of command line handling.
func (s *bootstrapSuite) TestProcessArguments(c *C) {
	cases := []struct {
		cmdline     []string
		snapName    string
		shouldSetNs bool
		userFstab   bool
		uid         uint
		errPattern  string
	}{
		// Corrupted buffer is dealt with.
		{[]string{}, "", false, false, 0, "argv0 is corrupted"},
		// When testing real bootstrap is identified and disabled.
		{[]string{"argv0.test"}, "", false, false, 0, "bootstrap is not enabled while testing"},
		// Snap name is mandatory.
		{[]string{"argv0"}, "", false, false, 0, "snap name not provided"},
		// Snap name is parsed correctly.
		{[]string{"argv0", "snapname"}, "snapname", true, false, 0, ""},
		{[]string{"argv0", "snapname_instance"}, "snapname_instance", true, false, 0, ""},
		// Onlye one snap name is allowed.
		{[]string{"argv0", "snapone", "snaptwo"}, "", false, false, 0, "too many positional arguments"},
		// Snap name is validated correctly.
		{[]string{"argv0", ""}, "", false, false, 0, "snap name must contain at least one letter"},
		{[]string{"argv0", "in--valid"}, "", false, false, 0, "snap name cannot contain two consecutive dashes"},
		{[]string{"argv0", "invalid-"}, "", false, false, 0, "snap name cannot end with a dash"},
		{[]string{"argv0", "@invalid"}, "", false, false, 0, "snap name must use lower case letters, digits or dashes"},
		{[]string{"argv0", "INVALID"}, "", false, false, 0, "snap name must use lower case letters, digits or dashes"},
		{[]string{"argv0", "foo_01234567890"}, "", false, false, 0, "instance key must be shorter than 10 characters"},
		{[]string{"argv0", "foo_0123456_2"}, "", false, false, 0, "snap instance name can contain only one underscore"},
		// The option --from-snap-confine disables setns.
		{[]string{"argv0", "--from-snap-confine", "snapname"}, "snapname", false, false, 0, ""},
		{[]string{"argv0", "snapname", "--from-snap-confine"}, "snapname", false, false, 0, ""},
		// The option --user-mounts switches to the real uid
		{[]string{"argv0", "--user-mounts", "snapname"}, "snapname", false, true, 0, ""},
		// Unknown options are reported.
		{[]string{"argv0", "-invalid"}, "", false, false, 0, "unsupported option"},
		{[]string{"argv0", "--option"}, "", false, false, 0, "unsupported option"},
		{[]string{"argv0", "--from-snap-confine", "-invalid", "snapname"}, "", false, false, 0, "unsupported option"},
		// The -u option can be used to specify the user id.
		{[]string{"argv0", "snapname", "-u", "1234"}, "snapname", true, true, 1234, ""},
		{[]string{"argv0", "-u", "1234", "snapname"}, "snapname", true, true, 1234, ""},
		/* Empty user id is rejected. */
		{[]string{"argv0", "-u", "", "snapname"}, "", false, false, 0, "cannot parse user id"},
		/* Partially parsed values are rejected. */
		{[]string{"argv0", "-u", "1foo", "snapname"}, "", false, false, 0, "cannot parse user id"},
		/* Hexadecimal values are rejected. */
		{[]string{"argv0", "-u", "0x16", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", " 0x16", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", "0x16 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", " 0x16 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		/* Octal-looking values are parsed as decimal. */
		{[]string{"argv0", "-u", "042", "snapname"}, "snapname", true, true, 42, ""},
		/* Spaces around octal values is rejected. */
		{[]string{"argv0", "-u", " 042", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", "042 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", " 042 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		/* Space around the value is rejected. */
		{[]string{"argv0", "-u", "42 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", " 42", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", " 42 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", "\n42 ", "snapname"}, "", false, false, 0, "cannot parse user id"},
		{[]string{"argv0", "-u", "42\t", "snapname"}, "", false, false, 0, "cannot parse user id"},
		/* Negative values are rejected. */
		{[]string{"argv0", "-u", "-1", "snapname"}, "", false, false, 0, "user id cannot be negative"},
		/* The option -u requires an argument. */
		{[]string{"argv0", "snapname", "-u"}, "", false, false, 0, "-u requires an argument"},
	}
	for _, tc := range cases {
		update.ClearBootstrapError()
		snapName, shouldSetNs, userFstab, uid := update.ProcessArguments(tc.cmdline)
		mylog.Check(update.BootstrapError())
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
		c.Check(uid, Equals, tc.uid, comment)
	}
}
