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
var validTime = regexp.MustCompile(`^([0-9]|0[0-9]|1[0-9]|2[0-4]):([0-5][0-9])$`)

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

// isValidTime returns true if given s looks like a valid time specification
func isValidTime(s string) bool {
	m := validTime.FindStringSubmatch(s)
	if len(m) == 0 {
		return false
	}
	return true
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
	if t.Hour == 24 {
		if t.Minute > 0 {
			return t, fmt.Errorf("cannot parse %q: not a valid time", s)
		}
	}
	return t, nil
}

// MonthWeekday is a helper type to represent a Weekday that occurs on a
// specific week in a month, eg. wed4 -> Wednesday in the 4th week of the month
type MonthWeekday int

const (
	// sane default
	monthWeekdayNone MonthWeekday = 0
	monthWeekdayMin  MonthWeekday = 1
	// not possible to have given day appear more than 5 times a month
	monthWeekdayMax MonthWeekday = 5
	// indicates the last weekday of a month, eg. the last Monday
	monthWeekdayLast = monthWeekdayMax
)

func (m MonthWeekday) String() string {
	if m == monthWeekdayNone {
		return ""
	}
	return strconv.Itoa(int(m))
}

// Schedule defines a start and end time and an optional weekday in which
// events should run.
type Schedule struct {
	Start TimeOfDay
	End   TimeOfDay

	// weekday the schedule starts on
	Weekday string
	// weekday the schedule ends on, can be empty if the same as Weekday
	WeekdayEnd string

	// specific weekday of the month, eg. the first Monday, the 2nd Tuesday, the
	// last Monday
	MonthWeekday MonthWeekday
	// indicates when the weekday-of-the-month schedule ends, can be set to
	// monthWeekdayNone if the same as MonthWeekday
	MonthWeekdayEnd MonthWeekday

	// if true then even time will be randomized
	Randomize bool
}

func (sched *Schedule) String() string {
	sep := "-"
	if sched.Randomize {
		sep = "~"
	}

	buf := &bytes.Buffer{}

	if sched.Weekday != "" {
		fmt.Fprintf(buf, "%s%s", sched.Weekday, sched.MonthWeekday)
	}
	if sched.WeekdayEnd != "" {
		mwe := sched.MonthWeekdayEnd
		if mwe == monthWeekdayNone && sched.MonthWeekday != monthWeekdayNone {
			mwe = sched.MonthWeekdayEnd
		}
		fmt.Fprintf(buf, "-%s%s", sched.WeekdayEnd, mwe)
	}

	if sched.Weekday != "" {
		fmt.Fprint(buf, ",")
	}
	fmt.Fprintf(buf, "%s", sched.Start)
	if sched.End != sched.Start {
		fmt.Fprint(buf, sep)
		fmt.Fprintf(buf, "%s", sched.End)
	}
	return buf.String()
}

// isLastWeekdayInMonth returns true it a.Weekday() is the last weekday
// occurring this a.Month(), eg. check is Feb 25 2017 is the last Saturday of
// February.
func isLastWeekdayInMonth(a time.Time) bool {
	month := a.Month()
	isLast := false
	t := a

	// try a week from now
	t = t.Add(7 * 24 * time.Hour)

	if t.Month() != month {
		// if hit a different month then current weekday is the last in
		// this month
		isLast = true
	}

	return isLast
}

