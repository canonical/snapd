// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package timeutil_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type timeutilSuite struct{}

var _ = Suite(&timeutilSuite{})

func (ts *timeutilSuite) TestSchedule(c *C) {
	for _, t := range []struct {
		in       string
		expected []*timeutil.Schedule
		errStr   string
	}{
		// invalid
		{"", nil, `cannot parse "": not a valid interval`},
		{"invalid-11:00", nil, `cannot parse "invalid": not a valid time`},
		{"9:00-11:00,invalid", nil, `cannot parse "invalid": not a valid interval`},
		{"09:00-25:00", nil, `cannot parse "25:00": not a valid time`},
		// FIXME: error message sucks
		{"9:00-mon@11:00", nil, `cannot parse "9:00-mon": not a valid day`},

		// valid
		{"9:00-11:00", []*timeutil.Schedule{&timeutil.Schedule{Start: "9:00", End: "11:00"}}, ""},
		{"mon@9:00-11:00", []*timeutil.Schedule{&timeutil.Schedule{Weekday: "mon", Start: "9:00", End: "11:00"}}, ""},
		{"9:00-11:00,20:00-22:00", []*timeutil.Schedule{&timeutil.Schedule{Start: "9:00", End: "11:00"}, &timeutil.Schedule{Start: "20:00", End: "22:00"}}, ""},
		{"mon@9:00-11:00,Wednesday@22:00-23:00", []*timeutil.Schedule{&timeutil.Schedule{Weekday: "mon", Start: "9:00", End: "11:00"}, &timeutil.Schedule{Weekday: "wednesday", Start: "22:00", End: "23:00"}}, ""},
	} {
		schedule, err := timeutil.ParseSchedule(t.in)
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr, Commentf("%q returned unexpected error: %s", err))
		} else {
			c.Check(err, IsNil, Commentf("%q returned error: %s", t.in, err))
			c.Check(schedule, DeepEquals, t.expected, Commentf("%q failed", t.in))
		}

	}
}
