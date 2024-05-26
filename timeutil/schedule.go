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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/randutil"
)

// Match 0:00-24:00, where 24:00 means the later end of the day.
var validTime = regexp.MustCompile(`^([0-9]|0[0-9]|1[0-9]|2[0-3]):([0-5][0-9])$|^24:00$`)

// Clock represents a hour:minute time within a day.
type Clock struct {
	Hour   int
	Minute int
}

func (t Clock) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

// Sub returns the duration t - other.
func (t Clock) Sub(other Clock) time.Duration {
	t1 := time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute
	t2 := time.Duration(other.Hour)*time.Hour + time.Duration(other.Minute)*time.Minute
	dur := t1 - t2
	if dur < 0 {
		dur = -(dur + 24*time.Hour)
	}
	return dur
}

// Add adds given duration to t and returns a new Clock
func (t Clock) Add(dur time.Duration) Clock {
	t1 := time.Duration(t.Hour)*time.Hour + time.Duration(t.Minute)*time.Minute
	t2 := t1 + dur
	nt := Clock{
		Hour:   int(t2.Hours()) % 24,
		Minute: int(t2.Minutes()) % 60,
	}
	return nt
}

// Time generates a time.Time with hour and minute set from t, while year, month
// and day are taken from base
func (t Clock) Time(base time.Time) time.Time {
	return time.Date(base.Year(), base.Month(), base.Day(),
		t.Hour, t.Minute, 0, 0, base.Location())
}

// ParseClock parses a string that contains hour:minute and returns
// a Clock type or an error
func ParseClock(s string) (t Clock, err error) {
	m := validTime.FindStringSubmatch(s)
	if len(m) == 0 {
		return t, fmt.Errorf("cannot parse %q", s)
	}

	if m[0] == "24:00" {
		t.Hour = 24
		return t, nil
	}

	t.Hour = mylog.Check2(strconv.Atoi(m[1]))

	t.Minute = mylog.Check2(strconv.Atoi(m[2]))

	return t, nil
}

const (
	EveryWeek uint = 0
	LastWeek  uint = 5
)

// Week represents a weekday such as Monday, Tuesday, with optional
// week-in-the-month position, eg. the first Monday of the month
type Week struct {
	Weekday time.Weekday
	// Pos defines which week inside the month the Day refers to, where zero
	// means every week, 1 means first occurrence of the weekday, and 5
	// means last occurrence (which might be the fourth or the fifth).
	Pos uint
}

func (w Week) String() string {
	// Wednesday -> wed
	day := strings.ToLower(w.Weekday.String()[0:3])
	if w.Pos == EveryWeek {
		return day
	}
	return day + strconv.Itoa(int(w.Pos))
}

// WeekSpan represents a span of weekdays between Start and End days, which may
// be a single day. WeekSpan may wrap around the week, eg. fri-mon is a span
// from Friday to Monday, mon1-fri is a span from the first Monday to the
// following Friday, while mon1 (internally, an equal start and end range)
// represents the 1st Monday of a month.
type WeekSpan struct {
	Start Week
	End   Week
}

func (ws WeekSpan) String() string {
	if ws.End != ws.Start {
		return ws.Start.String() + "-" + ws.End.String()
	}
	return ws.Start.String()
}

// findNthWeekDay finds the nth occurrence of a given weekday in the month of t
func findNthWeekDay(t time.Time, weekday time.Weekday, nthInMonth uint) time.Time {
	// move to the beginning of the month
	t = t.AddDate(0, 0, -t.Day()+1)

	var nth uint
	for {
		if t.Weekday() == weekday {
			nth++
			if nth == nthInMonth {
				break
			}
		}
		t = t.Add(24 * time.Hour)
	}
	return t
}

// findLastWeekDay finds the last occurrence of a given weekday in the month of t
func findLastWeekDay(t time.Time, weekday time.Weekday) time.Time {
	n := monthNext(t).Add(-24 * time.Hour)
	for n.Weekday() != weekday {
		n = n.Add(-24 * time.Hour)
	}
	return n
}

// matchingWeekdaysInMonth returns the number of occurrences of the weekday of t since
// the start of the month until t event
func matchingWeekdaysInMonth(t time.Time) int {
	month := t.Month()
	nth := 0
	for n := t; n.Month() == month; n = n.Add(-7 * 24 * time.Hour) {
		nth++
	}
	return nth
}

