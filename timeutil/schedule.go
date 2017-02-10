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
	if sched.Weekday != "" {
		wd := time.Weekday(weekdayMap[sched.Weekday])
		if t.Weekday() != wd {
			return false
		}
	}

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

var weekdayMap = map[string]int{
	"sun": 0, "sunday": 0,
	"mon": 1, "monday": 1,
	"tue": 2, "tuesday": 2,
	"wed": 3, "wednesday": 3,
	"thu": 4, "thursday": 4,
	"fri": 5, "friday": 5,
	"sat": 6, "saturday": 6,
}

func parseWeekday(s string, sched *Schedule) (rest string, err error) {
	if !strings.Contains(s, "@") {
		return s, nil
	}

	l := strings.SplitN(s, "@", 2)
	weekday := strings.ToLower(l[0])
	_, ok := weekdayMap[weekday]
	if !ok {
		return "", fmt.Errorf("cannot parse %q: not a valid day", l[0])
	}
	sched.Weekday = weekday
	rest = l[1]

	return rest, nil
}

func parseTimeInterval(s string, sched *Schedule) error {
	l := strings.SplitN(s, "-", 2)
	if len(l) != 2 {
		return fmt.Errorf("cannot parse %q: not a valid interval", s)
	}

	var err error
	sched.Start, err = ParseTime(l[0])
	if err != nil {
		return fmt.Errorf("cannot parse %q: not a valid time", l[0])
	}
	sched.End, err = ParseTime(l[1])
	if err != nil {
		return fmt.Errorf("cannot parse %q: not a valid time", l[1])
	}

	return nil
}

func parseSingleSchedule(s string) (*Schedule, error) {
	var cur Schedule

	rest, err := parseWeekday(s, &cur)
	if err != nil {
		return nil, err
	}
	if err := parseTimeInterval(rest, &cur); err != nil {
		return nil, err
	}

	return &cur, nil
}

// ParseSchedule takes a schedule string in the form of:
//
// 9:00-15:00 (every day between 9am and 3pm)
// 9:00-15:00,21:00-22:00 (every day between 9am,5pm and 9pm,10pm)
// thu@9:00-15:00 (only Thursday between 9am and 3pm)
// fri@9:00-11:00,mon@13:00-15:00 (only Friday between 9am and 3pm and Monday between 1pm and 3pm)
// fri@9:00-11:00,13:00-15:00  (only Friday between 9am and 3pm and every day between 1pm and 3pm)
//
// and returns a list of Schdule types or an error
func ParseSchedule(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, ",") {
		sched, err := parseSingleSchedule(s)
		if err != nil {
			return nil, err
		}
		schedule = append(schedule, sched)
	}

	return schedule, nil
}
