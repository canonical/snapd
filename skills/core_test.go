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

package skills

import (
	. "gopkg.in/check.v1"
)

type CoreSuite struct{}

var _ = Suite(&CoreSuite{})

func (s *CoreSuite) TestValidateName(c *C) {
	c.Assert(ValidateName("name with space"), ErrorMatches,
		`"name with space" is not a valid skill or slot name`)
	c.Assert(ValidateName("name-with-trailing-dash-"), ErrorMatches,
		`"name-with-trailing-dash-" is not a valid skill or slot name`)
	c.Assert(ValidateName("name-with-3-dashes"), IsNil)
	c.Assert(ValidateName("name"), IsNil)
}
