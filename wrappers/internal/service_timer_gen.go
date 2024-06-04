// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package internal

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timeutil"
)

func makeAbbrevWeekdays(start time.Weekday, end time.Weekday) []string {
	out := make([]string, 0, 7)
	for w := start; w%7 != (end + 1); w++ {
		out = append(out, time.Weekday(w % 7).String()[0:3])
	}
	return out
}

// daysRange generates a string representing a continuous range between given
// day numbers, which due to compatiblilty with old systemd version uses a
// verbose syntax of x,y,z instead of x..z
func daysRange(start, end uint) string {
	var buf bytes.Buffer
	for i := start; i <= end; i++ {
		buf.WriteString(strconv.FormatInt(int64(i), 10))
		if i < end {
			buf.WriteRune(',')
		}
	}
	return buf.String()
}

// generateOnCalendarSchedules converts a schedule into OnCalendar schedules
// suitable for use in systemd *.timer units using systemd.time(7)
// https://www.freedesktop.org/software/systemd/man/systemd.time.html
// XXX: old systemd versions do not support x..y ranges
func generateOnCalendarSchedules(schedule []*timeutil.Schedule) []string {
	calendarEvents := make([]string, 0, len(schedule))
	for _, sched := range schedule {
		days := make([]string, 0, len(sched.WeekSpans))
		for _, week := range sched.WeekSpans {
			abbrev := strings.Join(makeAbbrevWeekdays(week.Start.Weekday, week.End.Weekday), ",")

			if week.Start.Pos == timeutil.EveryWeek && week.End.Pos == timeutil.EveryWeek {
				// eg: mon, mon-fri, fri-mon
				days = append(days, fmt.Sprintf("%s *-*-*", abbrev))
				continue
			}
			// examples:
			// mon1 - Mon *-*-1..7 (Monday during the first 7 days)
			// fri1 - Fri *-*-1..7 (Friday during the first 7 days)

			// entries below will make systemd timer expire more
			// frequently than the schedule suggests, however snap
			// runner evaluates current time and gates the actual
			// action
			//
			// mon1-tue - *-*-1..7 *-*-8 (anchored at first
			// Monday; Monday happens during the 7 days,
			// Tuesday can possibly happen on the 8th day if
			// the month started on Tuesday)
			//
			// mon-tue1 - *-*~1 *-*-1..7 (anchored at first
			// Tuesday; matching Monday can happen on the
			// last day of previous month if Tuesday is the
			// 1st)
			//
			// mon5-tue - *-*~1..7 *-*-1 (anchored at last
			// Monday, the matching Tuesday can still happen
			// within the last 7 days, or on the 1st of the
			// next month)
			//
			// fri4-mon - *-*-22-31 *-*-1..7 (anchored at 4th
			// Friday, can span onto the next month, extreme case in
			// February when 28th is Friday)
			//
			// XXX: since old versions of systemd, eg. 229 available
			// in 16.04 does not support x..y ranges, days need to
			// be enumerated like so:
			// Mon *-*-1..7 -> Mon *-*-1,2,3,4,5,6,7
			//
			// XXX: old systemd versions do not support the last n
			// days syntax eg, *-*~1, thus the range needs to be
			// generated in more verbose way like so:
			// Mon *-*~1..7 -> Mon *-*-22,23,24,25,26,27,28,29,30,31
			// (22-28 is the last week, but the month can have
			// anywhere from 28 to 31 days)
			//
			startPos := week.Start.Pos
			endPos := startPos
			if !week.AnchoredAtStart() {
				startPos = week.End.Pos
				endPos = startPos
			}
			startDay := (startPos-1)*7 + 1
			endDay := (endPos) * 7

			if week.IsSingleDay() {
				// single day, can use the 'weekday' filter
				if startPos == timeutil.LastWeek {
					// last week of a month, which can be
					// 22-28 in case of February, while
					// month can have between 28 and 31 days
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(22, 31)))
				} else {
					days = append(days,
						fmt.Sprintf("%s *-*-%s", abbrev, daysRange(startDay, endDay)))
				}
				continue
			}

			if week.AnchoredAtStart() {
				// explore the edge cases first
				switch startPos {
				case timeutil.LastWeek:
					// starts in the last week of the month and
					// possibly spans into the first week of the
					// next month;
					// month can have between 28 and 31
					// days
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				case 4:
					// a range in the 4th week can span onto
					// the next week, which is either 28-31
					// or in extreme case (eg. February with
					// 28 days) 1-7 of the next month
					days = append(days,
						// trailing 29-31 that are not part of a full week
						fmt.Sprintf("*-*-%s", daysRange(29, 31)),
						fmt.Sprintf("*-*-%s", daysRange(1, 7)))
				default:
					// can possibly spill into the next week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay+7, endDay+7)))
				}

				if startDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					// from the end of the month
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			} else {
				switch endPos {
				case timeutil.LastWeek:
					// month can have between 28 and 31
					// days, add trailing 29-31 that are not
					// part of a full week
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(29, 31)))
				case 1:
					// possibly spans from the last week of the
					// previous month and ends in the first week of
					// current month
					days = append(days, fmt.Sprintf("*-*-%s", daysRange(22, 31)))
				default:
					// can possibly spill into the previous week
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
				if endDay < 28 {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay, endDay)))
				} else {
					days = append(days,
						fmt.Sprintf("*-*-%s", daysRange(startDay-7, endDay-7)))
				}
			}
		}

		if len(days) == 0 {
			// no weekday spec, meaning the timer runs every day
			days = []string{"*-*-*"}
		}

		startTimes := make([]string, 0, len(sched.ClockSpans))
		for _, clocks := range sched.ClockSpans {
			// use expanded clock spans
			for _, span := range clocks.ClockSpans() {
				when := span.Start
				if span.Spread {
					length := span.End.Sub(span.Start)
					if length < 0 {
						// span Start wraps around, so we have '00:00.Sub(23:45)'
						length = -length
					}
					if length > 5*time.Minute {
						// replicate what timeutil.Next() does
						// and cut some time at the end of the
						// window so that events do not happen
						// directly one after another
						length -= 5 * time.Minute
					}
					when = when.Add(randutil.RandomDuration(length))
				}
				if when.Hour == 24 {
					// 24:00 for us means the other end of
					// the day, for systemd we need to
					// adjust it to the 0-23 hour range
					when.Hour -= 24
				}

				startTimes = append(startTimes, when.String())
			}
		}

		for _, day := range days {
			if len(startTimes) == 0 {
				// current schedule is days only
				calendarEvents = append(calendarEvents, day)
				continue
			}

			for _, startTime := range startTimes {
				calendarEvents = append(calendarEvents, fmt.Sprintf("%s %s", day, startTime))
			}
		}
	}
	return calendarEvents
}

