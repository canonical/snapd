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
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type timeutilSuite struct{}

var _ = Suite(&timeutilSuite{})

func (ts *timeutilSuite) TestTimeOfDay(c *C) {
	td := timeutil.TimeOfDay{Hour: 23, Minute: 59}
	c.Check(td.Add(time.Minute), Equals, timeutil.TimeOfDay{Hour: 0, Minute: 0})

	td = timeutil.TimeOfDay{Hour: 5, Minute: 34}
	c.Check(td.Add(time.Minute), Equals, timeutil.TimeOfDay{Hour: 5, Minute: 35})

	td = timeutil.TimeOfDay{Hour: 10, Minute: 1}
	c.Check(td.Sub(timeutil.TimeOfDay{Hour: 10, Minute: 0}), Equals, time.Minute)
}

func (ts *timeutilSuite) TestParseTimeOfDay(c *C) {
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

func (ts *timeutilSuite) TestScheduleString(c *C) {
	for _, t := range []struct {
		sched timeutil.Schedule
		str   string
	}{
		{timeutil.Schedule{Start: timeutil.TimeOfDay{Hour: 13, Minute: 41}, End: timeutil.TimeOfDay{Hour: 14, Minute: 59}}, "13:41-14:59"},
	} {
		c.Check(t.sched.String(), Equals, t.str)
	}
}

func (ts *timeutilSuite) TestParseSchedule(c *C) {
	for _, t := range []struct {
		in       string
		expected []*timeutil.Schedule
		errStr   string
	}{
		// invalid
		{"", nil, `cannot parse "": not a valid interval`},
		{"invalid-11:00", nil, `cannot parse "invalid": not a valid time`},
		{"9:00-11:00/invalid", nil, `cannot parse "invalid": not a valid interval`},
		{"09:00-25:00", nil, `cannot parse "25:00": not a valid time`},
		// moving backwards
		{"11:00-09:00", nil, `cannot parse "11:00-09:00": time in an interval cannot go backwards`},
		{"23:00-01:00", nil, `cannot parse "23:00-01:00": time in an interval cannot go backwards`},

		// valid
		{"9:00-11:00", []*timeutil.Schedule{{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}}, ""},
		{"9:00-11:00/20:00-22:00", []*timeutil.Schedule{{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}, {Start: timeutil.TimeOfDay{Hour: 20}, End: timeutil.TimeOfDay{Hour: 22}}}, ""},
	} {
		schedule, err := timeutil.ParseSchedule(t.in)
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr, Commentf("%q returned unexpected error: %s", t.in, err))
		} else {
			c.Check(err, IsNil, Commentf("%q returned error: %s", t.in, err))
			c.Check(schedule, DeepEquals, t.expected, Commentf("%q failed", t.in))
		}

	}
}

func (ts *timeutilSuite) TestIsValidWeekday(c *C) {
	for _, t := range []struct {
		in       string
		expected bool
	}{
		{"mon", true},
		{"tue", true},
		{"wed", true},
		{"thu", true},
		{"fri", true},
		{"sat", true},
		{"sun", true},
		{"foo", false},
		{"bar", false},
		{"barsatfu", false},
	} {
		c.Check(t.expected, Equals, timeutil.IsValidWeekday(t.in),
			Commentf("%q returned unexpected value for, expected %v", t.in, t.expected))
	}
}

func parse(c *C, s string) (time.Duration, time.Duration) {
	l := strings.Split(s, "-")
	c.Assert(l, HasLen, 2)
	a, err := time.ParseDuration(l[0])
	c.Assert(err, IsNil)
	b, err := time.ParseDuration(l[1])
	c.Assert(err, IsNil)
	return a, b
}

func (ts *timeutilSuite) TestScheduleNext(c *C) {
	const shortForm = "2006-01-02 15:04"

	for _, t := range []struct {
		schedule string
		last     string
		now      string
		next     string
	}{
		{
			// daily schedule, missed one window
			// -> run next daily window
			schedule: "9:00-11:00/21:00-23:00",
			last:     "2017-02-05 22:00",
			now:      "2017-02-06 20:00",
			next:     "1h-3h",
		},
		{
			// daily schedule, used one window
			// -> run next daily window
			schedule: "9:00-11:00/21:00-23:00",
			last:     "2017-02-06 10:00",
			now:      "2017-02-06 20:00",
			next:     "1h-3h",
		},
		{
			// daily schedule, missed all todays windows
			// run tomorrow
			schedule: "9:00-11:00/21:00-22:00",
			last:     "2017-02-04 21:30",
			now:      "2017-02-06 23:00",
			next:     "10h-12h",
		},
		{
			// single daily schedule, already updated today
			schedule: "9:00-11:00",
			last:     "2017-02-06 09:30",
			now:      "2017-02-06 10:00",
			next:     "23h-25h",
		},
		{
			// single daily schedule, already updated today
			// (at exactly the edge)
			schedule: "9:00-11:00",
			last:     "2017-02-06 09:00",
			now:      "2017-02-06 09:00",
			next:     "24h-26h",
		},
		{
			// single daily schedule, last update a day ago
			// now is within the update window so randomize
			// (run within remaining time delta)
			schedule: "9:00-11:00",
			last:     "2017-02-05 09:30",
			now:      "2017-02-06 10:00",
			next:     "0-55m",
		},
		{
			// multi daily schedule, already updated today
			schedule: "9:00-11:00/21:00-22:00",
			last:     "2017-02-06 21:30",
			now:      "2017-02-06 23:00",
			next:     "10h-12h",
		},
		{
			// daily schedule, very small window
			schedule: "9:00-9:03",
			last:     "2017-02-05 09:02",
			now:      "2017-02-06 08:58",
			next:     "2m-5m",
		},
		{
			// daily schedule, zero window
			schedule: "9:00-9:00",
			last:     "2017-02-05 09:02",
			now:      "2017-02-06 08:58",
			next:     "2m-2m",
		},
	} {
		last, err := time.ParseInLocation(shortForm, t.last, time.Local)
		c.Assert(err, IsNil)

		fakeNow, err := time.ParseInLocation(shortForm, t.now, time.Local)
		c.Assert(err, IsNil)
		restorer := timeutil.MockTimeNow(func() time.Time {
			return fakeNow
		})
		defer restorer()

		sched, err := timeutil.ParseSchedule(t.schedule)
		c.Assert(err, IsNil)
		minDist, maxDist := parse(c, t.next)

		next := timeutil.Next(sched, last)
		c.Check(next >= minDist && next <= maxDist, Equals, true, Commentf("invalid  distance for schedule %q with last refresh %q, now %q, expected %v, got %v", t.schedule, t.last, t.now, t.next, next))
	}

}
