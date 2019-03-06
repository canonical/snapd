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
	"time"

	"github.com/jessevdk/go-flags"
)

type cmdChangeTimings struct {
	changeIDMixin
}

func init() {
	addDebugCommand("timings",
		"Get the timings of the tasks of a change",
		"The timings command displays details about the time each task runs.",
		func() flags.Commander {
			return &cmdChangeTimings{}
		}, changeIDMixinOptDesc, changeIDMixinArgDesc)
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
		DoingTime, UndoingTime time.Duration
	}
	params := struct {
		ChgID string `json:"chg-id"`
	}{
		ChgID: chgid,
	}
	if err := x.client.Debug("change-timings", params, &timings); err != nil {
		return err
	}

	// now combine with the other data about the change
	chg, err := x.client.Change(chgid)
	if err != nil {
		return err
	}
	w := tabWriter()
	fmt.Fprintf(w, "ID\tStatus\t%11s\t%11s\tSummary\n", "Doing", "Undoing")
	for _, t := range chg.Tasks {
		doingTime := timings[t.ID].DoingTime.Round(time.Millisecond).String()
		if timings[t.ID].DoingTime == 0 {
			doingTime = "-"
		}
		undoingTime := timings[t.ID].UndoingTime.Round(time.Millisecond).String()
		if timings[t.ID].UndoingTime == 0 {
			undoingTime = "-"
		}
		summary := t.Summary
		// Duration formats to 17m14.342s or 2.038s or 970ms, so with
		// 11 chars we can go up to 59m59.999s
		fmt.Fprintf(w, "%s\t%s\t%11s\t%11s\t%s\n", t.ID, t.Status, doingTime, undoingTime, summary)
	}
	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}
