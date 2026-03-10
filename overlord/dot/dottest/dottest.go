// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// Package dottest provides helpers for exporting change graphs in tests.
package dottest

import (
	"flag"
	"fmt"
	"os/exec"

	"github.com/snapcore/snapd/overlord/dot"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/check.v1"
)

var exportChangeGraphs = flag.Bool("snapd.export-change-graphs", false, "export change graphs during tests")
var openChangeGraphs = flag.Bool("snapd.open-change-graphs", false, "open exported change graphs during tests (implies -snapd.export-change-graphs)")

// RegisterChangeExporter integrates change graph creation with a test suite.
// Changes are exported as graphs as they enter key states by the task runner.
// Additionally, a function is returned that should be called during test
// cleanup. This function ensures that any changes that have not been executed
// are exported. Additionally, any tasks that are not associated with a change
// are grouped together and exported as a graph.
//
// The flags -snapd.export-change-graphs and -snapd.open-change-graphs control
// the behavior of this function.
func RegisterChangeExporter(c *check.C, st *state.State) func() {
	if !*exportChangeGraphs && !*openChangeGraphs {
		return func() {}
	}

	st.Lock()
	defer st.Unlock()

	show := map[state.Status]bool{
		state.DefaultStatus: true,
		state.WaitStatus:    true,
		state.ErrorStatus:   true,
		state.AbortStatus:   true,
		state.DoneStatus:    true,
		state.UndoneStatus:  true,
	}

	exported := make(map[string]bool)
	id := st.AddChangeStatusChangedHandler(func(chg *state.Change, old, new state.Status) {
		if exported[chg.ID()] && !show[new] {
			return
		}

		exported[chg.ID()] = true
		export(c, chg)
	})

	return func() {
		st.Lock()
		defer st.Unlock()

		st.RemoveChangeStatusChangedHandler(id)

		for _, chg := range st.Changes() {
			if exported[chg.ID()] {
				continue
			}

			export(c, chg)
		}

		exportUnlinkedChanges(c, st)
	}
}

func exportUnlinkedChanges(c *check.C, st *state.State) {
	tasks := st.AllTasksForTests()
	withoutChange := make([]*state.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Change() != nil {
			continue
		}
		withoutChange = append(withoutChange, t)
	}
	if len(withoutChange) == 0 {
		return
	}

	// since not all tests end up adding their tasks to a change, we group all
	// of the change-less tasks into a fake change to make this helper useful in
	// those contexts.
	chg := st.NewChange("tasks w/o change", c.TestName())
	for _, t := range withoutChange {
		chg.AddTask(t)
	}

	export(c, chg)
}

func export(c *check.C, chg *state.Change) {
	g, err := dot.NewChangeGraph(chg, fmt.Sprintf("%s - %s", chg.Status(), c.TestName()))
	c.Assert(err, check.IsNil)

	graphPath, err := g.Export()
	if err != nil {
		c.Logf("cannot export %q graph: %v", chg.Kind(), err)
		return
	}

	fmt.Printf("%s - %s - %q => %s\n", chg.Kind(), chg.Status(), c.TestName(), graphPath)

	if !*openChangeGraphs {
		return
	}
	if err := exec.Command("xdg-open", graphPath).Run(); err != nil {
		c.Logf("cannot open %q graph: %v", chg.Kind(), err)
	}
}
