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

package timeutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var validTime = regexp.MustCompile(`^([0-9]|0[0-9]|1[0-9]|2[0-3]):([0-5][0-9]):?([0-5][0-9])?$`)

type TimeOfDay struct {
	Hour   int
	Minute int
	Second int
}

func (td TimeOfDay) Less(other TimeOfDay) bool {
	if td.Hour < other.Hour {
		return true
	}
	if td.Minute < other.Minute {
		return true
	}
	if td.Second < other.Second {
		return true
	}
	return false
}

// ParseTime parses a string that contains hour:minute and returns
// an TimeOfDay type or an error
func ParseTime(s string) (t TimeOfDay, err error) {
	m := validTime.FindStringSubmatch(s)
	if len(m) == 0 {
		return t, fmt.Errorf("cannot parse %q", s)
	}
	t.Hour, err = strconv.Atoi(m[1])
	if err != nil {
		return t, fmt.Errorf("cannot parse %q: %s", m[1], err)
	}
	t.Minute, err = strconv.Atoi(m[2])
	if err != nil {
		return t, fmt.Errorf("cannot parse %q: %s", m[2], err)
	}
	if m[3] != "" {
		t.Second, err = strconv.Atoi(m[3])
		if err != nil {
			return t, fmt.Errorf("cannot parse %q: %s", m[3], err)
		}
	}
	return t, nil
}

// Schedule defines a start and end time and an optional weekday in which
// events should run.
type Schedule struct {
	Start TimeOfDay
	End   TimeOfDay

	Weekday string
}

// Matches returns true when the given time is within the schedule
// interval.
func (sched *Schedule) Matches(t time.Time) bool {
	// if the schedule is limited to a specific weekday, we need
	// to check if we are on that day
	if sched.Weekday != "" {
		wd := time.Weekday(weekdayMap[sched.Weekday])
		if t.Weekday() != wd {
			return false
		}
	}

	// check if we are within the schedule time window
	if t.Hour() >= sched.Start.Hour && t.Minute() >= sched.Start.Minute {
		if t.Hour() < sched.End.Hour {
			return true
		}
		if t.Hour() == sched.End.Hour && t.Minute() <= sched.End.Minute {
			return true
		}
	}

	return false
}

// Distance calculdates how long until this schedule can start.
// A zero means "right-now". It is always positive, i.e. either
// its now or at the next possible point in the future.
//
// A schedule can last a week, so the longest distance is 7*24h
func (sched *Schedule) Distance(t time.Time) time.Duration {
	if sched.Matches(t) {
		return 0
	}

	// find the next weekday
	nextDay := t
	if sched.Weekday != "" {
		wd := time.Weekday(weekdayMap[sched.Weekday])
		for nextDay.Weekday() != wd {
			nextDay = nextDay.Add(24 * time.Hour)
		}
	}

	// find the next starting interval
	schedStart := time.Date(t.Year(), t.Month(), nextDay.Day(), sched.Start.Hour, sched.Start.Minute, sched.Start.Second, 0, t.Location())
	d := schedStart.Sub(t)
	// in the past, so needs to happen on the next day
	if d < 0 {
		d = 24*time.Hour + d
	}

	return d
}

// Duration returns the total length of the schedule window
func (sched *Schedule) Duration() time.Duration {
	start := time.Date(2017, 02, 15, sched.Start.Hour, sched.Start.Minute, sched.Start.Second, 0, time.Local)
	end := time.Date(2017, 02, 15, sched.End.Hour, sched.End.Minute, sched.End.Second, 0, time.Local)
	return end.Sub(start)
}

func Next(schedule []*Schedule, last time.Time) time.Duration {
	// 7*24h
	shortestDistance, err := time.ParseDuration("168h")

	if err != nil {
		panic("cannot parse shortest distance")
	}

	now := time.Now()

	for _, sched := range schedule {
		d := sched.Distance(last)
		if d < shortestDistance && !sched.SameInterval(last, now.Add(d)) {
			shortestDistance = d
		}
	}

	// FIXME: randomization to end of interval

	return shortestDistance
}

