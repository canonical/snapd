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
	"strings"
)

type Schedule struct {
	Start   string
	End     string
	Weekday string
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

var validTime = regexp.MustCompile(`^([0-9]|0[0-9]|1[0-9]|2[0-3]):[0-5][0-9]$`)

func parseTimeInterval(s string, sched *Schedule) error {
	l := strings.SplitN(s, "-", 2)
	if len(l) != 2 {
		return fmt.Errorf("cannot parse %q: not a valid interval", s)
	}
	if !validTime.MatchString(l[0]) {
		return fmt.Errorf("cannot parse %q: not a valid time", l[0])
	}
	if !validTime.MatchString(l[1]) {
		return fmt.Errorf("cannot parse %q: not a valid time", l[1])
	}
	sched.Start = l[0]
	sched.End = l[1]

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
