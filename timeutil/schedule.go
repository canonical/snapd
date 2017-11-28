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
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Match 0:00-24:59, so that we cover 0:00-24:00, where 24:00 means the later
// end of the day. 24:<mm> where mm > 0 must be handled separately.
var validTime = regexp.MustCompile(`^([0-9]|0[0-9]|1[0-9]|2[0-3]):([0-5][0-9])$`)

// TimeOfDay represents a hour:minute time within a day.
type TimeOfDay struct {
	Hour   int
	Minute int
}

func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

// Sub subtracts `other` TimeOfDay from current and returns duration
func (t TimeOfDay) Sub(other TimeOfDay) time.Duration {
	t1 := time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute
	t2 := time.Duration(other.Hour)*time.Hour + time.Duration(other.Minute)*time.Minute
	return t1 - t2
}

// Add adds given duration and returns a new TimeOfDay
func (t TimeOfDay) Add(dur time.Duration) TimeOfDay {
	t1 := time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute
	t2 := t1 + dur
	nt := TimeOfDay{
		Hour:   int(t2.Hours()) % 24,
		Minute: int(t2.Minutes()) % 60,
	}
	return nt
}

// MakeTime generates a time.Time using base for information on year, month, day
// and with hour and minute set from TimeOfDay
func (t TimeOfDay) MakeTime(base time.Time) time.Time {
	return time.Date(base.Year(), base.Month(), base.Day(),
		t.Hour, t.Minute, 0, 0, time.Local)
}

// IsZero reports whether t represents a zero time instant
func (t TimeOfDay) IsZero() bool {
	return t == TimeOfDay{}
}

// isValidTime returns true if given s looks like a valid time specification
func isValidTime(s string) bool {
	return len(validTime.FindStringSubmatch(s)) > 0 || s == "24:00"
}

// IsValidWeekday returns true if given s looks like a valid weekday. Valid
// inputs are 3 letter, lowercase abbreviations of week days.
func IsValidWeekday(s string) bool {
	_, ok := weekdayMap[s]
	return ok
}

// ParseTime parses a string that contains hour:minute and returns
// an TimeOfDay type or an error
func ParseTime(s string) (t TimeOfDay, err error) {
	if s == "24:00" {
		t.Hour = 24
		return t, nil
	}

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
	return t, nil
}

// Weekday represents a weekday such as Monday, Tuesday, with optional
// week-in-the-month position, eg. the first Monday of the month
type Weekday struct {
	// Day stands for a weekday - "mon", "tue", ..
	Day string
	// Week the day appears in given month, 5 means the last week
	Pos uint
}

// IsZero indicates if Weekday is in the default state
func (w Weekday) IsZero() bool {
	return w == Weekday{}
}

func (w Weekday) String() string {
	if w.Pos == 0 {
		return w.Day
	}
	return w.Day + strconv.FormatUint(uint64(w.Pos), 10)
}

// WeekSpan represents a span of weekdays between Start and End days. WeekSpan
// may wrap around the week, eg. fri-mon is a span from Friday to Monday
type WeekSpan struct {
	Start Weekday
	End   Weekday
}

func (ws WeekSpan) String() string {
	if !ws.End.IsZero() && ws.End != ws.Start {
		return ws.Start.String() + "-" + ws.End.String()
	}
	return ws.Start.String()
}

// IsWithinRange checks if t is within the day-span represented by ws.
func (ws WeekSpan) IsWithinRange(t time.Time) bool {
	// every day matches with empty week span
	if ws.Start.IsZero() {
		return true
	}

	start, end := ws.Start, ws.End
	if end.IsZero() {
		end = start
	}
	wdStart, wdEnd := weekdayMap[start.Day], weekdayMap[end.Day]

	// is it the right week?
	if start.Pos > 0 {
		week := uint(t.Day()/7) + 1
		switch {
		case start.Pos == 5:
			if !isLastWeekdayInMonth(t) {
				return false
			}
		case week < start.Pos || week > end.Pos:
			return false
		}
	}

	// is it the right day?
	switch {
	case wdStart == wdEnd && t.Weekday() != wdStart:
		// a single day
		return false
	case wdEnd > wdStart && (t.Weekday() < wdStart || t.Weekday() > wdEnd):
		// day span, eg. mon-fri
		return false
	case wdEnd < wdStart && t.Weekday() < wdStart && t.Weekday()+7 > wdEnd+7:
		// day span that wraps around, eg. fri-mon,
		// since time.Weekday values go from 0-6, add 7
		// (week) at the end to get a continuous range
		return false
	}
	return true
}

