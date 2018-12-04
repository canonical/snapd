// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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

package testutil_test

import (
	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type intCheckersSuite struct{}

var _ = check.Suite(&intCheckersSuite{})

func (*intCheckersSuite) TestIntChecker(c *check.C) {
	c.Assert(1, IntLessThan, 2)
	c.Assert(1, IntLessEqual, 1)
	c.Assert(1, IntEqual, 1)
	c.Assert(2, IntNotEqual, 1)
	c.Assert(2, IntGreaterThan, 1)
	c.Assert(2, IntGreaterEqual, 2)

	// Wrong argument types.
	testCheck(c, IntLessThan, false, "left-hand-side argument must be an int", false, 1)
	testCheck(c, IntLessThan, false, "right-hand-side argument must be an int", 1, false)

	// Relationship error.
	testCheck(c, IntLessThan, false, "relation 2 < 1 is not true", 2, 1)
	testCheck(c, IntLessEqual, false, "relation 2 <= 1 is not true", 2, 1)
	testCheck(c, IntEqual, false, "relation 2 == 1 is not true", 2, 1)
	testCheck(c, IntNotEqual, false, "relation 2 != 2 is not true", 2, 2)
	testCheck(c, IntGreaterThan, false, "relation 1 > 2 is not true", 1, 2)
	testCheck(c, IntGreaterEqual, false, "relation 1 >= 2 is not true", 1, 2)

	// Unexpected relation.
	unexpected := UnexpectedIntChecker("===")
	testCheck(c, unexpected, false, `unexpected relation "==="`, 1, 2)
}
