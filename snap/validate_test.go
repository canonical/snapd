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

	. "github.com/snapcore/snapd/snap"
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

func (s *ValidateSuite) TestValidateEpoch(c *C) {
	validEpochs := []string{
		"0", "1*", "1", "400*", "1234",
	}
	for _, epoch := range validEpochs {
		err := ValidateEpoch(epoch)
		c.Assert(err, IsNil)
	}
	invalidEpochs := []string{
		"0*", "_", "1-", "1+", "-1", "+1", "-1*", "a", "1a", "1**",
	}
	for _, epoch := range invalidEpochs {
		err := ValidateEpoch(epoch)
		c.Assert(err, ErrorMatches, `invalid snap epoch: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateHook(c *C) {
	validHooks := []*HookInfo{
		&HookInfo{Name: "a"},
		&HookInfo{Name: "aaa"},
		&HookInfo{Name: "a-a"},
		&HookInfo{Name: "aa-a"},
		&HookInfo{Name: "a-aa"},
		&HookInfo{Name: "a-b-c"},
	}
	for _, hook := range validHooks {
		err := ValidateHook(hook)
		c.Assert(err, IsNil)
	}
	invalidHooks := []*HookInfo{
		&HookInfo{Name: ""},
		&HookInfo{Name: "a a"},
		&HookInfo{Name: "a--a"},
		&HookInfo{Name: "-a"},
		&HookInfo{Name: "a-"},
		&HookInfo{Name: "0"},
		&HookInfo{Name: "123"},
		&HookInfo{Name: "abc0"},
		&HookInfo{Name: "日本語"},
	}
	for _, hook := range invalidHooks {
		err := ValidateHook(hook)
		c.Assert(err, ErrorMatches, `invalid hook name: ".*"`)
	}
}

// ValidateApp

func (s *ValidateSuite) TestAppWhitelistSimple(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Command: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{StopCommand: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{PostStopCommand: "foo"}), IsNil)
}

func (s *ValidateSuite) TestAppWhitelistIllegal(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "x\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "test!me"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Command: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{StopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{PostStopCommand: "foo\n"}), NotNil)
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

// Validate

func (s *ValidateSuite) TestDetectIllegalYamlBinaries(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: someething
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, NotNil)
}

func (s *ValidateSuite) TestDetectIllegalYamlService(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
 tes!me:
   command: something
   daemon: forking
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, NotNil)
}

func (s *ValidateSuite) TestIllegalSnapName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo.something
version: 1.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid snap name: "foo.something"`)
}

func (s *ValidateSuite) TestValidateChecksName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`
version: 1.0
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `snap name cannot be empty`)
}

func (s *ValidateSuite) TestIllegalSnapEpoch(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
epoch: 0*
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid snap epoch: "0\*"`)
}

func (s *ValidateSuite) TestMissingSnapEpochIsOkay(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
`))
	c.Assert(err, IsNil)
	c.Assert(Validate(info), IsNil)
}

func (s *ValidateSuite) TestIllegalHookName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
hooks:
  abc123:
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid hook name: "abc123"`)
}
