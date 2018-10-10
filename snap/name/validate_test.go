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

package name_test

import (
	. "gopkg.in/check.v1"

	snapname "github.com/snapcore/snapd/snap/name"

	"github.com/snapcore/snapd/testutil"
)

type ValidateSuite struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateSuite{})

func (s *ValidateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ValidateSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *ValidateSuite) TestValidateName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
		// a regexp stresser
		"u-94903713687486543234157734673284536758",
	}
	for _, name := range validNames {
		err := snapname.ValidateSnap(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// names cannot be too long
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"1111111111111111111111111111111111111111x",
		"x1111111111111111111111111111111111111111",
		"x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x-x",
		// a regexp stresser
		"u-9490371368748654323415773467328453675-",
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
		err := snapname.ValidateSnap(name)
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateInstanceName(c *C) {
	validNames := []string{
		// plain names are also valid instance names
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		// snap instance
		"foo_bar",
		"foo_0123456789",
		"01game_0123456789",
		"foo_1", "foo_1234abcd",
	}
	for _, name := range validNames {
		err := snapname.ValidateInstance(name)
		c.Assert(err, IsNil)
	}
	invalidNames := []string{
		// invalid names are also invalid instance names, just a few
		// samples
		"",
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"a--a",
		"a-",
		"a ", " a", "a a",
		"_",
		"ру́сский_язы́к",
	}
	for _, name := range invalidNames {
		err := snapname.ValidateInstance(name)
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
	invalidInstanceKeys := []string{
		// the snap names are valid, but instance keys are not
		"foo_", "foo_1-23", "foo_01234567890", "foo_123_456",
		"foo__bar",
	}
	for _, name := range invalidInstanceKeys {
		err := snapname.ValidateInstance(name)
		c.Assert(err, ErrorMatches, `invalid instance key: ".*"`)
	}

}

func (s *ValidateSuite) TestValidateHookName(c *C) {
	validHooks := []string{
		"a",
		"aaa",
		"a-a",
		"aa-a",
		"a-aa",
		"a-b-c",
		"valid",
	}
	for _, hook := range validHooks {
		err := snapname.ValidateHook(hook)
		c.Assert(err, IsNil)
	}
	invalidHooks := []string{
		"",
		"a a",
		"a--a",
		"-a",
		"a-",
		"0",
		"123",
		"123abc",
		"日本語",
	}
	for _, hook := range invalidHooks {
		err := snapname.ValidateHook(hook)
		c.Assert(err, ErrorMatches, `invalid hook name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateAppName(c *C) {
	validAppNames := []string{
		"1", "a", "aa", "aaa", "aaaa", "Aa", "aA", "1a", "a1", "1-a", "a-1",
		"a-a", "aa-a", "a-aa", "a-b-c", "0a-a", "a-0a",
	}
	for _, name := range validAppNames {
		c.Check(snapname.ValidateApp(name), IsNil)
	}
	invalidAppNames := []string{
		"", "-", "--", "a--a", "a-", "a ", " a", "a a", "日本語", "한글",
		"ру́сский язы́к", "ໄຂ່​ອີ​ສ​ເຕີ້", ":a", "a:", "a:a", "_a", "a_", "a_a",
	}
	for _, name := range invalidAppNames {
		err := snapname.ValidateApp(name)
		c.Assert(err, ErrorMatches, `invalid app name: ".*"`)
	}
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
		err := snapname.ValidateAlias(alias)
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
		err := snapname.ValidateAlias(alias)
		c.Assert(err, ErrorMatches, `invalid alias name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateSocketName(c *C) {
	validNames := []string{
		"a", "aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
	}
	for _, name := range validNames {
		err := snapname.ValidateSocket(name)
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
		// no null chars in the string are allowed
		"aa-a\000-b",
	}
	for _, name := range invalidNames {
		err := snapname.ValidateSocket(name)
		c.Assert(err, ErrorMatches, `invalid socket name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateSlotPlugInterfaceName(c *C) {
	valid := []string{
		"a",
		"aaa",
		"a-a",
		"aa-a",
		"a-aa",
		"a-b-c",
		"valid",
		"valid-123",
	}
	for _, name := range valid {
		err := snapname.ValidateSlot(name)
		c.Assert(err, IsNil)
		err = snapname.ValidatePlug(name)
		c.Assert(err, IsNil)
		err = snapname.ValidateInterface(name)
		c.Assert(err, IsNil)
	}
	invalid := []string{
		"",
		"a a",
		"a--a",
		"-a",
		"a-",
		"0",
		"123",
		"123abc",
		"日本語",
	}
	for _, name := range invalid {
		err := snapname.ValidateSlot(name)
		c.Assert(err, ErrorMatches, `invalid slot name: ".*"`)
		err = snapname.ValidatePlug(name)
		c.Assert(err, ErrorMatches, `invalid plug name: ".*"`)
		err = snapname.ValidateInterface(name)
		c.Assert(err, ErrorMatches, `invalid interface name: ".*"`)
	}
}
