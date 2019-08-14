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
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type cmdDebugTasks struct {
	baseOfflineDebugCommand

	ChangeID  string `long:"change-id" required:"yes"`
	DotOutput bool   `long:"dot"` // XXX: mildly useful (too crowded in many cases), but let's have it just in case

	// When inspecting errors/undone tasks, those in Hold state are usually irrelevant, make it possible to ignore them
	NoHoldState bool `long:"no-hold"`
}

var shortDebugTasksHelp = i18n.G("The tasks command prints tasks of the given change.")
var longDebugTasksHelp = i18n.G("The change command prints tasks of the given change.")

func init() {
	descs := map[string]string{
		"change-id": i18n.G("ID of the change to inspect"),
		"dot":       i18n.G("Dot (graphviz) output"),
		"no-hold":   i18n.G("Omit tasks in 'Hold' state in the output"),
	}
	// tasks and change commands do the same and exist for symmetry with 'snap change' / 'snap tasks'.
	addDebugCommand("tasks", shortDebugTasksHelp, longDebugTasksHelp, func() flags.Commander {
		return &cmdDebugTasks{}
	}, descs, nil)
	addDebugCommand("change", shortDebugTasksHelp, longDebugTasksHelp, func() flags.Commander {
		return &cmdDebugTasks{}
	}, descs, nil)
}

type byLaneAndWaitTaskChain []*state.Task

func (t byLaneAndWaitTaskChain) Len() int      { return len(t) }
func (t byLaneAndWaitTaskChain) Swap(i, j int) { t[i], t[j] = t[j], t[i] }
func (t byLaneAndWaitTaskChain) Less(i, j int) bool {
	// cover the typical case (just one lane), and order by first lane
	if t[i].Lanes()[0] == t[j].Lanes()[0] {
		return waitChainSearch(t[i], t[j])
	}
	return t[i].Lanes()[0] < t[j].Lanes()[0]
}

func waitChainSearch(startT, searchT *state.Task) bool {
	for _, cand := range startT.HaltTasks() {
		if cand == searchT {
			return true
		}
		if waitChainSearch(cand, searchT) {
			return true
		}
	}

	return false
}

func (c *cmdDebugTasks) writeDotOutput(st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}

	fmt.Fprintf(Stdout, "digraph D{\n")
	tasks := chg.Tasks()
	for _, t := range tasks {
		if c.NoHoldState && t.Status() == state.HoldStatus {
			continue
		}
		fmt.Fprintf(Stdout, "  %s [label=%q];\n", t.ID(), t.Kind())
		for _, wt := range t.WaitTasks() {
			if c.NoHoldState && wt.Status() == state.HoldStatus {
				continue
			}
			fmt.Fprintf(Stdout, "  %s -> %s;\n", t.ID(), wt.ID())
		}
	}
	fmt.Fprintf(Stdout, "}\n")

	return nil
}

func (c *cmdDebugTasks) showTasks(st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}

	tasks := chg.Tasks()
	sort.Sort(byLaneAndWaitTaskChain(tasks))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "Lanes\tID\tStatus\tSpawn\tReady\tKind\tSummary\n")
	for _, t := range tasks {
		if c.NoHoldState && t.Status() == state.HoldStatus {
			continue
		}
		var lanes []string
		for _, lane := range t.Lanes() {
			lanes = append(lanes, fmt.Sprintf("%d", lane))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", strings.Join(lanes, ","), t.ID(), t.Status().String(), formatTime(t.SpawnTime()), formatTime(t.ReadyTime()), t.Kind(), t.Summary())
	}

	w.Flush()

	for _, t := range tasks {
		logs := t.Log()
		if len(logs) > 0 {
			fmt.Fprintf(Stdout, "---\n")
			fmt.Fprintf(Stdout, "%s %s\n", t.ID(), t.Summary())
			for _, log := range logs {
				fmt.Fprintf(Stdout, "  %s\n", log)
			}
		}
	}

	return nil
}

func (c *cmdDebugTasks) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}

	if c.DotOutput {
		return c.writeDotOutput(st, c.ChangeID)
	}
	return c.showTasks(st, c.ChangeID)
}
