// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/testutil"
)

type CompatSuite struct {
	testutil.BaseTest
}

var _ = Suite(&CompatSuite{})

func (s *CompatSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *CompatSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *CompatSuite) TestValidCompatFields(c *C) {
	for i, tc := range []struct {
		compat string
		cfield *builtin.CompatField
	}{
		{"foo", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{0, 0}}}}}},
		{"foo-0", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{0, 0}}}}}},
		{"foo-4", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{4, 4}}}}}},
		{"foo-4-(1..3)", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{4, 4}, {1, 3}}}}}},
		{"foo3e4-4-(1..3)-bar-2025-8-2", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo3e4", Values: []builtin.CompatRange{{4, 4}, {1, 3}}},
			{Tag: "bar", Values: []builtin.CompatRange{{2025, 2025}, {8, 8}, {2, 2}}},
		}}},
		{"foo3e4-4-(1..3)-bar-2025-8-2-libxxlongname-56-(4..90)", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo3e4", Values: []builtin.CompatRange{{4, 4}, {1, 3}}},
			{Tag: "bar", Values: []builtin.CompatRange{{2025, 2025}, {8, 8}, {2, 2}}},
			{Tag: "libxxlongname", Values: []builtin.CompatRange{{56, 56}, {4, 90}}},
		}}},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat)
		c.Assert(err, IsNil)
		c.Check(cfield, DeepEquals, tc.cfield)
	}
}

func (s *CompatSuite) TestInvalidCompatFields(c *C) {
	for i, tc := range []struct {
		compat string
		err    string
	}{
		{"", `bad dimension descriptor "" in compatibility field ""`},
		{"3foo", `bad dimension descriptor "3foo" in compatibility field "3foo"`},
		{"-foo", `bad dimension descriptor "" in compatibility field "-foo"`},
		{"foo-", `invalid tag "" in compatibility field "foo-"`},
		{"foo-2-foo-5", `dimension "foo" appears more than once`},
		{"foo-(0..0)-foo-5-other", `dimension "foo" appears more than once`},
		{"foo-01", `invalid tag "01" in compatibility field "foo-01"`},
		{"foo-(01..5)", `invalid tag "(01..5)" in compatibility field "foo-(01..5)"`},
		{"foo-(1..05)", `invalid tag "(1..05)" in compatibility field "foo-(1..05)"`},
		{"foo-bar-", `invalid tag "" in compatibility field "foo-bar-"`},
		// More than 32 characters in tag
		{"a12345678901234567890123456789012", `bad dimension descriptor "a12345678901234567890123456789012" in compatibility field "a12345678901234567890123456789012"`},
		{"fooBar", `bad dimension descriptor "fooBar" in compatibility field "fooBar"`},
		// More than 8 digits
		{"foo-012345678", `invalid tag "012345678" in compatibility field "foo-012345678"`},
		{"foo-(10..012345678)", `invalid tag "(10..012345678)" in compatibility field "foo-(10..012345678)"`},
		{"foo-bar-baz-other", `only 3 dimensions allowed in compatibility field: "foo-bar-baz-other"`},
		{"bar-(2..5)-3", `ranges only allowed at the end of compatibility field`},
		{"foo-5-1-bar-(2..5)-3", `ranges only allowed at the end of compatibility field`},
		{"foo-1-2-3-4", `only 3 integer/integer ranges allowed per dimension in compatibility field`},
		{"bar-3-(2..1)", `invalid range "(2..1)" in compatibility field "bar-3-(2..1)"`},
		{"bar-3-(2 ..99)", `invalid tag "(2 ..99)" in compatibility field "bar-3-(2 ..99)"`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat)
		c.Check(cfield, IsNil)
		c.Assert(err, NotNil)
		c.Check(err.Error(), Equals, tc.err)
	}
}

func (s *CompatSuite) TestCompatibility(c *C) {
	for i, tc := range []struct {
		compat1, compat2 string
		result           bool
	}{
		{"foo", "bar", false},
		{"foo", "foo", true},
		{"foo-bar", "foo-xxx", false},
		{"foo-bar", "foo", false},
		{"foo-bar", "bar-foo", false},
		{"foo-1-2", "foo-1", false},
		{"foo-2-bar-1", "foo-2-bar-1-2", false},
		{"foo-0", "foo", true},
		{"foo", "foo-(0..0)", true},
		{"foo-0", "foo-(0..0)", true},
		{"foo-1", "foo-(1..1)", true},
		{"foo-(1..1)", "foo-(1..1)", true},
		{"foo-1-2", "foo-1-2", true},
		{"foo-1-(2..3)", "foo-1-2", true},
		{"foo-1-(2..3)", "foo-1-3", true},
		{"foo-1-(2..3)", "foo-1-1", false},
		{"foo-1-(2..3)", "foo-1-4", false},
		{"foo-1-(2..5)-bar", "foo-1-4-bar", true},
		{"foo-1-(2..5)-bar", "foo-1-4-bar-0", true},
		{"foo-1-(2..5)-bar-5-6", "foo-1-4-bar-5-(4..10)", true},
		{"foo-1-(2..3)", "foo-1-(0..1)", false},
		{"foo-1-(2..3)", "foo-1-(0..2)", true},
		{"foo-1-(2..3)", "foo-1-(0..4)", true},
		{"foo-1-(2..3)", "foo-1-(3..4)", true},
		{"foo-1-(2..3)", "foo-1-(4..7)", false},
	} {
		c.Logf("tc %d: %+v", i, tc)

		field1, err := builtin.DecodeCompatField(tc.compat1)
		c.Assert(err, IsNil)
		field2, err := builtin.DecodeCompatField(tc.compat2)
		c.Assert(err, IsNil)
		c.Check(builtin.CheckCompatibility(*field1, *field2), Equals, tc.result)
		// Must be symmetric
		c.Check(builtin.CheckCompatibility(*field2, *field1), Equals, tc.result)
	}
}
