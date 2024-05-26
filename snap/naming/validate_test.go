// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package naming_test

import (
	"fmt"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap/naming"
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
		"aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
		// a regexp stresser
		"u-94903713687486543234157734673284536758",
	}
	for _, name := range validNames {
		mylog.Check(naming.ValidateSnap(name))

	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// too short (min 2 chars)
		"a",
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
		mylog.Check(naming.ValidateSnap(name))
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateInstanceName(c *C) {
	validNames := []string{
		// plain names are also valid instance names
		"aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		// snap instance
		"foo_bar",
		"foo_0123456789",
		"01game_0123456789",
		"foo_1", "foo_1234abcd",
	}
	for _, name := range validNames {
		mylog.Check(naming.ValidateInstance(name))

	}
	invalidNames := []string{
		// invalid names are also invalid instance names, just a few
		// samples
		"",
		"a",
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		"xxxxxxxxxxxxxxxxxxxx-xxxxxxxxxxxxxxxxxxxx",
		"a--a",
		"a-",
		"a ", " a", "a a",
		"_",
		"ру́сский_язы́к",
	}
	for _, name := range invalidNames {
		mylog.Check(naming.ValidateInstance(name))
		c.Assert(err, ErrorMatches, `invalid snap name: ".*"`)
	}
	invalidInstanceKeys := []string{
		// the snap names are valid, but instance keys are not
		"foo_", "foo_1-23", "foo_01234567890", "foo_123_456",
		"foo__bar",
	}
	for _, name := range invalidInstanceKeys {
		mylog.Check(naming.ValidateInstance(name))
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
		mylog.Check(naming.ValidateHook(hook))

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
		mylog.Check(naming.ValidateHook(hook))
		c.Assert(err, ErrorMatches, `invalid hook name: ".*"`)
	}
	// Regression test for https://bugs.launchpad.net/snapd/+bug/1638988
	c.Assert(naming.ValidateHook("connect-plug-i2c"), IsNil)
}

func (s *ValidateSuite) TestValidateAppName(c *C) {
	validAppNames := []string{
		"1", "a", "aa", "aaa", "aaaa", "Aa", "aA", "1a", "a1", "1-a", "a-1",
		"a-a", "aa-a", "a-aa", "a-b-c", "0a-a", "a-0a",
	}
	for _, name := range validAppNames {
		c.Check(naming.ValidateApp(name), IsNil)
	}
	invalidAppNames := []string{
		"", "-", "--", "a--a", "a-", "a ", " a", "a a", "日本語", "한글",
		"ру́сский язы́к", "ໄຂ່​ອີ​ສ​ເຕີ້", ":a", "a:", "a:a", "_a", "a_", "a_a",
	}
	for _, name := range invalidAppNames {
		mylog.Check(naming.ValidateApp(name))
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
		mylog.Check(naming.ValidateAlias(alias))

	}
	invalidAliases := []string{
		"",
		"_foo",
		"-foo",
		".foo",
		"foo$",
	}
	for _, alias := range invalidAliases {
		mylog.Check(naming.ValidateAlias(alias))
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
		mylog.Check(naming.ValidateSocket(name))

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
		mylog.Check(naming.ValidateSocket(name))
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
		mylog.Check(naming.ValidateSlot(name))

		mylog.Check(naming.ValidatePlug(name))

		mylog.Check(naming.ValidateInterface(name))

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
		mylog.Check(naming.ValidateSlot(name))
		c.Assert(err, ErrorMatches, `invalid slot name: ".*"`)
		mylog.Check(naming.ValidatePlug(name))
		c.Assert(err, ErrorMatches, `invalid plug name: ".*"`)
		mylog.Check(naming.ValidateInterface(name))
		c.Assert(err, ErrorMatches, `invalid interface name: ".*"`)
	}
}

func (s *ValidateSuite) TestValidateSnapID(c *C) {
	c.Check(naming.ValidateSnapID("buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ"), IsNil)

	invalid := []string{
		"",
		"buPKUD3TKqC",
		"buPKUD3TKqCOgLE-jHx5kSiCpIs5cMuQ",
		"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQxxx",
	}
	for _, id := range invalid {
		mylog.Check(naming.ValidateSnapID(id))
		c.Check(err, ErrorMatches, fmt.Sprintf("invalid snap-id: %q", id))
	}
}

func (s *ValidateSuite) TestValidateSecurityTag(c *C) {
	// valid snap names, snap instances, app names and hook names are accepted.
	c.Check(naming.ValidateSecurityTag("snap.pkg.app"), IsNil)
	c.Check(naming.ValidateSecurityTag("snap.pkg.hook.configure"), IsNil)
	c.Check(naming.ValidateSecurityTag("snap.pkg_key.app"), IsNil)
	c.Check(naming.ValidateSecurityTag("snap.pkg_key.hook.configure"), IsNil)

	// invalid format is rejected
	c.Check(naming.ValidateSecurityTag("snap.pkg_key.app.surprise"), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg_key.hook.configure.surprise"), ErrorMatches, "invalid security tag")

	// invalid snap and app names are rejected.
	c.Check(naming.ValidateSecurityTag("snap._.app"), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg._"), ErrorMatches, "invalid security tag")

	// invalid number of components are rejected.
	c.Check(naming.ValidateSecurityTag("snap.pkg.hook.surprise."), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg.hook."), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg.hook"), IsNil) // Perhaps somewhat unexpectedly, this tag is valid.
	c.Check(naming.ValidateSecurityTag("snap.pkg.app.surprise"), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg."), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap.pkg"), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap."), ErrorMatches, "invalid security tag")
	c.Check(naming.ValidateSecurityTag("snap"), ErrorMatches, "invalid security tag")
}

func (s *ValidateSuite) TestValidQuotaGroup(c *C) {
	validNames := []string{
		"aa", "aaa", "aaaa",
		"a-a", "aa-a", "a-aa", "a-b-c",
		"a0", "a-0", "a-0a",
		"01game", "1-or-2",
		// a regexp stresser
		"u-94903713687486543234157734673284536758",
	}
	for _, name := range validNames {
		mylog.Check(naming.ValidateQuotaGroup(name))

	}
	invalidNames := []string{
		// name cannot be empty
		"",
		// too short (min 2 chars)
		"a",
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
		mylog.Check(naming.ValidateQuotaGroup(name))
		c.Assert(err, ErrorMatches, `invalid quota group name:.*`)
	}
}

func (s *ValidateSuite) TestValidateProvenance(c *C) {
	c.Check(naming.ValidateProvenance("a"), IsNil)
	c.Check(naming.ValidateProvenance("123A-abz-dd3Z9"), IsNil)

	c.Check(naming.ValidateProvenance(""), ErrorMatches, `invalid provenance: must not be empty`)

	invalid := []string{
		"+",
		"-",
		"--",
		"a--z",
	}
	for _, prov := range invalid {
		mylog.Check(naming.ValidateProvenance(prov))
		c.Check(err, ErrorMatches, regexp.QuoteMeta(fmt.Sprintf("invalid provenance: %q", prov)))
	}
}