func (sched *Schedule) Next(last time.Time) (start, end time.Time) {
	now := timeNow()
	wdStart := time.Weekday(weekdayMap[sched.Weekday])
	wdEnd := wdStart
	if sched.WeekdayEnd != "" {
		wdEnd = time.Weekday(weekdayMap[sched.WeekdayEnd])
	}

	t := last
	for {
		a := time.Date(t.Year(), t.Month(), t.Day(), sched.Start.Hour, sched.Start.Minute, 0, 0, time.Local)
		b := time.Date(t.Year(), t.Month(), t.Day(), sched.End.Hour, sched.End.Minute, 0, 0, time.Local)
		if b.Before(a) {
			// eg. 23:00-1:00, end time is on the next day
			b = b.Add(24 * time.Hour)
		}

		// not using AddDate() here as this can panic() if no
		// location is set
		t = t.Add(24 * time.Hour)

		if sched.Weekday != "" {
			// looking for a specific weekday

			mwe := sched.MonthWeekday
			if sched.MonthWeekdayEnd != monthWeekdayNone {
				mwe = sched.MonthWeekdayEnd
			}

			if sched.MonthWeekday != monthWeekdayNone {
				// looking for a specific weekday in a month
				week := MonthWeekday((a.Day() / 7) + 1)
				switch {
				case sched.MonthWeekday == monthWeekdayLast:
					if !isLastWeekdayInMonth(a) {
						// looking for last weekday in month, this one isn't
						continue
					}
				case week < sched.MonthWeekday || week > mwe:
					// looking for specific week in a month, this one isn't
					continue
				}
			}

			// we have not hit the right day yet
			switch {
			case wdStart == wdEnd && a.Weekday() != wdStart:
				// a single day
				continue
			case wdEnd > wdStart && a.Weekday() < wdStart && a.Weekday() > wdEnd:
				// day span, eg. mon-fri
				continue
			case wdEnd < wdStart && a.Weekday() < wdStart && a.Weekday()+7 > wdEnd+7:
				// day span that wraps around, eg. fri-mon,
				// since time.Weekday values go from 0-6, add 7
				// (week) at the end to get a continuous range
				continue
			}
		}

		// same interval as last update, move forward
		if (last.Equal(a) || last.After(a)) && (last.Equal(b) || last.Before(b)) {
			continue
		}
		if b.Before(now) {
			continue
		}

		return a, b
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
		start, end := sched.Next(last)
		if start.Before(a) {
			randomize = sched.Randomize
			a = start
			b = end
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

var weekdayMap = map[string]int{
	"sun": 0,
	"mon": 1,
	"tue": 2,
	"wed": 3,
	"thu": 4,
	"fri": 5,
	"sat": 6,
}

var weekdayOrder = []string{
	"mon",
	"tue",
	"wed",
	"thu",
	"fri",
	"sat",
	"sun",
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

// parseSingleSchedule parses a schedule string like "9:00-11:00"and returns a
// Schedule struct and an error.
func parseSingleSchedule(s string) (*Schedule, error) {
	start, end, err := parseTimeInterval(s)
	if err != nil {
		return nil, err
	}

	return &Schedule{
		Start:     start,
		End:       end,
		Randomize: true,
	}, nil
}

// ParseSchedule takes a schedule string in the form of:
//
// 9:00-15:00 (every day between 9am and 3pm)
// 9:00-15:00/21:00-22:00 (every day between 9am,5pm and 9pm,10pm)
//
// and returns a list of Schedule types or an error
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

// ParseScheduleV2 parses a schedule in V2 format. The format is loosely
// described as:
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
// Special tokens:
//   weekend	-> sat-sun
//   day	-> 0:00-24:00
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
func ParseScheduleV2(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, "..") {
		// cut the schedule in event sets
		//     eventlist = eventset *( ".." eventset )
		sched, err := parseEventSetScheduleV2(s)
		if err != nil {
			return nil, err
		}
		schedule = append(schedule, sched...)
	}
	return schedule, nil
}

// makeWeekdayEvent generates a per day (span) schedule
func makeWeekdayEvent(start string, end string) (*Schedule, error) {
	startDay, startMonthWeekDay, _ := parseWeekdaySpec(start)
	endDay, endMonthWeekDay, _ := parseWeekdaySpec(end)

	if endMonthWeekDay < startMonthWeekDay {
		return nil, fmt.Errorf("unsupported schedule")
	}

	sched := &Schedule{
		Weekday: startDay,
		// WeekdayEnd:      endDay,
		MonthWeekday: startMonthWeekDay,
		// MonthWeekdayEnd: endMonthWeekDay,
	}
	if startDay != endDay {
		sched.WeekdayEnd = endDay
	}
	if endMonthWeekDay != startMonthWeekDay {
		sched.MonthWeekdayEnd = endMonthWeekDay
	}

	return sched, nil
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

// parseTimeV2 parses a time specification which can either be `<hh>:<mm>` or
// `<hh>:<mm>-<hh>:<mm>`. Returns corresponding TimeOfDay structs.
func parseTimeV2(start, end string) (TimeOfDay, TimeOfDay, error) {
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

// timeScheduleConf is a helper type for passing current timespec information
type timeScheduleConf struct {
	// event start time
	start TimeOfDay
	// event end time
	end TimeOfDay
	// how many events between start and end, 0 or 1 mean exactly one event
	count uint
	// should event time be randomized
	randomize bool
}

// makeTimeSchedule creates a single
func makeTimeSchedule(conf timeScheduleConf, base *Schedule) *Schedule {
	if base == nil {
		base = &Schedule{}
	}
	nsch := *base
	nsch.Start = conf.start
	nsch.End = conf.end
	nsch.Randomize = conf.randomize
	return &nsch
}

// parseWeekdaySpec will parse a string and extract weekday (eg. wed),
// MonthWeekday (eg. 1st, 5th in the month).
func parseWeekdaySpec(s string) (string, MonthWeekday, error) {
	l := len(s)
	if l != 3 && l != 4 {
		return "", monthWeekdayNone, fmt.Errorf("cannot parse %q: invalid format", s)
	}

	wday := s
	mw := monthWeekdayNone
	if l == 4 {
		wday = s[0:3]
		if v, err := strconv.ParseUint(s[3:], 10, 32); err != nil {
			return "", monthWeekdayNone, fmt.Errorf("cannot parse %q: invalid week", s)
		} else {
			mw = MonthWeekday(v)
		}
		if mw < monthWeekdayMin || mw > monthWeekdayMax {
			return "", monthWeekdayNone, fmt.Errorf("cannot parse %q: incorrect week number", mw)
		}
	}
	if !IsValidWeekday(wday) {
		return "", monthWeekdayNone, fmt.Errorf("cannot parse %q: invalid weekday", s)
	}

	return wday, mw, nil
}

// makeTimeSchedules returns a slice of Schedules generated from the template
// list. The schedules are filled with time information passed in `conf`. Note,
// that there may be more schedules generated than passed as a template.
func makeTimeSchedules(template []*Schedule, conf timeScheduleConf) []*Schedule {

	scheds := []*Schedule{}
	for _, tsch := range template {
		// Cut up the 'count' schedules (eg. 9:00-11:00/2 - twice
		// between 9 and 11) into separate schedules. This way we don't
		// have to keep any state data to be able to tell how many times
		// the event has already happened and each event becomes separate.
		if conf.count > 1 {
			step := time.Duration(uint64(conf.end.Sub(conf.start)) / uint64(conf.count))
			tempConf := conf
			tempConf.start = conf.start
			for i := uint(0); i < conf.count; i++ {
				tempConf.end = tempConf.start.Add(step)
				scheds = append(scheds,
					makeTimeSchedule(tempConf, tsch))
				tempConf.start = tempConf.end
			}
		} else {
			scheds = append(scheds,
				makeTimeSchedule(conf, tsch))
		}
	}
	return scheds
}

// isValidWeekdaySpec returns true if string is of the following format:
//     wday =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" ) [ DIGIT ]
func isValidWeekdaySpec(s string) bool {
	_, _, err := parseWeekdaySpec(s)
	return err == nil
}

const (
	spanToken           = "-"
	randomizedSpanToken = "~"
	countToken          = "/"
)

var specialTokens = map[string]string{
	"day":     "0:00-24:00",
	"weekend": "sat-sun",
	// shorthand variant of whole day
	"-": "0:00-24:00",
	// and randomized whole day
	"~": "0:00~24:00",
}

// Parse each event and return a slice of schedules.
func parseEventSetScheduleV2(s string) ([]*Schedule, error) {
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

	// temporary schedules
	var tempScheds []*Schedule
	// currently collected schedules
	var scheds []*Schedule
	// indicates that any further events must be time events
	var expectTime bool

	for _, event := range events {
		// process events one by one

		randomized := false
		var count uint = 1
		var start string
		var end string

		if strings.Contains(event, countToken) {
			//     timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
			ws := strings.Split(event, countToken)
			eventBase := ws[0]
			countStr := ws[1]
			if c, err := strconv.ParseUint(countStr, 10, 32); err != nil || c == 0 {
				return nil, fmt.Errorf("cannot parse %q: not a valid event interval",
					event)
			} else {
				count = uint(c)
				event = eventBase
			}

			// count token is only allowed in timespans, meaning
			// we're parsing only timespans from now on
			expectTime = true
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

			// is it a time?
			var err error
			var tstart, tend TimeOfDay

			if tstart, tend, err = parseTimeV2(start, end); err != nil {
				return nil, err
			}

			if len(tempScheds) == 0 {
				// make an every-day schedule if there was none
				// specified yet
				tempScheds = append(tempScheds,
					&Schedule{})
			}

			// make a schedule for each day that we currently have
			scheds = append(scheds,
				makeTimeSchedules(tempScheds, timeScheduleConf{
					start:     tstart,
					end:       tend,
					count:     count,
					randomize: randomized,
				})...)
		} else if isValidWeekdaySpec(start) && isValidWeekdaySpec(end) {
			// is it a day?

			if expectTime {
				return nil, fmt.Errorf("cannot parse %q: expected time spec", s)
			}
			weSchedule, err := makeWeekdayEvent(start, end)
			if err != nil {
				return nil, fmt.Errorf("cannot parse %q: %s", event, err.Error())
			}
			tempScheds = append(tempScheds, weSchedule)
		} else {
			// no, it's an error
			return nil, fmt.Errorf("cannot parse %q: not a valid event", event)
		}

	}

	if len(scheds) == 0 {
		scheds = tempScheds
	}
	return scheds, nil
}