// TimeSpan is a time span within a single day (or this and the next day).
// TimeSpan may wrap around, eg. 23:00-1:00 representing a span from 11pm to 1am
// the next day.
type TimeSpan struct {
	Start TimeOfDay
	End   TimeOfDay
	// indicates if the span is split into smaller subspans. Use Subspan()
	// to generate a slice of TimeSpan using the current TimeSpan as a base
	Split uint
	// indicates if the events are randomly spread between Start and End
	Spread bool
}

func (ts TimeSpan) String() string {
	sep := "-"
	if ts.Spread {
		sep = "~"
	}
	if !ts.End.IsZero() && ts.End != ts.Start {
		s := ts.Start.String() + sep + ts.End.String()
		if ts.Split > 0 {
			s += "/" + strconv.FormatUint(uint64(ts.Split), 10)
		}
		return s
	}
	return ts.Start.String()
}

// MakeTime generates a start and end times from ts using t as a base. Returned
// end time is automatically shifted to the next day if End is before Start
func (ts TimeSpan) MakeTime(t time.Time) (start time.Time, end time.Time) {
	a := ts.Start.MakeTime(t)
	b := a
	if !ts.End.IsZero() && ts.End != ts.Start {
		b = ts.End.MakeTime(t)

		// 23:00-1:00
		if b.Before(a) {
			b = b.Add(24 * time.Hour)
		}
	}
	return a, b
}

// Subspans returns a slice of TimeSpan generated from ts by splitting the time
// between ts.Start and ts.End into ts.Split equal spans.
func (ts TimeSpan) Subspans() []TimeSpan {
	if ts.Split == 0 || ts.Split == 1 || ts.End == ts.Start {
		return []TimeSpan{ts}
	}

	span := ts.End.Sub(ts.Start)
	step := span / time.Duration(ts.Split)

	var spans []TimeSpan
	for i := uint(0); i < ts.Split; i++ {
		start := ts.Start.Add(time.Duration(i) * step)
		spans = append(spans, TimeSpan{
			Start:  start,
			End:    start.Add(step),
			Split:  0, // no more subspans
			Spread: ts.Spread,
		})
	}
	return spans
}

// Schedule represents a single schedule
type Schedule struct {
	Week  []WeekSpan
	Times []TimeSpan
}

func (sched *Schedule) String() string {
	buf := &bytes.Buffer{}

	for i, span := range sched.Week {
		if i > 0 {
			fmt.Fprint(buf, ",")
		}
		fmt.Fprintf(buf, "%s", span)
	}

	if len(sched.Week) > 0 && len(sched.Times) > 0 {
		fmt.Fprint(buf, ",")
	}

	for i, span := range sched.Times {
		if i > 0 {
			fmt.Fprintf(buf, ",")
		}
		fmt.Fprintf(buf, "%s", span)
	}
	return buf.String()
}

// isLastWeekdayInMonth returns true if t.Weekday() is the last weekday
// occurring this t.Month(), eg. check is Feb 25 2017 is the last Saturday of
// February.
func isLastWeekdayInMonth(t time.Time) bool {
	// try a week from now, if it's still the same month then t.Weekday() is
	// not last
	return t.Month() != t.Add(7*24*time.Hour).Month()
}

type NextSchedule struct {
	Start  time.Time
	End    time.Time
	Spread bool
}