// Match checks if t is within the day span represented by ws.
func (ws WeekSpan) Match(t time.Time) bool {
	start, end := ws.Start, ws.End
	wdStart, wdEnd := start.Weekday, end.Weekday

	weekdayMatch := func(t time.Time) bool {
		if wdStart <= wdEnd {
			// single day (mon) or start < end (eg. mon-fri)
			return t.Weekday() >= wdStart && t.Weekday() <= wdEnd
		}
		// wraps around the week end, eg. fri-mon
		return t.Weekday() >= wdStart || t.Weekday() <= wdEnd
	}

	if start.Pos == EveryWeek && end.Pos == EveryWeek {
		// generic weekday match, eg. mon-fri
		return weekdayMatch(t)
	}

	// things that use a numbered weekday

	// fun cases, eg (consider the calendar below):
	//
	// - mon1-fri, week span, start anchored at 1st Monday 06.08, matches:
	// 06.08-10.08
	// - mon-fri2, week span, end anchored at 2nd Friday 10.08, matches:
	// 06.08-10.08
	// - fri1-mon, week span, start anchored at 1st Friday 3.08, matches
	// 03.08-06.08
	// - mon-fri1, week span, end anchored at 1st Friday 3.08, matches
	// 30.07-03.08, (crossing the month boundary)
	// - fri4-thu, week span, end anchored at 4th Friday 27.07, matches
	// 27.07-02.08, (crossing the month boundary), but also 24.08-30.08,
	// which is within a single month
	//
	//     July 2018            August 2018
	// Su Mo Tu We Th Fr Sa  Su Mo Tu We Th Fr Sa
	//  1  2  3  4  5  6  7            1  2  3  4
	//  8  9 10 11 12 13 14   5  6  7  8  9 10 11
	// 15 16 17 18 19 20 21  12 13 14 15 16 17 18
	// 22 23 24 25 26 27 28  19 20 21 22 23 24 25
	// 29 30 31              26 27 28 29 30 31

	// find out the range of week span, anchor sharing the same month as t
	startDay, endDay := ws.dateRangeAnchoredAt(t)
	anchoredAtStart := ws.AnchoredAtStart()

	if t.After(endDay) || t.Before(startDay) {
		// outside of dates range of the week span, consider edge cases:
		// - next month if the span is anchored at the end (eg. mon-fri1 30.07-03.08, t=31.07)
		// - previous month if the span is anchored at the start (eg. fri4-thu 27.07-02.08, t=01.08)

		if anchoredAtStart {
			// eg. fri4-thu, range anchored at previous month
			if matchingWeekdaysInMonth(t) != 1 {
				// no match if t is not within the first week
				return false
			}
			prevMonth := monthPrev(t)
			startDay, endDay = ws.dateRangeAnchoredAt(prevMonth)
		} else {
			// eg. mon-fri1, range anchored at the next month
			if !isLastWeekdayInMonth(t) {
				// no match if t is not within the last week
				return false
			}
			nextMonth := monthNext(t)
			startDay, endDay = ws.dateRangeAnchoredAt(nextMonth)
		}
		// at this point we will check whether t matches the range that
		// spills from the previous month or from the next month
	}
	outside := t.Before(startDay) || t.After(endDay)
	return !outside
}

// monthNext returns the first day of the next month relative to t
func monthNext(t time.Time) time.Time {
	n := t
	// advance by 28 days at most, so that we don't skip a 28 day February
	n = n.AddDate(0, 0, 28)
	for n.Month() == t.Month() {
		n = n.Add(24 * time.Hour)
	}
	if n.Day() != 1 {
		// backtrack if we didn't land on the first day yet
		n = n.AddDate(0, 0, -n.Day()+1)
	}
	return n
}

// monthPrev returns the last day of previous month relative to t
func monthPrev(t time.Time) time.Time {
	return t.AddDate(0, 0, -1*(t.Day()+1))
}

// AnchoredAtStart returns true when the week span is anchored at the starting
// point, or false otherwise
func (ws WeekSpan) AnchoredAtStart() bool {
	return ws.Start.Pos != EveryWeek
}

