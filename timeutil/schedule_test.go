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
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 13, Minute: 41},
					End: timeutil.TimeOfDay{Hour: 14, Minute: 59}},
			},
		}, "13:41-14:59"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{{
				Start: timeutil.TimeOfDay{Hour: 13, Minute: 41},
				End:   timeutil.TimeOfDay{Hour: 14, Minute: 59}}},
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon"}}},
		}, "mon,13:41-14:59"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 13, Minute: 41},
					End: timeutil.TimeOfDay{Hour: 14, Minute: 59}, Spread: true}},
		}, "13:41~14:59"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 6}, End: timeutil.TimeOfDay{Hour: 6}}},
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon"}, End: timeutil.Weekday{Day: "fri"}}},
		}, "mon-fri,06:00"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 6}, End: timeutil.TimeOfDay{Hour: 6}},
				{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 14},
					Spread: true, Split: 2}},
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon"}, End: timeutil.Weekday{Day: "fri"}},
				{Start: timeutil.Weekday{Day: "sat"}}},
		}, "mon-fri,sat,06:00,09:00~14:00/2"},

		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 6}, End: timeutil.TimeOfDay{Hour: 6}}},
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon", Pos: 1}, End: timeutil.Weekday{Day: "fri", Pos: 1}}},
		}, "mon1-fri1,06:00"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 6}, End: timeutil.TimeOfDay{Hour: 6}}},
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon", Pos: 5}}},
		}, "mon5,06:00"},
		{timeutil.Schedule{
			Week: []timeutil.WeekSpan{
				{Start: timeutil.Weekday{Day: "mon"}}},
		}, "mon"},
		{timeutil.Schedule{
			Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 6}, End: timeutil.TimeOfDay{Hour: 9},
					Spread: true, Split: 2}},
		}, "06:00~09:00/2"},
	} {
		c.Check(t.sched.String(), Equals, t.str)
	}
}

func (ts *timeutilSuite) TestParseLegacySchedule(c *C) {
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
		{"09:00-24:30", nil, `cannot parse "24:30": not a valid time`},

		// valid
		{"9:00-11:00", []*timeutil.Schedule{
			{Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}, Spread: true}}},
		}, ""},
		{"9:00-11:00/20:00-22:00", []*timeutil.Schedule{
			{Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}, Spread: true}}},
			{Times: []timeutil.TimeSpan{
				{Start: timeutil.TimeOfDay{Hour: 20}, End: timeutil.TimeOfDay{Hour: 22}, Spread: true}}},
		}, ""},
	} {
		c.Logf("trying: %v", t)
		schedule, err := timeutil.ParseLegacySchedule(t.in)
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

func (ts *timeutilSuite) TestLegacyScheduleNext(c *C) {
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

		sched, err := timeutil.ParseLegacySchedule(t.schedule)
		c.Assert(err, IsNil)
		minDist, maxDist := parse(c, t.next)

		next := timeutil.Next(sched, last)
		c.Check(next >= minDist && next <= maxDist, Equals, true, Commentf("invalid  distance for schedule %q with last refresh %q, now %q, expected %v, got %v", t.schedule, t.last, t.now, t.next, next))
	}

}

