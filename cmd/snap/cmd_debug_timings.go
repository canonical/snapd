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
	Verbose bool `long:"verbose"`
}

func init() {
	addDebugCommand("timings",
		"Get the timings of the tasks of a change",
		"The timings command displays details about the time each task runs.",
		func() flags.Commander {
			return &cmdChangeTimings{}
		}, changeIDMixinOptDesc.also(map[string]string{
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

func printTiming(w io.Writer, t *Timing, verbose, doing bool) {
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
	if verbose {
		fmt.Fprintf(w, "%s\t \t%11s\t%11s\t%s\t%s\n", strings.Repeat(" ", t.Level+1)+"^", doingTimeStr, undoingTimeStr, t.Label, strings.Repeat(" ", 2*(t.Level+1))+t.Summary)
	} else {
		fmt.Fprintf(w, "%s\t \t%11s\t%11s\t%s\n", strings.Repeat(" ", t.Level+1)+"^", doingTimeStr, undoingTimeStr, strings.Repeat(" ", 2*(t.Level+1))+t.Summary)
	}
}

func (x *cmdChangeTimings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	chgid, err := x.GetChangeID()
	if err != nil {
		return err
	}

	// gather debug timings first
	var timings map[string]struct {
		DoingTime      time.Duration `json:"doing-time,omitempty"`
		UndoingTime    time.Duration `json:"undoing-time,omitempty"`
		DoingTimings   []Timing      `json:"doing-timings,omitempty"`
		UndoingTimings []Timing      `json:"undoing-timings,omitempty"`
	}

	if err := x.client.DebugGet("change-timings", &timings, map[string]string{"chg-id": chgid}); err != nil {
		return err
	}

	// now combine with the other data about the change
	chg, err := x.client.Change(chgid)
	if err != nil {
		return err
	}
	w := tabWriter()
	if x.Verbose {
		fmt.Fprintf(w, "ID\tStatus\t%11s\t%11s\tLabel\tSummary\n", "Doing", "Undoing")
	} else {
		fmt.Fprintf(w, "ID\tStatus\t%11s\t%11s\tSummary\n", "Doing", "Undoing")
	}
	for _, t := range chg.Tasks {
		doingTime := formatDuration(timings[t.ID].DoingTime)
		if timings[t.ID].DoingTime == 0 {
			doingTime = "-"
		}
		undoingTime := formatDuration(timings[t.ID].UndoingTime)
		if timings[t.ID].UndoingTime == 0 {
			undoingTime = "-"
		}
		summary := t.Summary
		// Duration formats to 17m14.342s or 2.038s or 970ms, so with
		// 11 chars we can go up to 59m59.999s
		if x.Verbose {
			fmt.Fprintf(w, "%s\t%s\t%11s\t%11s\t%s\t%s\n", t.ID, t.Status, doingTime, undoingTime, t.Kind, summary)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%11s\t%11s\t%s\n", t.ID, t.Status, doingTime, undoingTime, summary)
		}

		for _, nested := range timings[t.ID].DoingTimings {
			showDoing := true
			printTiming(w, &nested, x.Verbose, showDoing)
		}
		for _, nested := range timings[t.ID].UndoingTimings {
			showDoing := false
			printTiming(w, &nested, x.Verbose, showDoing)
		}
	}
	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}