// dateRangeAnchoredAt returns the range of dates that match the week span, with the
// anchor sharing the same month as t
func (ws WeekSpan) dateRangeAnchoredAt(t time.Time) (start, end time.Time) {
	weekPos := ws.End.Pos
	anchoredAtStart := ws.AnchoredAtStart()
	if anchoredAtStart {
		weekPos = ws.Start.Pos
	}
	// first check the start/end dates in the same month as t
	if weekPos != LastWeek {
		start = findNthWeekDay(t, ws.Start.Weekday, weekPos)
		end = findNthWeekDay(t, ws.End.Weekday, weekPos)
	} else {
		start = findLastWeekDay(t, ws.Start.Weekday)
		end = findLastWeekDay(t, ws.End.Weekday)
	}

	// eg. mon1-mon span falls under the Equal && !singleDay case
	if start.After(end) || (start.Equal(end) && !ws.IsSingleDay()) {
		if anchoredAtStart {
			end = end.Add(7 * 24 * time.Hour)
		} else {
			start = start.Add(-7 * 24 * time.Hour)
		}
	}
	return start, end
}

// IsSingleDay returns true when the week span represents a single day
func (ws WeekSpan) IsSingleDay() bool {
	return ws.Start == ws.End
}

// ClockSpan represents a time span within 24h, potentially crossing days. For
// example, 23:00-1:00 represents a span from 11pm to 1am.
type ClockSpan struct {
	Start Clock
	End   Clock
	// Split defines the number of subspans this span will be divided into.
	Split uint
	// Spread defines whether the events are randomly spread inside the span
	// or subspans.
	Spread bool
}

func (ts ClockSpan) String() string {
	sep := "-"
	if ts.Spread {
		sep = "~"
	}
	if ts.End != ts.Start {
		s := ts.Start.String() + sep + ts.End.String()
		if ts.Split > 0 {
			s += "/" + strconv.Itoa(int(ts.Split))
		}
		return s
	}
	return ts.Start.String()
}

// Window generates a ScheduleWindow which has the start date same as t. The
// window's start and end time are set according to Start and End, with the end
// time possibly crossing into the next day.
func (ts ClockSpan) Window(t time.Time) ScheduleWindow {
	start := ts.Start.Time(t)
	end := ts.End.Time(t)

	// 23:00-1:00
	if end.Before(start) {
		end = end.Add(24 * time.Hour)
	}
	return ScheduleWindow{
		Start:  start,
		End:    end,
		Spread: ts.Spread,
	}
}

// ClockSpans returns a slice of ClockSpans generated from ts by splitting the
// time between ts.Start and ts.End into ts.Split equal spans.
func (ts ClockSpan) ClockSpans() []ClockSpan {
	if ts.Split == 0 || ts.Split == 1 || ts.End == ts.Start {
		return []ClockSpan{ts}
	}

	span := ts.End.Sub(ts.Start)
	if span < 0 {
		span = -span
	}
	step := span / time.Duration(ts.Split)

	spans := make([]ClockSpan, ts.Split)
	for i := uint(0); i < ts.Split; i++ {
		start := ts.Start.Add(time.Duration(i) * step)
		spans[i] = ClockSpan{
			Start:  start,
			End:    start.Add(step),
			Split:  0, // no more subspans
			Spread: ts.Spread,
		}
	}
	return spans
}

// Schedule represents a single schedule
type Schedule struct {
	WeekSpans  []WeekSpan
	ClockSpans []ClockSpan
}

func (sched *Schedule) String() string {
	var buf bytes.Buffer

	for i, span := range sched.WeekSpans {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(span.String())
	}

	if len(sched.WeekSpans) > 0 && len(sched.ClockSpans) > 0 {
		buf.WriteByte(',')
	}

	for i, span := range sched.ClockSpans {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString(span.String())
	}
	return buf.String()
}

func (sched *Schedule) flattenedClockSpans() []ClockSpan {
	baseTimes := sched.ClockSpans
	if len(baseTimes) == 0 {
		baseTimes = []ClockSpan{{}}
	}

	times := make([]ClockSpan, 0, len(baseTimes))
	for _, ts := range baseTimes {
		times = append(times, ts.ClockSpans()...)
	}
	return times
}