func (ts *timeutilSuite) TestParseSchedule(c *C) {
	for _, t := range []struct {
		in       string
		expected []*timeutil.Schedule
		errStr   string
	}{
		// invalid
		{"", nil, `cannot parse "": not a valid event`},
		{"invalid-11:00", nil, `cannot parse "invalid-11:00": not a valid event`},
		{"9:00-11:00/invalid", nil, `cannot parse "9:00-11:00/invalid": not a valid event interval`},
		{"9:00-11:00/0", nil, `cannot parse "9:00-11:00/0": not a valid event interval`},
		{"09:00-25:00", nil, `cannot parse "09:00-25:00": not a valid event`},
		{"09:00-24:30", nil, `cannot parse "09:00-24:30": not a valid event`},
		{"mon-01:00", nil, `cannot parse "mon-01:00": not a valid event`},
		{"9:00-mon@11:00", nil, `cannot parse "9:00-mon@11:00": not a valid event`},
		{"9:00,mon", nil, `cannot parse "9:00,mon": expected time spec`},
		{"mon~wed", nil, `cannot parse "mon~wed": expected time spec`},
		{"mon-wed/2..9:00", nil, `cannot parse "mon-wed/2": expected time spec`},
		{"mon9,9:00", nil, `cannot parse "mon9,9:00": not a valid event`},
		{"mon0,9:00", nil, `cannot parse "mon0,9:00": not a valid event`},
		{"mon5-mon1,9:00", nil, `cannot parse "mon5-mon1": unsupported schedule`},
		{"mon-mon2,9:00", nil, `cannot parse "mon-mon2": mixed weekday and nonweekday`},
		{"mon%,9:00", nil, `cannot parse "mon%,9:00": not a valid event`},
		{"foo2,9:00", nil, `cannot parse "foo2,9:00": not a valid event`},
		// valid
		{
			in: "9:00-11:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}},
			}},
		},
		{
			in: "9:00-11:00/2",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11},
						Split: 2}},
			}},
		},
		{
			in: "mon,9:00-11:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}},
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}}},
			}},
		},
		{
			in: "fri,mon,9:00-11:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}},
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "fri"}},
					{Start: timeutil.Weekday{Day: "mon"}}},
			}},
		},
		{
			in: "9:00-11:00..20:00-22:00",
			expected: []*timeutil.Schedule{
				{Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}}},
				{Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 20}, End: timeutil.TimeOfDay{Hour: 22}}}},
			},
		},
		{
			in: "mon,9:00-11:00..wed,22:00-23:00",
			expected: []*timeutil.Schedule{
				{
					Times: []timeutil.TimeSpan{{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}},
					Week:  []timeutil.WeekSpan{{Start: timeutil.Weekday{Day: "mon"}}},
				},
				{
					Times: []timeutil.TimeSpan{{Start: timeutil.TimeOfDay{Hour: 22}, End: timeutil.TimeOfDay{Hour: 23}}},
					Week:  []timeutil.WeekSpan{{Start: timeutil.Weekday{Day: "wed"}}},
				},
			},
		},
		{
			in: "mon,9:00,10:00,14:00,15:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}},
					{Start: timeutil.TimeOfDay{Hour: 10}},
					{Start: timeutil.TimeOfDay{Hour: 14}},
					{Start: timeutil.TimeOfDay{Hour: 15}},
				},
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}},
				},
			}},
		},
		{
			in: "mon,wed",
			expected: []*timeutil.Schedule{{
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}},
					{Start: timeutil.Weekday{Day: "wed"}},
				},
			}},
		},
		// same as above
		{
			in: "mon..wed",
			expected: []*timeutil.Schedule{
				{Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}}}},
				{Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "wed"}}}},
			},
		},
		// but not the same as this one
		{
			in: "mon-wed",
			expected: []*timeutil.Schedule{{
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}, End: timeutil.Weekday{Day: "wed"}}},
			}},
		},
		{
			in: "mon-wed,fri,9:00-11:00/2",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11},
						Split: 2},
				},
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon"}, End: timeutil.Weekday{Day: "wed"}},
					{Start: timeutil.Weekday{Day: "fri"}},
				},
			}},
		},
		{
			in: "9:00~11:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11},
						Spread: true},
				},
			}},
		},
		{
			in: "9:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}},
				},
			}},
		},
		{
			in: "mon1,9:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 9}},
				},
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "mon", Pos: 1}},
				},
			}},
		},
		{
			in: "00:00-24:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 0}, End: timeutil.TimeOfDay{Hour: 24}},
				},
			}},
		},
		// 4 events during the whole day - 0:00-24:00/4
		{
			in: "-/4",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 0}, End: timeutil.TimeOfDay{Hour: 24},
						Split: 4},
				},
			}},
		},
		// randomized variant of above
		{
			in: "~/4",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 0}, End: timeutil.TimeOfDay{Hour: 24},
						Spread: true, Split: 4},
				},
			}},
		},
		{
			in: "23:00-01:00",
			expected: []*timeutil.Schedule{{
				Times: []timeutil.TimeSpan{
					{Start: timeutil.TimeOfDay{Hour: 23}, End: timeutil.TimeOfDay{Hour: 1}},
				},
			}},
		},
		{
			in: "fri-mon",
			expected: []*timeutil.Schedule{{
				Week: []timeutil.WeekSpan{
					{Start: timeutil.Weekday{Day: "fri"}, End: timeutil.Weekday{Day: "mon"}}},
			}},
		},
	} {
		c.Logf("trying %+v", t)
		schedule, err := timeutil.ParseSchedule(t.in)
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr, Commentf("%q returned unexpected error: %s", t.in, err))
		} else {
			c.Check(err, IsNil, Commentf("%q returned error: %s", t.in, err))
			c.Check(schedule, DeepEquals, t.expected, Commentf("%q failed", t.in))
		}
	}
}

