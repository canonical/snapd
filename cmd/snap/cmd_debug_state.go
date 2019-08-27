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
	"text/tabwriter"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type cmdDebugState struct {
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

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func loadState(path string) (*state.State, error) {
	if path == "" {
		path = "state.json"
	}
	r, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	var s *state.State
	s, err = state.ReadState(nil, r)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func init() {
	addDebugCommand("state", cmdDebugStateShortHelp, cmdDebugStateLongHelp, func() flags.Commander {
		return &cmdDebugState{}
	}, map[string]string{
		"change":  i18n.G("ID of the change to inspect"),
		"task":    i18n.G("ID of the task to inspect"),
		"dot":     i18n.G("Dot (graphviz) output"),
		"no-hold": i18n.G("Omit tasks in 'Hold' state in the change output"),
		"changes": i18n.G("List all changes"),
	}, nil)
}

func (c *cmdDebugState) showChanges(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	sort.Sort(byChangeID(changes))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, chg := range changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", chg.ID(), chg.Status().String(), formatTime(chg.SpawnTime()), formatTime(chg.ReadyTime()), chg.Kind(), chg.Summary())
	}
	w.Flush()

	return nil
}

func (c *cmdDebugState) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}

	// check valid combinations of args
	var cmds int
	if c.Changes {
		cmds++
	}
	if c.ChangeID != "" {
		cmds++
	}
	if c.TaskID != "" {
		cmds++
	}
	if cmds > 1 {
		return fmt.Errorf("cannot use --changes, --change= or --task= together")
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
