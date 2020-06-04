// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package sanity_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sanity"
)

func (s *sanitySuite) TestCheckTimePast(c *C) {
	restore := sanity.MockTimeNow(func() time.Time {
		return time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	})
	defer restore()

	err := sanity.CheckTime()
	c.Check(err, ErrorMatches, "current time 2016-01-01 00:00:00 \\+0000 UTC is not realistic, clock is not set")
}

func (s *sanitySuite) TestCheckTimeFuture(c *C) {
	// Future as of this writing.
	restore := sanity.MockTimeNow(func() time.Time {
		return time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC)
	})
	defer restore()

	err := sanity.CheckTime()
	c.Check(err, IsNil)
}
