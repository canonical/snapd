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

package main_test

import (
	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestBootedErrorsOnExtra(c *check.C) {
	_, err := snap.Parser().ParseArgs([]string{"booted", "extra-arg"})
	c.Assert(err, check.ErrorMatches, `too many arguments for command`)
}

func (s *SnapSuite) TestBootedSkippedOnClassic(c *check.C) {
	rest, err := snap.Parser().ParseArgs([]string{"booted"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.DeepEquals, []string{})
	c.Assert(s.Stdout(), check.Equals, "Ignoring 'booted' on classic")
}