// SameInterval returns true if the given times are within the same
// interval. Same means that they are on the same day (if its a
// schedule that runs on every day or the same week (if ihts a schedule
// that is run only on a specific weekday).
//
// E.g. for a schedule of "9:00-11:00"
//
// t1="2017-01-01 9:10", t1="2017-01-01 9:30"
// (same day) is the same interval
//
// t1="2017-01-01 9:10", t1="2017-01-02 9:30"
// (different day) is the *not* same interval
//
func (sched *Schedule) SameInterval(t1, t2 time.Time) bool {
	if !sched.Matches(t1) || !sched.Matches(t2) {
		// FIXME: or return error here?
		return false
	}

	if sched.Weekday != "" {
		t1Year, t1Week := t1.ISOWeek()
		t2Year, t2Week := t2.ISOWeek()
		return t1Year == t2Year && t1Week == t2Week
	}

	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day()
}

var weekdayMap = map[string]int{
	"sun": 0,
	"mon": 1,
	"tue": 2,
	"wed": 3,
	"thu": 4,
	"fri": 5,
	"sat": 6,
}

// parseWeekday gets an input like "mon@9:00-11:00" or "9:00-11:00"
// and extracts the weekday of that schedule string (which can be
// empty). It returns the remainder of the string, the weekday
// and an error.
func parseWeekday(s string) (weekday, rest string, err error) {
	if !strings.Contains(s, "@") {
		return "", s, nil
	}
	s = strings.ToLower(s)
	l := strings.SplitN(s, "@", 2)
	weekday = l[0]
	_, ok := weekdayMap[weekday]
	if !ok {
		return "", "", fmt.Errorf(`cannot parse %q, want "mon", "tue", etc`, l[0])
	}
	rest = l[1]

	return weekday, rest, nil
}

// parseTimeInterval gets an input like "9:00-11:00"
// and extracts the start and end of that schedule string and
// returns them and any errors.
func parseTimeInterval(s string) (start, end TimeOfDay, err error) {
	if strings.Contains(s, "@") {
		return start, end, fmt.Errorf("cannot parse %q: contains invalid @", s)
	}
	l := strings.SplitN(s, "-", 2)
	if len(l) != 2 {
		return start, end, fmt.Errorf("cannot parse %q: not a valid interval", s)
	}

	start, err = ParseTime(l[0])
	if err != nil {
		return start, end, fmt.Errorf("cannot parse %q: not a valid time", l[0])
	}
	end, err = ParseTime(l[1])
	if err != nil {
		return start, end, fmt.Errorf("cannot parse %q: not a valid time", l[1])
	}
	if end.Less(start) {
		return start, end, fmt.Errorf("cannot parse %q: not a valid interval", s)
	}

	return start, end, nil
}

// parseSingleSchedule parses a schedule string like "mon@9:00-11:00" or
// "9:00-11:00" and returns a Schedule struct and an error.
func parseSingleSchedule(s string) (*Schedule, error) {
	weekday, rest, err := parseWeekday(s)
	if err != nil {
		return nil, err
	}
	start, end, err := parseTimeInterval(rest)
	if err != nil {
		return nil, err
	}

	return &Schedule{
		Weekday: weekday,
		Start:   start,
		End:     end,
	}, nil
}

// ParseSchedule takes a schedule string in the form of:
//
// 9:00-15:00 (every day between 9am and 3pm)
// 9:00-15:00/21:00-22:00 (every day between 9am,5pm and 9pm,10pm)
// thu@9:00-15:00 (only Thursday between 9am and 3pm)
// fri@9:00-11:00/mon@13:00-15:00 (only Friday between 9am and 3pm and Monday between 1pm and 3pm)
// fri@9:00-11:00/13:00-15:00  (only Friday between 9am and 3pm and every day between 1pm and 3pm)
//
// and returns a list of Schdule types or an error
func ParseSchedule(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, "/") {
		sched, err := parseSingleSchedule(s)
		if err != nil {
			return nil, err
		}
		schedule = append(schedule, sched)
	}

	return schedule, nil
}
