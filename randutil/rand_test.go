// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package randutil_test

import (
	"math/rand"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/randutil"
)

func Test(t *testing.T) { TestingT(t) }

type randutilSuite struct{}

var _ = Suite(&randutilSuite{})

func (s *randutilSuite) TestMakeRandomString(c *C) {
	// for our tests
	rand.Seed(1)

	s1 := randutil.MakeRandomString(10)
	c.Assert(s1, Equals, "pw7MpXh0JB")

	s2 := randutil.MakeRandomString(5)
	c.Assert(s2, Equals, "4PQyl")
}
