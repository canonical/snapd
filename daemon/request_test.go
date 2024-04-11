// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package daemon_test

import (
	"time"

	"github.com/snapcore/snapd/daemon"
	"gopkg.in/check.v1"
)

type requestSuite struct{}

var _ = check.Suite(&requestSuite{})

func (s *requestSuite) TestParseDateHasNanosecondPrecision(c *check.C) {
	oDateTime := time.Date(2024, time.April, 11, 15, 5, 3, 123456789, time.UTC).Format(time.RFC3339Nano)
	dateTime, err := daemon.ParseOptionalTime(oDateTime)
	c.Assert(err, check.IsNil)
	c.Assert(dateTime, check.NotNil)
	c.Assert(dateTime.Nanosecond(), check.Equals, 123456789)
}