// isLastWeekdayInMonth returns true if t.Weekday() is the last weekday
// occurring this t.Month(), eg. check is Feb 25 2017 is the last Saturday of
// February.
func isLastWeekdayInMonth(t time.Time) bool {
	// try a week from now, if it's still the same month then t.Weekday() is
	// not last
	return t.Month() != t.Add(7*24*time.Hour).Month()
}

// ScheduleWindow represents a time window between Start and End times when the
// scheduled event can happen.
type ScheduleWindow struct {
	Start time.Time
	End   time.Time
	// Spread defines whether the event shall be randomly placed between
	// Start and End times
	Spread bool
}

// Includes returns whether t is inside the window.
func (s ScheduleWindow) Includes(t time.Time) bool {
	return !(t.Before(s.Start) || t.After(s.End))
}

// IsZero returns whether s is uninitialized.
func (s ScheduleWindow) IsZero() bool {
	return s.Start.IsZero() || s.End.IsZero()
}

// Next returns the earliest window after last according to the schedule.
func (sched *Schedule) Next(last time.Time) ScheduleWindow {
	now := timeNow()

	tspans := sched.flattenedClockSpans()

	for t := last; ; t = t.Add(24 * time.Hour) {
		// try to find a matching schedule by moving in 24h jumps, check
		// if the event needs to happen on a specific day in a specific
		// week, next pick the earliest event time

		var window ScheduleWindow

		if len(sched.WeekSpans) > 0 {
			// if there's a week schedule, check if we hit that
			// first
			var weekMatch bool
			for _, week := range sched.WeekSpans {
				if week.Match(t) {
					weekMatch = true
					break
				}
			}

			if !weekMatch {
				continue
			}
		}

		for _, tspan := range tspans {
			// consider all time spans for this particular date and
			// find the earliest possible one that is not before
			// 'now', and does not include the 'last' time
			newWindow := tspan.Window(t)

			if newWindow.End.Before(now) {
				// the time span ends before 'now', try another
				// one
				continue
			}

			if newWindow.Includes(last) {
				// same interval as last update, move forward
				continue
			}

			if window.IsZero() || newWindow.Start.Before(window.Start) {
				// this candidate comes before current
				// candidate, so use it
				window = newWindow
			}
		}
		if window.End.Before(now) {
			// no suitable time span was found this day so try the
			// next day
			continue
		}
		return window
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

	return randutil.RandomDuration(dur)
}

var timeNow = time.Now

// Next returns the earliest event after last according to the provided
// schedule but no later than maxDuration since last.
func Next(schedule []*Schedule, last time.Time, maxDuration time.Duration) time.Duration {
	now := timeNow()

	window := ScheduleWindow{
		Start: last.Add(maxDuration),
		End:   last.Add(maxDuration).Add(1 * time.Hour),
	}

	for _, sched := range schedule {
		next := sched.Next(last)
		if next.Start.Before(window.Start) {
			window = next
		}
	}
	if window.Start.Before(now) {
		return 0
	}

	when := window.Start.Sub(now)
	if window.Spread {
		when += randDur(window.Start, window.End)
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

// parseClockRange parses a string like "9:00-11:00" and returns the start and
// end times.
func parseClockRange(s string) (start, end Clock, err error) {
	l := strings.SplitN(s, "-", 2)
	if len(l) != 2 {
		return start, end, fmt.Errorf("cannot parse %q: not a valid interval", s)
	}

	start = mylog.Check2(ParseClock(l[0]))

	end = mylog.Check2(ParseClock(l[1]))

	return start, end, nil
}

// ParseLegacySchedule takes an obsolete schedule string in the form of:
//
// 9:00-15:00 (every day between 9am and 3pm)
// 9:00-15:00/21:00-22:00 (every day between 9am,5pm and 9pm,10pm)
//
// and returns a list of Schedule types or an error
func ParseLegacySchedule(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, "/") {
		start, end := mylog.Check3(parseClockRange(s))

		schedule = append(schedule, &Schedule{
			ClockSpans: []ClockSpan{{
				Start:  start,
				End:    end,
				Spread: true,
			}},
		})
	}

	return schedule, nil
}

// ParseSchedule parses a schedule in V2 format. The format is described as:
//
//	eventlist = eventset *( ",," eventset )
//	eventset = wdaylist / timelist / wdaylist "," timelist
//
//	wdaylist = wdayset *( "," wdayset )
//	wdayset = wday / wdaynumber / wdayspan
//	wday =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" )
//	wdaynumber =  ( "sun" / "mon" / "tue" / "wed" / "thu" / "fri" / "sat" ) DIGIT
//	wdayspan = wday "-" wday / wdaynumber "-" wday / wday "-" wdaynumber
//
//	timelist = timeset *( "," timeset )
//	timeset = time / timespan
//	time = 2DIGIT ":" 2DIGIT
//	timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
//	count = 1*DIGIT
//
// Examples:
// mon,10:00,,fri,15:00 (Monday at 10:00, Friday at 15:00)
// mon,fri,10:00,15:00 (Monday at 10:00 and 15:00, Friday at 10:00 and 15:00)
// mon-wed,fri,9:00-11:00/2 (Monday to Wednesday and on Friday, twice between
// 9:00 and 11:00)
// mon,9:00~11:00,,wed,22:00~23:00 (Monday, sometime between 9:00 and 11:00,
// and on Wednesday, sometime between 22:00 and 23:00)
// mon,wed  (Monday and on Wednesday)
// mon,,wed (same as above)
// mon1-wed (1st Monday of the month to the following Wednesday)
// mon-wed1 (from the 1st Wednesday of the month to the prior Monday)
// mon1 (1st Monday of the month)
// mon1-mon (from the 1st Monday of the month to the following Monday)
//
// Returns a slice of schedules or an error if parsing failed
func ParseSchedule(scheduleSpec string) ([]*Schedule, error) {
	var schedule []*Schedule

	for _, s := range strings.Split(scheduleSpec, ",,") {
		// cut the schedule in event sets
		//     eventlist = eventset *( ",," eventset )
		sched := mylog.Check2(parseEventSet(s))

		schedule = append(schedule, sched)
	}
	return schedule, nil
}

// parseWeekSpan parses a weekly span such as "mon-tue" or "mon2-tue3".
func parseWeekSpan(s string) (span WeekSpan, err error) {
	var parsed WeekSpan

	split := strings.Split(s, spanToken)
	if len(split) > 2 {
		return span, fmt.Errorf("cannot parse %q: invalid week span", s)
	}

	parsed.Start = mylog.Check2(parseWeekday(split[0]))

	if len(split) == 2 {
		parsed.End = mylog.Check2(parseWeekday(split[1]))
	} else {
		parsed.End = parsed.Start
	}

	if (parsed.Start.Pos != EveryWeek) && (parsed.End.Pos != EveryWeek) {
		// both ends have a week position set

		if parsed.End.Pos < parsed.Start.Pos {
			// eg. mon4-mon1
			return span, fmt.Errorf("cannot parse %q: unsupported schedule", s)
		}

		if !parsed.IsSingleDay() {
			// ambiguous case that produces different schedules depending on
			// the calendar, to avoid the ambiguity, anchor the schedule at
			// the start of the week span, eg. mon1-tue2 -> mon1-tue
			//
			// TODO: error out instead of degrading when a
			// deprecated span is used under the new rules
			parsed.End.Pos = EveryWeek
		}
	}

	return parsed, nil
}

// parseClockSpan parses a time specification which can either be `<hh>:<mm>` or
// `<hh>:<mm>[-~]<hh>:<mm>[/count]`. Alternatively the span can be one of
// special tokens `-`, `~` (followed by an optional [/count]) that indicate a
// whole day span, or a whole day span with spread respectively.
func parseClockSpan(s string) (span ClockSpan, err error) {
	var rest string

	// timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]

	span.Split, rest = mylog.Check3(parseCount(s))

	if strings.Contains(rest, spreadToken) {
		// timespan uses "~" to indicate that the actual event
		// time is to be spread.
		span.Spread = true
		rest = strings.Replace(rest, spreadToken, spanToken, 1)
	}

	if rest == "-" {
		// whole day span
		span.Start = Clock{0, 0}
		span.End = Clock{24, 0}
	} else if strings.Contains(rest, spanToken) {
		span.Start, span.End = mylog.Check3(parseClockRange(rest))
	} else {
		span.Start = mylog.Check2(ParseClock(rest))
		span.End = span.Start
	}

	return span, nil
}

// parseWeekday parses a single weekday (eg. wed, mon5),
func parseWeekday(s string) (week Week, err error) {
	l := len(s)
	if l != 3 && l != 4 {
		return week, fmt.Errorf("cannot parse %q: invalid format", s)
	}

	day := s
	var pos uint
	if l == 4 {
		day = s[0:3]
		v := mylog.Check2(strconv.ParseUint(s[3:], 10, 32))
		if err != nil || v < 1 || v > 5 {
			return week, fmt.Errorf("cannot parse %q: invalid week number", s)
		}
		pos = uint(v)
	}

	weekday, ok := weekdayMap[day]
	if !ok {
		return week, fmt.Errorf("cannot parse %q: invalid weekday", s)
	}

	return Week{weekday, pos}, nil
}

// parseCount will parse the string containing a count token and return the
// count count and the rest of the string with count information removed, or an error.
func parseCount(s string) (count uint, rest string, err error) {
	if !strings.Contains(s, countToken) {
		return 0, s, nil
	}

	// timespan = time ( "-" / "~" ) time [ "/" ( time / count ) ]
	split := strings.Split(s, countToken)
	if len(split) != 2 {
		return 0, "", fmt.Errorf("cannot parse %q: invalid event count", s)
	}

	rest = split[0]
	countStr := split[1]
	c := mylog.Check2(strconv.ParseUint(countStr, 10, 32))
	if err != nil || c == 0 {
		return 0, "", fmt.Errorf("cannot parse %q: invalid event interval", s)
	}
	return uint(c), rest, nil
}

const (
	spanToken   = "-"
	spreadToken = "~"
	countToken  = "/"
)

// Parse event set into a Schedule
func parseEventSet(s string) (*Schedule, error) {
	var fragments []string
	// split eventset into fragments
	//     eventset = wdaylist / timelist / wdaylist "," timelist
	// or wdaysets
	//     wdaylist = wdayset *( "," wdayset )
	// or timesets
	//     timelist = timeset *( "," timeset )
	//
	// NOTE: the syntax is ambiguous in the sense the type of a 'set' is now
	// explicitly indicated, fragments with : inside are expected to be
	// timesets

	if els := strings.Split(s, ","); len(els) > 1 {
		fragments = els
	} else {
		fragments = []string{s}
	}

	var schedule Schedule
	// indicates that any further fragment must be timesets
	var expectTime bool

	for _, fragment := range fragments {
		if len(fragment) == 0 {
			return nil, fmt.Errorf("cannot parse %q: not a valid fragment", s)
		}

		if strings.Contains(fragment, ":") {
			// must be a clock span
			span := mylog.Check2(parseClockSpan(fragment))

			schedule.ClockSpans = append(schedule.ClockSpans, span)

			expectTime = true

		} else if !expectTime {
			// we're not expecting timeset , so this must be a wdayset
			span := mylog.Check2(parseWeekSpan(fragment))

			schedule.WeekSpans = append(schedule.WeekSpans, span)
		} else {
			// not a timeset
			return nil, fmt.Errorf("cannot parse %q: invalid schedule fragment", fragment)
		}
	}

	return &schedule, nil
}

// Includes checks whether given time t falls inside the time range covered by
// the schedule. A single time schedule eg. '10:00' is treated as spanning the
// time [10:00, 10:01)
func (sched *Schedule) Includes(t time.Time) bool {
	if len(sched.WeekSpans) > 0 {
		var weekMatch bool
		for _, week := range sched.WeekSpans {
			if week.Match(t) {
				weekMatch = true
				break
			}
		}
		if !weekMatch {
			return false
		}
	}

	for _, tspan := range sched.flattenedClockSpans() {
		window := tspan.Window(t)
		if window.End.Equal(window.Start) {
			// schedule granularity is a minute, a schedule '10:00'
			// in fact is: [10:00, 10:01)
			window.End = window.End.Add(time.Minute)
		}
		// Includes() does the [start,end] check, but we really what
		// [start,end)
		if window.Includes(t) && t.Before(window.End) {
			return true
		}
	}
	return false
}

// Includes checks whether given time t falls inside the time range covered by
// a schedule.
func Includes(schedule []*Schedule, t time.Time) bool {
	for _, sched := range schedule {
		if sched.Includes(t) {
			return true
		}
	}
	return false
}
