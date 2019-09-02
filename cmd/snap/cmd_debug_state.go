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
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type cmdDebugState struct {
	timeMixin

	st *state.State

	Changes  bool   `long:"changes"`
	TaskID   string `long:"task"`
	ChangeID string `long:"change"`

	// flags for --change=N output
	DotOutput bool `long:"dot"` // XXX: mildly useful (too crowded in many cases), but let's have it just in case
	// When inspecting errors/undone tasks, those in Hold state are usually irrelevant, make it possible to ignore them
	NoHoldState bool `long:"no-hold"`

	Positional struct {
		StateFilePath string `positional-args:"yes" positional-arg-name:"<state-file>"`
	} `positional-args:"yes"`
}

var cmdDebugStateShortHelp = i18n.G("Inspect a snapd state file.")
var cmdDebugStateLongHelp = i18n.G("Inspect a snapd state file, bypassing snapd API.")

type byChangeID []*state.Change

func (c byChangeID) Len() int           { return len(c) }
func (c byChangeID) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c byChangeID) Less(i, j int) bool { return c[i].ID() < c[j].ID() }

func loadState(path string) (*state.State, error) {
	if path == "" {
		path = "state.json"
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	return state.ReadState(nil, r)
}

func init() {
	addDebugCommand("state", cmdDebugStateShortHelp, cmdDebugStateLongHelp, func() flags.Commander {
		return &cmdDebugState{}
	}, timeDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"change":  i18n.G("ID of the change to inspect"),
		"task":    i18n.G("ID of the task to inspect"),
		"dot":     i18n.G("Dot (graphviz) output"),
		"no-hold": i18n.G("Omit tasks in 'Hold' state in the change output"),
		"changes": i18n.G("List all changes"),
	}), nil)
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

func (c *cmdDebugState) writeDotOutput(st *state.State, changeID string) error {
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

func (c *cmdDebugState) showTasks(st *state.State, changeID string) error {
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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			strings.Join(lanes, ","),
			t.ID(),
			t.Status().String(),
			c.fmtTime(t.SpawnTime()),
			c.fmtTime(t.ReadyTime()),
			t.Kind(),
			t.Summary())
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

func (c *cmdDebugState) showChanges(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	sort.Sort(byChangeID(changes))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, chg := range changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", chg.ID(), chg.Status().String(), c.fmtTime(chg.SpawnTime()), c.fmtTime(chg.ReadyTime()), chg.Kind(), chg.Summary())
	}
	w.Flush()

	return nil
}

func (c *cmdDebugState) showTask(st *state.State, taskID string) error {
	st.Lock()
	defer st.Unlock()

	task := st.Task(taskID)
	if task == nil {
		return fmt.Errorf("no such task: %s", taskID)
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	// the output of 'debug task' is yaml'ish
	fmt.Fprintf(Stdout, "id: %s\nkind: %s\nsummary: %s\nstatus: %s\n", taskID, task.Kind(), task.Summary(), task.Status().String())
	log := task.Log()
	if len(log) > 0 {
		fmt.Fprintf(Stdout, "log: |\n")
		for _, msg := range log {
			if err := wrapLine(Stdout, []rune(msg), "  ", termWidth); err != nil {
				break
			}
		}
		fmt.Fprintln(Stdout)
	}

	fmt.Fprintf(Stdout, "halt-tasks:")
	if len(task.HaltTasks()) == 0 {
		fmt.Fprintln(Stdout, " []")
	} else {
		fmt.Fprintln(Stdout)
		for _, ht := range task.HaltTasks() {
			fmt.Fprintf(Stdout, " - %s (%s)\n", ht.Kind(), ht.ID())
		}
	}

	return nil
}

func (c *cmdDebugState) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}

	// check valid combinations of args
	var cmds []string
	if c.Changes {
		cmds = append(cmds, "--changes")
	}
	if c.ChangeID != "" {
		cmds = append(cmds, "--change=")
	}
	if c.TaskID != "" {
		cmds = append(cmds, "--task=")
	}
	if len(cmds) > 1 {
		return fmt.Errorf("cannot use %s and %s together", cmds[0], cmds[1])
	}

	if c.DotOutput && c.ChangeID == "" {
		return fmt.Errorf("--dot can only be used with --change=")
	}
	if c.NoHoldState && c.ChangeID == "" {
		return fmt.Errorf("--no-hold can only be used with --change=")
	}

	if c.Changes {
		return c.showChanges(st)
	}

	if c.ChangeID != "" {
		_, err := strconv.ParseInt(c.ChangeID, 0, 64)
		if err != nil {
			return fmt.Errorf("invalid change: %s", c.ChangeID)
		}
		if c.DotOutput {
			return c.writeDotOutput(st, c.ChangeID)
		}
		return c.showTasks(st, c.ChangeID)
	}

	if c.TaskID != "" {
		_, err := strconv.ParseInt(c.TaskID, 0, 64)
		if err != nil {
			return fmt.Errorf("invalid task: %s", c.TaskID)
		}
		return c.showTask(st, c.TaskID)
	}

	// show changes by default
	return c.showChanges(st)
}
