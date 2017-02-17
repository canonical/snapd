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
	"math/rand"
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

func (sched *Schedule) Next(last time.Time) (start, end time.Time) {
	now := timeNow()
	wd := time.Weekday(weekdayMap[sched.Weekday])

	t := last
	for {
		a := time.Date(t.Year(), t.Month(), t.Day(), sched.Start.Hour, sched.Start.Minute, sched.Start.Second, 0, time.Local)
		b := time.Date(t.Year(), t.Month(), t.Day(), sched.End.Hour, sched.End.Minute, sched.End.Second, 0, time.Local)

		// not using AddDate() here as this can panic() if no
		// location is set
		t = t.Add(24 * time.Hour)

		// we have not hit the right day yet
		if sched.Weekday != "" && a.Weekday() != wd {
			continue
		}
		// same inteval as last update, move forward
		if last.After(a) && last.Before(b) {
			continue
		}
		if b.Before(now) {
			continue
		}

		return a, b
	}
}

func randDur(dur time.Duration) time.Duration {
	return time.Duration(rand.Int63n(int64(dur)))
}

var (
	timeNow     = time.Now
	maxDuration = 14 * 24 * time.Hour
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Next will return the duration until a random time in the next
// schedule window.
func Next(schedule []*Schedule, last time.Time) time.Duration {
	now := timeNow()

	a := last.Add(maxDuration)
	b := a.Add(1 * time.Hour)
	for _, sched := range schedule {
		start, end := sched.Next(last)
		if start.Before(a) {
			a = start
			b = end
		}
	}
	if a.Before(now) {
		return 0
	}

	when := a.Sub(now) + randDur(b.Add(-5*time.Minute).Sub(a))

	return when
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
	if !(start.Hour <= end.Hour && start.Minute <= end.Minute) {
		return start, end, fmt.Errorf("cannot parse %q: time in an interval cannot go backwards", s)
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
