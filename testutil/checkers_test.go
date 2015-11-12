// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package testutil

import (
	. "gopkg.in/check.v1"
	"testing"
)

func Test2(t *testing.T) {
	TestingT(t)
}

type CheckersSuite struct{}

var _ = Suite(&CheckersSuite{})

func (s *CheckersSuite) TestUnsupportedTypes(c *C) {
	c.ExpectFailure("haystack is of unsupported type int")
	c.Assert(5, Contains, "foo")
}

func (s *CheckersSuite) TestContainsVerifiesTypes(c *C) {
	c.ExpectFailure("haystack contains items of type int but needle is a string")
	c.Assert([...]int{1, 2, 3}, Contains, "foo")
	c.Assert([]int{1, 2, 3}, Contains, "foo")
	// This looks tricky, Contains looks at _values_, not at keys
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Contains, "foo")
}

func (s *CheckersSuite) TestContainsString(c *C) {
	c.Assert("foo", Contains, "f")
	c.Assert("foo", Contains, "fo")
	c.Assert("foo", Not(Contains), "foobar")
}

func (s *CheckersSuite) TestContainsArray(c *C) {
	c.Assert([...]int{1, 2, 3}, Contains, 1)
	c.Assert([...]int{1, 2, 3}, Contains, 2)
	c.Assert([...]int{1, 2, 3}, Contains, 3)
	c.Assert([...]int{1, 2, 3}, Not(Contains), 4)
}

func (s *CheckersSuite) TestContainsSlice(c *C) {
	c.Assert([]int{1, 2, 3}, Contains, 1)
	c.Assert([]int{1, 2, 3}, Contains, 2)
	c.Assert([]int{1, 2, 3}, Contains, 3)
	c.Assert([]int{1, 2, 3}, Not(Contains), 4)
}

func (s *CheckersSuite) TestContainsMap(c *C) {
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Contains, 1)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Contains, 2)
	c.Assert(map[string]int{"foo": 1, "bar": 2}, Not(Contains), 3)
}