func (ts *timeutilSuite) TestScheduleNext(c *C) {
	const shortForm = "2006-01-02 15:04"

	for _, t := range []struct {
		schedule   string
		last       string
		now        string
		next       string
		randomized bool
	}{
		{
			schedule: "mon,10:00..fri,15:00",
			// sun 22:00
			last: "2017-02-05 22:00",
			// mon 9:00
			now:  "2017-02-06 9:00",
			next: "1h-1h",
		},
		{
			// first monday of the month, at 10:00
			schedule: "mon1,10:00",
			// Sun 22:00
			last: "2017-02-05 22:00",
			// Mon 9:00
			now:  "2017-02-06 9:00",
			next: "1h-1h",
		},
		{
			// first Monday of the month, at 10:00
			schedule: "mon1,10:00",
			// first Monday of the month, 10:00
			last: "2017-02-06 10:00",
			// first Monday of the month, 11:00, right after
			// 'previous first Monday' run
			now: "2017-02-06 11:00",
			// expecting March, 6th, 10:00, 27 days and 23 hours
			// from now
			next: "671h-671h",
		},
		{
			// second Monday of the month, at 10:00
			schedule: "mon2,10:00",
			// first Monday of the month, 9:00
			last: "2017-02-06 10:00",
			// first Monday of the month, 11:00, right after
			// 'previous first Monday' run
			now: "2017-02-06 11:00",
			// expecting February, 13, 10:00, 6 days and 23 hours
			// from now
			next: "167h-167h",
		},
		{
			// last Monday of the month, at 10:00
			schedule: "mon5,10:00",
			// first Monday of the month, 9:00
			last: "2017-02-06 10:00",
			// first Monday of the month, 11:00, right after
			// 'previous first Monday' run
			now: "2017-02-06 11:00",
			// expecting February, 27th, 10:00, 20 days and 23 hours
			// from now
			next: "503h-503h",
		},
		{
			// from last Monday of the month to the second Tuesday of
			// the month, at 10:00
			schedule: "mon1-tue2,10:00",
			// Sunday, 22:00
			last: "2017-02-05 22:00",
			// first Monday of the month
			now: "2017-02-06 11:00",
			// expecting to run the next day at 10:00
			next: "23h-23h",
		},
		{
			// from last Monday of the month to the second Tuesday of
			// the month, at 10:00
			schedule: "mon1-tue2,10:00-12:00",
			// Sunday, 22:00
			last: "2017-02-05 22:00",
			// first Monday of the month, within the update window
			now: "2017-02-06 11:00",
			// expecting to run now
			next: "0h-0h",
		},
		{
			// from last Monday of the month to the second Tuesday of
			// the month, at 10:00
			schedule: "mon1-tue2,10:00~12:00",
			// Sunday, 22:00
			last: "2017-02-05 22:00",
			// first Monday of the month, within the update window
			now: "2017-02-06 11:00",
			// expecting to run now
			next: "0h-1h",
			// since we're in update window we'll run now regardless
			// of 'spreading'
			randomized: false,
		},
		{
			schedule:   "mon,10:00~12:00..fri,15:00",
			last:       "2017-02-05 22:00",
			now:        "2017-02-06 9:00",
			next:       "1h-3h",
			randomized: true,
		},
		{
			schedule: "mon,10:00-12:00..fri,15:00",
			last:     "2017-02-06 12:00",
			// tue 12:00
			now: "2017-02-07 12:00",
			// 3 days and 3 hours from now
			next: "75h-75h",
		},
		{
			// randomized between 10:00 and 12:00
			schedule: "mon,10:00~12:00",
			// sun 22:00
			last: "2017-02-05 22:00",
			// mon 9:00
			now:        "2017-02-06 9:00",
			next:       "1h-3h",
			randomized: true,
		},
		{
			// Friday to Monday, 10am
			schedule: "fri-mon,10:00",
			// sun 22:00
			last: "2017-02-05 22:00",
			// mon 9:00
			now:  "2017-02-06 9:00",
			next: "1h-1h",
		},
		{
			// Friday to Monday, 10am
			schedule: "fri-mon,10:00",
			// mon 10:00
			last: "2017-02-06 10:00",
			// mon 10:00
			now: "2017-02-06 10:00",
			// 4 days from now
			next: "96h-96h",
		},
		{
			// Wednesday to Friday, 10am
			schedule: "wed-fri,10:00",
			// mon 10:00
			last: "2017-02-06 10:00",
			// mon 10:00
			now: "2017-02-06 10:00",
			// 2 days from now
			next: "48h-48h",
		},
		{
			// randomized, once a day
			schedule: "0:00~24:00",
			// sun 22:00
			last: "2017-02-05 22:00",
			// mon 9:00
			now:        "2017-02-05 23:00",
			next:       "1h-25h",
			randomized: true,
		},
		{
			// randomized, once a day
			schedule: "0:00~24:00",
			// mon 10:00
			last: "2017-02-06 10:00",
			// mon 11:00
			now: "2017-02-06 11:00",
			// sometime the next day
			next:       "13h-37h",
			randomized: true,
		},
		{
			// during the night, 23:00-1:00
			schedule: "23:00~1:00",
			// mon 10:00
			last: "2017-02-06 10:00",
			// mon 11:00
			now: "2017-02-06 22:00",
			// sometime over the night
			next:       "1h-3h",
			randomized: true,
		},
		{
			// during the night, 23:00-1:00
			schedule: "23:00~1:00",
			// Mon 23:00
			last: "2017-02-06 23:00",
			// Tue 0:00
			now: "2017-02-07 00:00",
			// sometime over the night
			next:       "23h-25h",
			randomized: true,
		},
		{
			// twice between 9am and 11am
			schedule: "9:00-11:00/2",
			// last attempt at the beginning of window
			last: "2017-02-06 9:00",
			// sometime between 10am and 11am
			now:  "2017-02-06 9:30",
			next: "30m-90m",
		},
		{
			// 2 ranges
			schedule: "9:00-10:00,10:00-11:00",
			// last attempt at the beginning of window
			last: "2017-02-06 9:01",
			// next one at 10am
			now:  "2017-02-06 9:30",
			next: "30m-30m",
		},
		{
			// twice, at 9am and at 2pm
			schedule: "9:00,14:00",
			// last right after scheduled time window
			last: "2017-02-06 9:01",
			// next one at 2pm
			now:  "2017-02-06 9:30",
			next: "270m-270m",
		},
		{
			// 2 ranges, reversed order in spec
			schedule: "10:00~11:00,9:00-10:00",
			// last attempt at the beginning of window
			last: "2017-02-06 9:01",
			// sometime between 10am and 11am
			now:        "2017-02-06 9:30",
			next:       "30m-90m",
			randomized: true,
		},
	} {
		c.Logf("trying %+v", t)

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

		// keep track of previous result for tests where event time is
		// randomized
		previous := time.Duration(0)
		calls := 2

		for i := 0; i < calls; i++ {
			next := timeutil.Next(sched, last)
			if t.randomized {
				c.Check(next, Not(Equals), previous)
			} else if previous != 0 {
				// not randomized and not the first run
				c.Check(next, Equals, previous)
			}

			c.Logf("next: %v", next)
			minDist, maxDist := parse(c, t.next)

			c.Check(next >= minDist && next <= maxDist,
				Equals, true,
				Commentf("invalid  distance for schedule %q with last refresh %q, now %q, expected %v, got %v",
					t.schedule, t.last, t.now, t.next, next))
			previous = next
		}
	}
}
