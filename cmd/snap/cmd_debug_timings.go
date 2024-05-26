// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdChangeTimings struct {
	changeIDMixin
	EnsureTag  string `long:"ensure" choice:"auto-refresh" choice:"become-operational" choice:"refresh-catalogs" choice:"refresh-hints" choice:"seed" choice:"install-system"`
	All        bool   `long:"all"`
	StartupTag string `long:"startup" choice:"load-state" choice:"ifacemgr"`
	Verbose    bool   `long:"verbose"`
}

func init() {
	addDebugCommand("timings",
		i18n.G("Get the timings of the tasks of a change"),
		i18n.G("The timings command displays details about the time each task runs."),
		func() flags.Commander {
			return &cmdChangeTimings{}
		}, changeIDMixinOptDesc.also(map[string]string{
			"ensure":  i18n.G("Show timings for a change related to the given Ensure activity (one of: auto-refresh, become-operational, refresh-catalogs, refresh-hints, seed)"),
			"all":     i18n.G("Show timings for all executions of the given Ensure or startup activity, not just the latest"),
			"startup": i18n.G("Show timings for the startup of given subsystem (one of: load-state, ifacemgr)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"verbose": i18n.G("Show more information"),
		}), changeIDMixinArgDesc)
}

type Timing struct {
	Level    int           `json:"level,omitempty"`
	Label    string        `json:"label,omitempty"`
	Summary  string        `json:"summary,omitempty"`
	Duration time.Duration `json:"duration,omitempty"`
}

func formatDuration(dur time.Duration) string {
	return fmt.Sprintf("%dms", dur/time.Millisecond)
}

func printTiming(w io.Writer, verbose bool, nestLevel int, id, status, doingTimeStr, undoingTimeStr, label, summary string) {
	// don't display id for nesting>1, instead show nesting indicator
	if nestLevel > 0 {
		id = strings.Repeat(" ", nestLevel) + "^"
	}
	// Duration formats to 17m14.342s or 2.038s or 970ms, so with
	// 11 chars we can go up to 59m59.999s
	if verbose {
		fmt.Fprintf(w, "%s\t%s\t%11s\t%11s\t%s\t%s\n", id, status, doingTimeStr, undoingTimeStr, label, strings.Repeat(" ", 2*nestLevel)+summary)
	} else {
		fmt.Fprintf(w, "%s\t%s\t%11s\t%11s\t%s\n", id, status, doingTimeStr, undoingTimeStr, strings.Repeat(" ", 2*nestLevel)+summary)
	}
}

func printTaskTiming(w io.Writer, t *Timing, verbose, doing bool) {
	var doingTimeStr, undoingTimeStr string
	if doing {
		doingTimeStr = formatDuration(t.Duration)
		undoingTimeStr = "-"
	} else {
		doingTimeStr = "-"
		undoingTimeStr = formatDuration(t.Duration)
	}
	printTiming(w, verbose, t.Level+1, "", "", doingTimeStr, undoingTimeStr, t.Label, t.Summary)
}

// sortTimingsTasks sorts tasks from changeTimings by lane and ready time with special treatment of lane 0 tasks:
//   - tasks from lanes >0 are grouped by lanes and sorted by ready time.
//   - tasks from lane 0 are sorted by ready time and inserted before and after other lanes based on the min/max
//     ready times of non-zero lanes.
//   - tasks from lane 0 with ready time between non-zero lane tasks are not really expected in our system and will
//     appear after non-zero lane tasks.
func sortTimingsTasks(timings map[string]changeTimings) []string {
	tasks := make([]string, 0, len(timings))

	var minReadyTime time.Time
	// determine min ready time from all non-zero lane tasks
	for taskID, taskData := range timings {
		if taskData.Lane > 0 {
			if minReadyTime.IsZero() {
				minReadyTime = taskData.ReadyTime
			}
			if taskData.ReadyTime.Before(minReadyTime) {
				minReadyTime = taskData.ReadyTime
			}
		}
		tasks = append(tasks, taskID)
	}

	sort.Slice(tasks, func(i, j int) bool {
		t1 := timings[tasks[i]]
		t2 := timings[tasks[j]]
		if t1.Lane != t2.Lane {
			// if either t1 or t2 is from lane 0, then it comes before or after non-zero lane tasks
			if t1.Lane == 0 {
				return t1.ReadyTime.Before(minReadyTime)
			}
			if t2.Lane == 0 {
				return !t2.ReadyTime.Before(minReadyTime)
			}
			// different lanes (but neither of them is 0), order by lane
			return t1.Lane < t2.Lane
		}

		// same lane - order by ready time
		return t1.ReadyTime.Before(t2.ReadyTime)
	})

	return tasks
}

func (x *cmdChangeTimings) printChangeTimings(w io.Writer, timing *timingsData) error {
	tasks := sortTimingsTasks(timing.ChangeTimings)

	for _, taskID := range tasks {
		chgTiming := timing.ChangeTimings[taskID]
		doingTime := formatDuration(timing.ChangeTimings[taskID].DoingTime)
		if chgTiming.DoingTime == 0 {
			doingTime = "-"
		}
		undoingTime := formatDuration(timing.ChangeTimings[taskID].UndoingTime)
		if chgTiming.UndoingTime == 0 {
			undoingTime = "-"
		}

		printTiming(w, x.Verbose, 0, taskID, chgTiming.Status, doingTime, undoingTime, chgTiming.Kind, chgTiming.Summary)
		for _, nested := range timing.ChangeTimings[taskID].DoingTimings {
			showDoing := true
			printTaskTiming(w, &nested, x.Verbose, showDoing)
		}
		for _, nested := range timing.ChangeTimings[taskID].UndoingTimings {
			showDoing := false
			printTaskTiming(w, &nested, x.Verbose, showDoing)
		}
	}

	return nil
}

func (x *cmdChangeTimings) printEnsureTimings(w io.Writer, timings []*timingsData) error {
	for _, td := range timings {
		printTiming(w, x.Verbose, 0, x.EnsureTag, "", formatDuration(td.TotalDuration), "-", "", "")
		for _, t := range td.EnsureTimings {
			printTiming(w, x.Verbose, t.Level+1, "", "", formatDuration(t.Duration), "-", t.Label, t.Summary)
		}

		// change is optional for ensure timings
		if td.ChangeID != "" {
			x.printChangeTimings(w, td)
		}
	}
	return nil
}

func (x *cmdChangeTimings) printStartupTimings(w io.Writer, timings []*timingsData) error {
	for _, td := range timings {
		printTiming(w, x.Verbose, 0, x.StartupTag, "", formatDuration(td.TotalDuration), "-", "", "")
		for _, t := range td.StartupTimings {
			printTiming(w, x.Verbose, t.Level+1, "", "", formatDuration(t.Duration), "-", t.Label, t.Summary)
		}
	}
	return nil
}

type changeTimings struct {
	Status         string        `json:"status,omitempty"`
	Kind           string        `json:"kind,omitempty"`
	Summary        string        `json:"summary,omitempty"`
	Lane           int           `json:"lane,omitempty"`
	ReadyTime      time.Time     `json:"ready-time,omitempty"`
	DoingTime      time.Duration `json:"doing-time,omitempty"`
	UndoingTime    time.Duration `json:"undoing-time,omitempty"`
	DoingTimings   []Timing      `json:"doing-timings,omitempty"`
	UndoingTimings []Timing      `json:"undoing-timings,omitempty"`
}

type timingsData struct {
	ChangeID       string        `json:"change-id"`
	EnsureTimings  []Timing      `json:"ensure-timings,omitempty"`
	StartupTimings []Timing      `json:"startup-timings,omitempty"`
	TotalDuration  time.Duration `json:"total-duration,omitempty"`
	// ChangeTimings are indexed by task id
	ChangeTimings map[string]changeTimings `json:"change-timings,omitempty"`
}

func (x *cmdChangeTimings) checkConflictingFlags() error {
	var i int
	for _, opt := range []string{string(x.Positional.ID), x.StartupTag, x.EnsureTag} {
		if opt != "" {
			i++
			if i > 1 {
				return fmt.Errorf("cannot use change id, 'startup' or 'ensure' together")
			}
		}
	}

	if x.All && (x.Positional.ID != "" || x.LastChangeType != "") {
		return fmt.Errorf("cannot use 'all' with change id or 'last'")
	}
	return nil
}

func (x *cmdChangeTimings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(x.checkConflictingFlags())

	var chgid string

	if x.EnsureTag == "" && x.StartupTag == "" {
		if x.Positional.ID == "" && x.LastChangeType == "" {
			// GetChangeID() below checks for empty change ID / --last, check them early here to provide more helpful error message
			return fmt.Errorf("please provide change ID or type with --last=<type>, or query for --ensure=<name> or --startup=<name>")
		}

		// GetChangeID takes care of --last=... if change ID was not specified by the user
		chgid = mylog.Check2(x.GetChangeID())

	}

	// gather debug timings first
	var timings []*timingsData
	var allEnsures string
	if x.All {
		allEnsures = "true"
	} else {
		allEnsures = "false"
	}
	mylog.Check(x.client.DebugGet("change-timings", &timings, map[string]string{"change-id": chgid, "ensure": x.EnsureTag, "all": allEnsures, "startup": x.StartupTag}))

	w := tabWriter()
	if x.Verbose {
		fmt.Fprintf(w, "ID\tStatus\t%11s\t%11s\tLabel\tSummary\n", "Doing", "Undoing")
	} else {
		fmt.Fprintf(w, "ID\tStatus\t%11s\t%11s\tSummary\n", "Doing", "Undoing")
	}

	// If a specific change was requested, we expect exactly one timingsData element.
	// If "ensure" activity was requested, we may get multiple elements (for multiple executions of the ensure)
	if chgid != "" && len(timings) > 0 {
		x.printChangeTimings(w, timings[0])
	}

	if x.EnsureTag != "" {
		x.printEnsureTimings(w, timings)
	}

	if x.StartupTag != "" {
		x.printStartupTimings(w, timings)
	}

	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}
