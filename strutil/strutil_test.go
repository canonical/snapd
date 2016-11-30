// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package strutil

import (
	"math/rand"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type MakeRandomStringTestSuite struct{}

var _ = Suite(&MakeRandomStringTestSuite{})

func (ts *MakeRandomStringTestSuite) TestMakeRandomString(c *C) {
	// for our tests
	rand.Seed(1)

	s1 := MakeRandomString(10)
	c.Assert(s1, Equals, "pw7MpXh0JB")

	s2 := MakeRandomString(5)
	c.Assert(s2, Equals, "4PQyl")
}

func (ts *MakeRandomStringTestSuite) TestSizeToStr(c *C) {
	for _, t := range []struct {
		size int64
		str  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{400, "400 B"},
		{1000, "1 kB"},
		{1000 + 1, "1 kB"},
		{900 * 1000, "900 kB"},
		{1000 * 1000, "1 MB"},
		{20 * 1000 * 1000, "20 MB"},
		{1000 * 1000 * 1000, "1 GB"},
		{31 * 1000 * 1000 * 1000, "31 GB"},
	} {
		c.Check(SizeToStr(t.size), Equals, t.str)
	}
}