// Next returns when the time of the next interval defined in sched.
func (sched *Schedule) Next(last time.Time) NextSchedule {
	now := timeNow()

	weeks := sched.Week
	if len(weeks) == 0 {
		weeks = []WeekSpan{{}}
	}

	times := sched.Times
	if len(times) == 0 {
		times = []TimeSpan{{}}
	}

	t := last
	tnext := t

	for {
		// try to find a matching schedule by moving in 24h jumps, check
		// if the event needs to happen on a specific day in a specific
		// week, next pick the earliest event time

		var a, b time.Time

		t = tnext

		tnext = t.Add(24 * time.Hour)

		// if there's a week schedule, check if we hit that first
		var weekMatch bool
		for _, week := range weeks {
			if week.IsWithinRange(t) {
				weekMatch = true
				break
			}
		}

		if !weekMatch {
			continue
		}

		var spread bool
		for i := range times {
			for _, ts := range times[i].Subspans() {
				newA, newB := ts.MakeTime(t)

				// same interval as last update, move forward
				if (last.Equal(newA) || last.After(newA)) && (last.Equal(newB) || last.Before(newB)) {
					continue
				}

				// if candidate comes before current use it
				if a.IsZero() || newA.Before(a) {
					a = newA
					b = newB
					spread = ts.Spread
				}
			}
		}

		// if the end is before now
		if b.Before(now) {
			continue
		}
		return NextSchedule{
			Start:  a,
			End:    b,
			Spread: spread,
		}
	}

}

func randDur(a, b time.Time) time.Duration {
	dur := b.Sub(a)
	if dur > 5*time.Minute {
		// doing it this way we still spread really small windows about
		dur -= 5 * time.Minute
	}

	if dur <= 0 {
		// avoid panic'ing (even if things are probably messed up)
		return 0
	}

	return time.Duration(rand.Int63n(int64(dur)))
}

