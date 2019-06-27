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
	"strings"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdChangeTimings struct {
	changeIDMixin
	EnsureTag  string `long:"ensure" choice:"auto-refresh" choice:"become-operational" choice:"refresh-catalogs" choice:"refresh-hints" choice:"seed"`
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
		if doing {
			doingTimeStr = "-"
			undoingTimeStr = formatDuration(t.Duration)
		}
	}
	printTiming(w, verbose, t.Level+1, "", "", doingTimeStr, undoingTimeStr, t.Label, t.Summary)
}

func (x *cmdChangeTimings) printChangeTimings(w io.Writer, timing *timingsData) error {
	chgid := timing.ChangeID
	chg, err := x.client.Change(chgid)
	if err != nil {
		return err
	}

	for _, t := range chg.Tasks {
		doingTime := formatDuration(timing.ChangeTimings[t.ID].DoingTime)
		if timing.ChangeTimings[t.ID].DoingTime == 0 {
			doingTime = "-"
		}
		undoingTime := formatDuration(timing.ChangeTimings[t.ID].UndoingTime)
		if timing.ChangeTimings[t.ID].UndoingTime == 0 {
			undoingTime = "-"
		}

		printTiming(w, x.Verbose, 0, t.ID, t.Status, doingTime, undoingTime, t.Kind, t.Summary)
		for _, nested := range timing.ChangeTimings[t.ID].DoingTimings {
			showDoing := true
			printTaskTiming(w, &nested, x.Verbose, showDoing)
		}
		for _, nested := range timing.ChangeTimings[t.ID].UndoingTimings {
			showDoing := false
			printTaskTiming(w, &nested, x.Verbose, showDoing)
		}
	}

	return nil
}

func (x *cmdChangeTimings) printEnsureTimings(w io.Writer, timings []*timingsData) error {
	for _, td := range timings {
		printTiming(w, x.Verbose, 0, x.EnsureTag, "", "-", "-", "", "")
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
		printTiming(w, x.Verbose, 0, x.StartupTag, "", "-", "-", "", "")
		for _, t := range td.StartupTimings {
			printTiming(w, x.Verbose, t.Level+1, "", "", formatDuration(t.Duration), "-", t.Label, t.Summary)
		}
	}
	return nil
}

type timingsData struct {
	ChangeID       string   `json:"change-id"`
	EnsureTimings  []Timing `json:"ensure-timings,omitempty"`
	StartupTimings []Timing `json:"startup-timings,omitempty"`
	ChangeTimings  map[string]struct {
		DoingTime      time.Duration `json:"doing-time,omitempty"`
		UndoingTime    time.Duration `json:"undoing-time,omitempty"`
		DoingTimings   []Timing      `json:"doing-timings,omitempty"`
		UndoingTimings []Timing      `json:"undoing-timings,omitempty"`
	} `json:"change-timings,omitempty"`
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

	if err := x.checkConflictingFlags(); err != nil {
		return err
	}

	var chgid string
	var err error

	if x.EnsureTag == "" && x.StartupTag == "" {
		if x.Positional.ID == "" && x.LastChangeType == "" {
			// GetChangeID() below checks for empty change ID / --last, check them early here to provide more helpful error message
			return fmt.Errorf("please provide change ID or type with --last=<type>, or query for --ensure=<name> or --startup=<name>")
		}

		// GetChangeID takes care of --last=... if change ID was not specified by the user
		chgid, err = x.GetChangeID()
		if err != nil {
			return err
		}
	}

	// gather debug timings first
	var timings []*timingsData
	var allEnsures string
	if x.All {
		allEnsures = "true"
	} else {
		allEnsures = "false"
	}
	if err := x.client.DebugGet("change-timings", &timings, map[string]string{"change-id": chgid, "ensure": x.EnsureTag, "all": allEnsures, "startup": x.StartupTag}); err != nil {
		return err
	}

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
