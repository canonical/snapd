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

func (ts *timeutilSuite) TestParseTimeOfDay(c *C) {
	for _, t := range []struct {
		timeStr              string
		hour, minute, second int
		errStr               string
	}{
		{"8:59", 8, 59, 0, ""},
		{"8:59:12", 8, 59, 12, ""},
		{"08:59", 8, 59, 0, ""},
		{"12:00", 12, 0, 0, ""},
		{"xx", 0, 0, 0, `cannot parse "xx"`},
		{"11:61", 0, 0, 0, `cannot parse "11:61"`},
		{"25:00", 0, 0, 0, `cannot parse "25:00"`},
		{"11:59:61", 0, 0, 0, `cannot parse "11:59:61"`},
	} {
		ti, err := timeutil.ParseTime(t.timeStr)
		if t.errStr != "" {
			c.Check(err, ErrorMatches, t.errStr)
		} else {
			c.Check(err, IsNil)
			c.Check(ti.Hour, Equals, t.hour)
			c.Check(ti.Minute, Equals, t.minute)
			c.Check(ti.Second, Equals, t.second)
		}
	}
}

func (ts *timeutilSuite) TestOrderTimeOfDay(c *C) {
	for _, t := range []struct {
		t1, t2 string
		isLess bool
	}{
		{"9:00", "10:00", true},
		{"9:00", "9:01", true},
		{"9:00:00", "9:00:01", true},
		{"10:00", "9:00", false},
		{"9:00", "9:00", false},
	} {
		t1, err := timeutil.ParseTime(t.t1)
		c.Assert(err, IsNil)
		t2, err := timeutil.ParseTime(t.t2)
		c.Assert(err, IsNil)
		c.Check(t1.Less(t2), Equals, t.isLess, Commentf("incorrect result for %#v", t))
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
		{"11:00-09:00", nil, `cannot parse "11:00-09:00": not a valid interval`},
		{"23:00-01:00", nil, `cannot parse "23:00-01:00": not a valid interval`},
		// FIXME: error message sucks
		{"9:00-mon@11:00", nil, `cannot parse "9:00-mon", want "mon", "tue", etc`},

		// valid
		{"9:00-11:00", []*timeutil.Schedule{{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}}, ""},
		{"mon@9:00-11:00", []*timeutil.Schedule{{Weekday: "mon", Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}}, ""},
		{"9:00-11:00/20:00-22:00", []*timeutil.Schedule{{Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}, {Start: timeutil.TimeOfDay{Hour: 20}, End: timeutil.TimeOfDay{Hour: 22}}}, ""},
		{"mon@9:00-11:00/Wed@22:00-23:00", []*timeutil.Schedule{{Weekday: "mon", Start: timeutil.TimeOfDay{Hour: 9}, End: timeutil.TimeOfDay{Hour: 11}}, {Weekday: "wed", Start: timeutil.TimeOfDay{Hour: 22}, End: timeutil.TimeOfDay{Hour: 23}}}, ""},
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

func (ts *timeutilSuite) TestScheduleDistance(c *C) {
	const shortForm = "2006-01-02 3:04"

	for _, t := range []struct {
		schedStr string
		timeStr  string
		distance time.Duration
	}{
		// same interval
		{"9:00-11:00", "2017-02-05 9:00", 0},
		{"9:00-11:00", "2017-02-05 10:00", 0},
		{"9:00-11:00", "2017-02-05 11:00", 0},
		// same day
		{"9:00-11:00", "2017-02-05 8:00", 1 * time.Hour},
		// next day
		{"9:00-11:00", "2017-02-05 12:00", 21 * time.Hour},
		// same weekday
		{"sun@9:00-11:00", "2017-02-05 8:00", 1 * time.Hour},
		// tomorrow
		{"mon@9:00-11:00", "2017-02-05 8:00", 25 * time.Hour},
		{"mon@9:00-11:00", "2017-02-05 12:00", 21 * time.Hour},
		// the day after tomorrow
		{"tue@9:00-11:00", "2017-02-05 8:00", 49 * time.Hour},
		{"tue@9:00-11:00", "2017-02-05 12:00", 45 * time.Hour},
		// schedule on sunday, distance from monday
		{"sun@9:00-11:00", "2017-02-06 8:00", 145 * time.Hour},
	} {
		ti, err := time.Parse(shortForm, t.timeStr)
		c.Assert(err, IsNil)

		sched, err := timeutil.ParseSchedule(t.schedStr)
		c.Assert(err, IsNil)
		c.Assert(sched, HasLen, 1)

		c.Check(sched[0].Distance(ti), Equals, t.distance, Commentf("invalid distance for schedule %q with time %q, expected %v, got %v", t.schedStr, t.timeStr, t.distance, sched[0].Distance(ti)))
	}

}

func (ts *timeutilSuite) TestScheduleNext(c *C) {
	const shortForm = "2006-01-02 3:04"

	for _, t := range []struct {
		schedStr string
		timeStr  string
		distance time.Duration
	}{
		// same interval
		{"6:00-7:00/9:00-11:00", "2017-02-05 11:00", 0},
		// same day
		{"6:00-7:00/9:00-11:00/14:00-15:00", "2017-02-05 8:00", 1 * time.Hour},
		// next day
		{"6:00-7:00/9:00-11:00", "2017-02-05 12:00", 18 * time.Hour},
		// same weekday
		{"sun@9:00-11:00/6:00-7:00", "2017-02-05 8:00", 1 * time.Hour},
		// tomorrow
		{"mon@9:00-11:00/wed@9:00-11:00", "2017-02-05 8:00", 25 * time.Hour},
	} {
		ti, err := time.Parse(shortForm, t.timeStr)
		c.Assert(err, IsNil)

		sched, err := timeutil.ParseSchedule(t.schedStr)
		c.Assert(err, IsNil)

		shortest := timeutil.Next(sched, ti)
		c.Check(shortest, Equals, t.distance, Commentf("invalid distance for schedule %q with time %q, expected %v, got %v", t.schedStr, t.timeStr, t.distance, shortest))
	}

}

func (ts *timeutilSuite) TestScheduleMatches(c *C) {
	const shortForm = "2006-01-02 3:04"

	for _, t := range []struct {
		schedStr string
		fakeNow  string
		matches  bool
	}{
		// 2017-02-05 is a Sunday
		{"9:00-11:00", "2017-02-05 8:59", false},
		{"9:00-11:00", "2017-02-05 9:00", true},
		{"9:00-11:00", "2017-02-05 11:00", true},
		{"9:00-11:00", "2017-02-05 11:01", false},
		// 2017-02-06 is a Monday
		{"mon@9:00-11:00", "2017-02-06 9:00", true},
		// 2017-02-07 is a Tuesday
		{"mon@9:00-11:00", "2017-02-07 9:00", false},
	} {
		fakeNow, err := time.Parse(shortForm, t.fakeNow)
		c.Assert(err, IsNil)

		sched, err := timeutil.ParseSchedule(t.schedStr)
		c.Assert(err, IsNil)
		c.Assert(sched, HasLen, 1)

		c.Check(sched[0].Matches(fakeNow), Equals, t.matches, Commentf("invalid match for %q with time %q, expected %v, got %v", t.schedStr, t.fakeNow, t.matches, sched[0].Matches(fakeNow)))
	}
}

func (ts *timeutilSuite) TestScheduleSameInterval(c *C) {
	const shortForm = "2006-01-02 3:04"

	for _, t := range []struct {
		schedStr string

		t1       string
		t2       string
		expected bool
	}{
		// not matched intervals are always false
		{"9:00-11:00", "2017-02-05 8:59", "2017-02-05 8:59", false},

		// same day, same interval
		{"9:00-11:00", "2017-02-05 9:00", "2017-02-05 9:20", true},
		{"9:00-11:00", "2017-02-05 9:00", "2017-02-05 10:59", true},

		// different days
		{"9:00-11:00", "2017-02-05 9:00", "2017-02-06 10:59", false},
		{"9:00-11:00", "2017-02-05 9:00", "2017-02-17 09:00", false},

		// weekly schedule, matching day
		{"sun@9:00-11:00", "2017-02-05 9:00", "2017-02-05 10:59", true},

		// weekly schedule, not matching day
		{"sun@9:00-11:00", "2017-02-05 9:00", "2017-02-07 10:59", false},
		// different weeks
		{"sun@9:00-11:00", "2017-02-12 9:00", "2017-02-05 10:59", false},
	} {
		t1, err := time.Parse(shortForm, t.t1)
		c.Assert(err, IsNil)
		t2, err := time.Parse(shortForm, t.t2)
		c.Assert(err, IsNil)

		sched, err := timeutil.ParseSchedule(t.schedStr)
		c.Assert(err, IsNil)
		c.Assert(sched, HasLen, 1)

		res := sched[0].SameInterval(t1, t2)
		c.Check(res, Equals, t.expected, Commentf("SameInterval(%q,%q) for %q returned %v, expected %v", t.t1, t.t2, t.schedStr, res, t.expected))
	}
}
