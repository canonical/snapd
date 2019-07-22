// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main_test

import (
	"gopkg.in/check.v1"

	snapunset "github.com/snapcore/snapd/cmd/snap"
)

func (s *snapSetSuite) TestInvalidUnsetParameters(c *check.C) {
	invalidParameters := []string{"unset"}
	_, err := snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, "the required arguments `<snap>` and `<conf key> \\(at least 1 argument\\)` were not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)

	invalidParameters = []string{"unset", "snap-name"}
	_, err = snapunset.Parser(snapunset.Client()).ParseArgs(invalidParameters)
	c.Check(err, check.ErrorMatches, "the required argument `<conf key> \\(at least 1 argument\\)` was not provided")
	c.Check(s.setConfApiCalls, check.Equals, 0)
}

func (s *snapSetSuite) TestSnapUnset(c *check.C) {
	// expected value is "nil" as the key is unset
	s.mockSetConfigServer(c, nil)

	_, err := snapunset.Parser(snapunset.Client()).ParseArgs([]string{"unset", "snapname", "key"})
	c.Assert(err, check.IsNil)
	c.Check(s.setConfApiCalls, check.Equals, 1)
}
