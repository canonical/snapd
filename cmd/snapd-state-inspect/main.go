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
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/snapcore/snapd/overlord/state"

	"github.com/jessevdk/go-flags"
)

// commandline args
var opts struct {
	//Changes bool `long:"changes" description:"Show changes"`
	ByReadyTime bool `long:"by-ready" description:"Sort by Ready time"`
}

func loadState(path string) (*state.State, error) {
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

type byChangeID []*state.Change

func (a byChangeID) Len() int           { return len(a) }
func (a byChangeID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byChangeID) Less(i, j int) bool { return a[i].ID() < a[j].ID() }

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func showChanges(w io.Writer, st *state.State) error {
	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	sort.Sort(byChangeID(changes))

	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, chg := range changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", chg.ID(), chg.Status().String(), formatTime(chg.SpawnTime()), formatTime(chg.ReadyTime()), chg.Kind(), chg.Summary())
	}
	return nil
}

func showTasks(w *tabwriter.Writer, st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}

	tasks := chg.Tasks()

	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, t := range tasks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", t.ID(), t.Status().String(), formatTime(t.SpawnTime()), formatTime(t.ReadyTime()), t.Kind(), t.Summary())
	}

	w.Flush()

	for _, t := range tasks {
		logs := t.Log()
		if len(logs) > 0 {

			fmt.Fprintf(os.Stdout, "-----\n")
			fmt.Fprintf(os.Stdout, "%s %s\n", t.ID(), t.Summary())
			for _, log := range logs {
				fmt.Fprintf(os.Stdout, "  %s\n", log)
			}
		}
	}

	return nil
}

func run() error {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	rest, err := parser.ParseArgs(os.Args[1:])
	if err != nil {
		return err
	}

	if len(rest) < 2 {
		return fmt.Errorf("invalid arguments, expected a command and state.json path")
	}

	what := rest[0]
	statePath := rest[len(rest)-1]
	st, err := loadState(statePath)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 5, 3, 2, ' ', 0)
	switch {
	case what == "changes":
		err = showChanges(w, st)
	case what == "tasks" || what == "change":
		if len(rest) != 3 {
			return fmt.Errorf("expected single change ID")
		}
		changeID := rest[1]
		err = showTasks(w, st, changeID)
	default:
		return fmt.Errorf("unknown command: %s", what)
	}

	w.Flush()

	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot inspect state: %s\n", err)
		os.Exit(1)
	}
}
