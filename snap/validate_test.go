// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snap_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/snap"
)

type ValidateSuite struct{}

var _ = Suite(&ValidateSuite{})

func (s *ValidateSuite) TestValidateName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
	}
	for _, name := range validNames {
		err := ValidateName(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// dashes alone are not a name
		"-", "--",
		// double dashes in a name are not allowed
		"a--a",
		// name should not end with a dash
		"a-",
		// name cannot have any spaces in it
		"a ", " a", "a a",
		// a number alone is not a name
		"0", "123",
		// identifier must be plain ASCII
		"日本語", "한글", "ру́сский язы́к",
	}
	for _, name := range invalidNames {
		err := ValidateName(name)
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
}

// ValidateApp

func (s *ValidateSuite) TestAppWhitelistSimple(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Command: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Stop: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{PostStop: "foo"}), IsNil)
}

func (s *ValidateSuite) TestAppWhitelistIllegal(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "x\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "test!me"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Command: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Stop: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{PostStop: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{SocketMode: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{ListenStream: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{BusName: "foo\n"}), NotNil)
}

func (s *ValidateSuite) TestAppDaemonValue(c *C) {
	c.Check(ValidateApp(&AppInfo{Daemon: "oneshot"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Daemon: "nono"}), ErrorMatches, `"daemon" field contains invalid value "nono"`)
}

func (s *ValidateSuite) TestAppWhitelistError(c *C) {
	err := ValidateApp(&AppInfo{Name: "x\n"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `app description field 'name' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:-]*$')`)
}
