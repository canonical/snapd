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
	"text/tabwriter"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type baseOfflineDebugCommand struct {
	st *state.State

	Positional struct {
		StateFilePath string `positional-args:"yes" positional-arg-name:"<state-file>"`
	} `positional-args:"yes"`
}

type cmdDebugChanges struct {
	baseOfflineDebugCommand
}

var shortDebugChangesHelp = i18n.G("Show all changes from a snapd state file.")
var longDebugChangesHelp = i18n.G("Show all changes from a snapd state file, bypassing snapd API.")

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
	addDebugCommand("changes", shortDebugChangesHelp, longDebugChangesHelp, func() flags.Commander {
		return &cmdDebugChanges{}
	}, nil, nil)
}

func (c *cmdDebugChanges) showChanges(st *state.State) error {
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

func (c *cmdDebugChanges) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}

	return c.showChanges(st)
}