var (
	timeNow = time.Now

	// FIMXE: pass in as a parameter for next
	maxDuration = 31 * 24 * time.Hour
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Next will return the duration until an optionally random time in the next
// schedule window.
func Next(schedule []*Schedule, last time.Time) time.Duration {
	now := timeNow()

	a := last.Add(maxDuration)
	b := a.Add(1 * time.Hour)

	randomize := false
	for _, sched := range schedule {
		next := sched.Next(last)
		if next.Start.Before(a) {
			// randomize = sched.Randomize
			a = next.Start
			b = next.End
			randomize = next.Spread
		}
	}
	if a.Before(now) {
		return 0
	}

	when := a.Sub(now)
	if randomize {
		when += randDur(a, b)
	}

	return when

}

var weekdayMap = map[string]time.Weekday{
	"sun": time.Sunday,
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
}

// parseTimeInterval gets an input like "9:00-11:00"
// and extracts the start and end of that schedule string and
// returns them and any errors.
func parseTimeInterval(s string) (start, end TimeOfDay, err error) {
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

	return start, end, nil
}

// parseSingleSchedule parses a schedule string like "9:00-11:00"
func parseSingleSchedule(s string) (*Schedule, error) {
	start, end, err := parseTimeInterval(s)
	if err != nil {
		return nil, err
	}

	return &Schedule{
		Times: []TimeSpan{
			{
				Start:  start,
				End:    end,
				Spread: true,
			},
		},
	}, nil
}

// ParseLegacySchedule takes a schedule string in the form of:
//
// 9:00-15:00 (every day between 9am and 3pm)
// 9:00-15:00/21:00-22:00 (every day between 9am,5pm and 9pm,10pm)
//
// and returns a list of Schedule types or an error
func ParseLegacySchedule(scheduleSpec string) ([]*Schedule, error) {
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

// ParseSchedule parses a schedule in V2 format. The format is described as:
//
//     eventlist = eventset *( ".." eventset )
//     eventset = wdaylist / timelist / wdaylist "," timelist
//
//     wdaylist = wdayset *( "," wdayset )
//     wdayset = wday / wdayspan
//     wday =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" ) [ DIGIT ]
//     wdayspan = wday "-" wday
//
//     timelist = timeset *( "," timeset )
//     timeset = time / timespan
//     time = 2DIGIT ":" 2DIGIT
//     timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
//     count = 1*DIGIT
//
// Examples:
// mon,10:00..fri,15:00 (Monday at 10:00, Friday at 15:00)
// mon,fri,10:00,15:00 (Monday at 10:00 and 15:00, Friday at 10:00 and 15:00)
// mon-wed,fri,9:00-11:00/2 (Monday to Wednesday and on Friday, twice between
//                           9:00 and 11:00)
// mon,9:00~11:00..wed,22:00~23:00 (Monday, sometime between 9:00 and 11:00, and
//                                  on Wednesday, sometime between 22:00 and 23:00)
// mon,wed  (Monday and on Wednesday)
// mon..wed (same as above)
//
// Returns a slice of schedules or an error if parsing failed
func ParseSchedule(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, "..") {
		// cut the schedule in event sets
		//     eventlist = eventset *( ".." eventset )
		sched, err := parseEventSet(s)
		if err != nil {
			return nil, err
		}
		schedule = append(schedule, sched)
	}
	return schedule, nil
}

// parseWeekSpan generates a per day (span) schedule
func parseWeekSpan(start string, end string) (*WeekSpan, error) {
	startWeekday, err := parseWeekday(start)
	if err != nil {
		return nil, err
	}

	endWeekday, err := parseWeekday(end)
	if err != nil {
		return nil, err
	}

	if endWeekday.Pos < startWeekday.Pos {
		return nil, fmt.Errorf("unsupported schedule")
	}

	if (startWeekday.Pos != 0 && endWeekday.Pos == 0) ||
		(endWeekday.Pos != 0 && startWeekday.Pos == 0) {
		return nil, fmt.Errorf("mixed weekday and nonweekday")
	}

	span := &WeekSpan{
		Start: *startWeekday,
	}
	if *endWeekday != *startWeekday {
		span.End = *endWeekday
	}

	return span, nil
}

// parseSpan splits a span string `<start>-<end>` and returns the start and end
// pieces. If string is not a span then the whole string is returned
func parseSpan(spec string) (string, string) {
	var start, end string

	specs := strings.Split(spec, spanToken)

	if len(specs) == 1 {
		start = specs[0]
		end = start
	} else {
		start = specs[0]
		end = specs[1]
	}
	return start, end
}

// parseTime parses a time specification which can either be `<hh>:<mm>` or
// `<hh>:<mm>-<hh>:<mm>`. Returns corresponding TimeOfDay structs.
func parseTime(start, end string) (TimeOfDay, TimeOfDay, error) {
	// is it a time?
	var err error
	var tstart, tend TimeOfDay

	if start == end {
		// single time, eg. 10:00
		tstart, err = ParseTime(start)
		if err == nil {
			tend = tstart
		}
	} else {
		// time span, eg. 10:00-12:00
		tstart, tend, err = parseTimeInterval(fmt.Sprintf("%s-%s", start, end))
	}

	return tstart, tend, err
}

func parseTimeSpan(start, end string) (*TimeSpan, error) {

	startTime, endTime, err := parseTime(start, end)
	if err != nil {
		return nil, err
	}

	span := &TimeSpan{
		Start: startTime,
	}
	if endTime != startTime {
		span.End = endTime
	}
	return span, nil
}

// parseWeekday will parse a string and extract weekday (eg. wed),
// MonthWeekday (eg. 1st, 5th in the month).
func parseWeekday(s string) (*Weekday, error) {
	l := len(s)
	if l != 3 && l != 4 {
		return nil, fmt.Errorf("cannot parse %q: invalid format", s)
	}

	day := s
	var pos uint
	if l == 4 {
		day = s[0:3]
		if v, err := strconv.ParseUint(s[3:], 10, 32); err != nil {
			return nil, fmt.Errorf("cannot parse %q: invalid week", s)
		} else if v < 1 || v > 5 {
			return nil, fmt.Errorf("cannot parse %q: incorrect week number", s)
		} else {
			pos = uint(v)
		}
	}

	if !IsValidWeekday(day) {
		return nil, fmt.Errorf("cannot parse %q: invalid weekday", s)
	}

	week := &Weekday{
		Day: day,
		Pos: pos,
	}
	return week, nil
}

// parseCount will parse the string containing a count token and return the
// count, the rest of the string with count information removed or an error
func parseCount(s string) (count uint, rest string, err error) {
	if strings.Contains(s, countToken) {
		//     timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
		ws := strings.Split(s, countToken)
		rest = ws[0]
		countStr := ws[1]
		c, err := strconv.ParseUint(countStr, 10, 32)
		if err != nil || c == 0 {
			return 0, "", fmt.Errorf("cannot parse %q: not a valid event interval",
				s)
		}
		return uint(c), rest, nil
	}
	return 0, s, nil
}

// isValidWeekdaySpec returns true if string is of the following format:
//     wday =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" ) [ DIGIT ]
func isValidWeekdaySpec(s string) bool {
	_, err := parseWeekday(s)
	return err == nil
}

const (
	spanToken           = "-"
	randomizedSpanToken = "~"
	countToken          = "/"
)

var specialTokens = map[string]string{
	// shorthand variant of whole day
	"-": "0:00-24:00",
	// and randomized whole day
	"~": "0:00~24:00",
}

// Parse each event and return a slice of schedules.
func parseEventSet(s string) (*Schedule, error) {
	var events []string
	// split event set into events
	//     eventset = wdaylist / timelist / wdaylist "," timelist
	// or wdaysets
	//     wdaylist = wdayset *( "," wdayset )
	// or timesets
	//     timelist = timeset *( "," timeset )
	//
	// NOTE: the syntax is ambiguous in the sense the type of a 'set' is now
	// explicitly indicated, thus the parsing will first try to handle it as
	// a wdayset, then as timeset

	if els := strings.Split(s, ","); len(els) > 1 {
		events = els
	} else {
		events = []string{s}
	}

	var schedule Schedule
	// indicates that any further events must be time events
	var expectTime bool

	for _, event := range events {
		// process events one by one

		randomized := false
		var count uint
		var start string
		var end string

		if c, rest, err := parseCount(event); err != nil {
			return nil, err
		} else if c > 0 {
			// count was specified
			count = c
			// count token is only allowed in timespans, meaning
			// we're parsing only timespans from now on
			expectTime = true

			// update the remaining part of the event
			event = rest
		}

		if special, ok := specialTokens[event]; ok {
			event = special
		}

		if strings.Contains(event, randomizedSpanToken) {
			// timespan uses "~" to indicate that the actual event
			// time is to be randomized.
			randomized = true
			event = strings.Replace(event,
				randomizedSpanToken,
				spanToken, 1)

			// randomize token is only allowed in timespans, meaning
			// we're parsing only timespans from now on
			expectTime = true
		}

		// spans
		//     wdayspan = wday "-" wday
		//     timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
		start, end = parseSpan(event)

		if isValidTime(start) && isValidTime(end) {
			// from now on we expect only timespans
			expectTime = true

			if span, err := parseTimeSpan(start, end); err != nil {
				return nil, err
			} else {
				span.Split = count
				span.Spread = randomized

				schedule.Times = append(schedule.Times, *span)
			}

		} else if isValidWeekdaySpec(start) && isValidWeekdaySpec(end) {
			// is it a day?

			if expectTime {
				return nil, fmt.Errorf("cannot parse %q: expected time spec", s)
			}

			if span, err := parseWeekSpan(start, end); err != nil {
				return nil, fmt.Errorf("cannot parse %q: %s", event, err.Error())
			} else {
				schedule.Week = append(schedule.Week, *span)
			}
		} else {
			// no, it's an error
			return nil, fmt.Errorf("cannot parse %q: not a valid event", s)
		}

	}

	return &schedule, nil
}
