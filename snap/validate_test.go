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
	"fmt"
	"regexp"

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
		"01game", "1-or-2",
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
		{Name: "a"},
		{Name: "aaa"},
		{Name: "a-a"},
		{Name: "aa-a"},
		{Name: "a-aa"},
		{Name: "a-b-c"},
	}
	for _, hook := range validHooks {
		err := ValidateHook(hook)
		c.Assert(err, IsNil)
	}
	invalidHooks := []*HookInfo{
		{Name: ""},
		{Name: "a a"},
		{Name: "a--a"},
		{Name: "-a"},
		{Name: "a-"},
		{Name: "0"},
		{Name: "123"},
		{Name: "123abc"},
		{Name: "日本語"},
	}
	for _, hook := range invalidHooks {
		err := ValidateHook(hook)
		c.Assert(err, ErrorMatches, `invalid hook name: ".*"`)
	}
}

// ValidateApp

func (s *ValidateSuite) TestValidateAppName(c *C) {
	validAppNames := []string{
		"1", "a", "aa", "aaa", "aaaa", "Aa", "aA", "1a", "a1", "1-a", "a-1",
		"a-a", "aa-a", "a-aa", "a-b-c", "0a-a", "a-0a",
	}
	for _, name := range validAppNames {
		c.Check(ValidateApp(&AppInfo{Name: name}), IsNil)
	}
	invalidAppNames := []string{
		"", "-", "--", "a--a", "a-", "a ", " a", "a a", "日本語", "한글",
		"ру́сский язы́к", "ໄຂ່​ອີ​ສ​ເຕີ້", ":a", "a:", "a:a", "_a", "a_", "a_a",
	}
	for _, name := range invalidAppNames {
		err := ValidateApp(&AppInfo{Name: name})
		c.Assert(err, ErrorMatches, `cannot have ".*" as app name.*`)
	}
}

func (s *ValidateSuite) TestAppWhitelistSimple(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo"}), IsNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo"}), IsNil)
}

func (s *ValidateSuite) TestAppWhitelistIllegal(c *C) {
	c.Check(ValidateApp(&AppInfo{Name: "x\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "test!me"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", Command: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", StopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", PostStopCommand: "foo\n"}), NotNil)
	c.Check(ValidateApp(&AppInfo{Name: "foo", BusName: "foo\n"}), NotNil)
}

func (s *ValidateSuite) TestAppDaemonValue(c *C) {
	for _, t := range []struct {
		daemon string
		ok     bool
	}{
		// good
		{"", true},
		{"simple", true},
		{"forking", true},
		{"oneshot", true},
		{"dbus", true},
		{"notify", true},
		// bad
		{"invalid-thing", false},
	} {
		if t.ok {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon}), IsNil)
		} else {
			c.Check(ValidateApp(&AppInfo{Name: "foo", Daemon: t.daemon}), ErrorMatches, fmt.Sprintf(`"daemon" field contains invalid value %q`, t.daemon))
		}
	}
}

func (s *ValidateSuite) TestAppWhitelistError(c *C) {
	err := ValidateApp(&AppInfo{Name: "foo", Command: "x\n"})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, `app description field 'command' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:-]*$')`)
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
	hookType := NewHookType(regexp.MustCompile(".*"))
	restore := MockSupportedHookTypes([]*HookType{hookType})
	defer restore()

	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
hooks:
  123abc:
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `invalid hook name: "123abc"`)
}

func (s *ValidateSuite) TestPlugSlotNamesUnique(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: snap
plugs:
 foo:
slots:
 foo:
`))
	c.Assert(err, IsNil)
	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have plug and slot with the same name: "foo"`)
}

func (s *ValidateSuite) TestIllegalAliasName(c *C) {
	info, err := InfoFromSnapYaml([]byte(`name: foo
version: 1.0
apps:
  foo:
    aliases: [foo$]
`))
	c.Assert(err, IsNil)

	err = Validate(info)
	c.Check(err, ErrorMatches, `cannot have "foo\$" as alias name for app "foo" - use only letters, digits, dash, underscore and dot characters`)
}

func (s *ValidateSuite) TestValidateAlias(c *C) {
	validAliases := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"a_a", "aa_a", "a_aa", "a_b_c",
		"a0", "a_0", "a_0a",
		"01game", "1-or-2",
		"0.1game", "1_or_2",
	}
	for _, alias := range validAliases {
		err := ValidateAlias(alias)
		c.Assert(err, IsNil)
	}
	invalidAliases := []string{
		"",
		"_foo",
		"-foo",
		".foo",
		"foo$",
	}
	for _, alias := range invalidAliases {
		err := ValidateAlias(alias)
		c.Assert(err, ErrorMatches, `invalid alias name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateLayout(c *C) {
	// Several invalid layouts.
	c.Check(ValidateLayout(&Layout{}),
		ErrorMatches, "cannot accept layout with empty path")
	c.Check(ValidateLayout(&Layout{Path: "/foo"}),
		ErrorMatches, `cannot determine layout for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Bind: "/bar", Type: "tmpfs"}),
		ErrorMatches, `cannot accept conflicting layout for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Bind: "/bar", Symlink: "/froz"}),
		ErrorMatches, `cannot accept conflicting layout for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Type: "tmpfs", Symlink: "/froz"}),
		ErrorMatches, `cannot accept conflicting layout for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Type: "ext4"}),
		ErrorMatches, `cannot accept filesystem "ext4" for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo/bar", Type: "tmpfs", User: "foo"}),
		ErrorMatches, `cannot accept user "foo" for "/foo/bar"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo/bar", Type: "tmpfs", Group: "foo"}),
		ErrorMatches, `cannot accept group "foo" for "/foo/bar"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Type: "tmpfs", Mode: 01755}),
		ErrorMatches, `cannot accept mode 01755 for "/foo"`)
	c.Check(ValidateLayout(&Layout{Path: "$FOO", Type: "tmpfs"}),
		ErrorMatches, `cannot accept layout of "\$FOO": reference to unknown variable "\$FOO"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Bind: "$BAR"}),
		ErrorMatches, `cannot accept layout of "/foo": reference to unknown variable "\$BAR"`)
	c.Check(ValidateLayout(&Layout{Path: "/foo", Symlink: "$BAR"}),
		ErrorMatches, `cannot accept layout of "/foo": reference to unknown variable "\$BAR"`)
	// Several valid layouts.
	c.Check(ValidateLayout(&Layout{Path: "/tmp", Type: "tmpfs"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/usr", Bind: "$SNAP/usr"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/var", Bind: "$SNAP_DATA/var"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/var", Bind: "$SNAP_COMMON/var"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/etc/foo.conf", Symlink: "$SNAP_DATA/etc/foo.conf"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/a/b", Type: "tmpfs", User: "nobody"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/a/b", Type: "tmpfs", User: "root"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/a/b", Type: "tmpfs", Group: "nobody"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/a/b", Type: "tmpfs", Group: "root"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/a/b", Type: "tmpfs", Mode: 0655}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/usr", Symlink: "$SNAP/usr"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/var", Symlink: "$SNAP_DATA/var"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "/var", Symlink: "$SNAP_COMMON/var"}), IsNil)
	c.Check(ValidateLayout(&Layout{Path: "$SNAP/data", Symlink: "$SNAP_DATA"}), IsNil)
}