func GenerateSnapServiceTimerUnitFile(app *snap.AppInfo) ([]byte, error) {
	timerTemplate := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Timer {{.TimerName}} for snap application {{.App.Snap.InstanceName}}.{{.App.Name}}
{{- if .MountUnit}}
Requires={{.MountUnit}}
After={{.MountUnit}}
{{- end}}
X-Snappy=yes

[Timer]
Unit={{.ServiceFileName}}
{{ range .Schedules }}OnCalendar={{ . }}
{{ end }}
[Install]
WantedBy={{.TimersTarget}}
`
	var templateOut bytes.Buffer
	t := template.Must(template.New("timer-wrapper").Parse(timerTemplate))

	timerSchedule, err := timeutil.ParseSchedule(app.Timer.Timer)
	if err != nil {
		return nil, err
	}

	schedules := generateOnCalendarSchedules(timerSchedule)

	wrapperData := struct {
		App             *snap.AppInfo
		ServiceFileName string
		TimersTarget    string
		TimerName       string
		MountUnit       string
		Schedules       []string
	}{
		App:             app,
		ServiceFileName: filepath.Base(app.ServiceFile()),
		TimersTarget:    systemd.TimersTarget,
		TimerName:       app.Name,
		Schedules:       schedules,
	}
	switch app.DaemonScope {
	case snap.SystemDaemon:
		wrapperData.MountUnit = filepath.Base(systemd.MountUnitPath(app.Snap.MountDir()))
	case snap.UserDaemon:
		// nothing
	default:
		panic("unknown snap.DaemonScope")
	}

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.Bytes(), nil
}
