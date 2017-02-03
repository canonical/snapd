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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type timeutilSuite struct{}

var _ = Suite(&timeutilSuite{})

func (ts *timeutilSuite) TestParseSchedule(c *C) {
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
		{"9:00-11:00", []*timeutil.Schedule{&timeutil.Schedule{Start: timeutil.TimeOnly{Hour: 9}, End: timeutil.TimeOnly{Hour: 11}}}, ""},
		{"mon@9:00-11:00", []*timeutil.Schedule{&timeutil.Schedule{Weekday: "mon", Start: timeutil.TimeOnly{Hour: 9}, End: timeutil.TimeOnly{Hour: 11}}}, ""},
		{"9:00-11:00,20:00-22:00", []*timeutil.Schedule{&timeutil.Schedule{Start: timeutil.TimeOnly{Hour: 9}, End: timeutil.TimeOnly{Hour: 11}}, &timeutil.Schedule{Start: timeutil.TimeOnly{Hour: 20}, End: timeutil.TimeOnly{Hour: 22}}}, ""},
		{"mon@9:00-11:00,Wednesday@22:00-23:00", []*timeutil.Schedule{&timeutil.Schedule{Weekday: "mon", Start: timeutil.TimeOnly{Hour: 9}, End: timeutil.TimeOnly{Hour: 11}}, &timeutil.Schedule{Weekday: "wednesday", Start: timeutil.TimeOnly{Hour: 22}, End: timeutil.TimeOnly{Hour: 23}}}, ""},
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

func (ts *timeutilSuite) TestParseTime(c *C) {
	for _, t := range []struct {
		timeStr      string
		hour, minute int
		errStr       string
	}{
		{"8:59", 8, 59, ""},
		{"08:59", 8, 59, ""},
		{"12:00", 12, 0, ""},
		{"xx", 0, 0, `cannot parse "xx"`},
		{"11:61", 0, 0, `cannot parse "11:61"`},
		{"25:00", 0, 0, `cannot parse "25:00"`},
	} {
		ti, err := timeutil.ParseTime(t.timeStr)
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr)
		} else {
			c.Check(err, IsNil)
			c.Check(ti.Hour, Equals, t.hour)
			c.Check(ti.Minute, Equals, t.minute)
		}
	}
}

func (ts *timeutilSuite) TestScheduleMatches(c *C) {
	for _, t := range []struct {
		schedStr string
		timeStr  string
		matches  bool
	}{
		{"9:00-11:00", "8:59", false},
		{"9:00-11:00", "9:00", true},
		{"9:00-11:00", "11:00", true},
		{"9:00-11:00", "11:01", false},
	} {
		sched, err := timeutil.ParseSchedule(t.schedStr)
		c.Assert(err, IsNil)
		c.Assert(sched, HasLen, 1)

		ti, err := timeutil.ParseTime(t.timeStr)
		c.Assert(err, IsNil)

		d := time.Date(2017, 02, 03, ti.Hour, ti.Minute, 0, 0, time.UTC)

		c.Check(sched[0].Matches(d), Equals, t.matches, Commentf("invalid match for %q with time %q, expected %v, got %v", t.schedStr, t.timeStr, t.matches, sched[0].Matches(d)))
	}
}
