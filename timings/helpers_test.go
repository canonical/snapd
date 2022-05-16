// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package timings_test

import (
	"encoding/json"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timings"
)

func (s *timingsSuite) TestStartupTimestampMsg(c *C) {
	type msgTimestamp struct {
		Stage string `json:"stage"`
		Time  string `json:"time"`
	}

	now := time.Date(2022, time.May, 16, 10, 43, 12, 22312000, time.UTC)
	timings.MockTimeNow(func() time.Time {
		return now
	})
	msg := timings.StartupTimestampMsg("foo")
	c.Check(msg, Equals, `{"stage":"foo", "time":"1652697792.022312"}`)

	var m msgTimestamp
	err := json.Unmarshal([]byte(msg), &m)
	c.Assert(err, IsNil)
	c.Check(m, Equals, msgTimestamp{
		Stage: "foo",
		Time:  "1652697792.022312",
	})
}
