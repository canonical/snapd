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

// ExportChangeGraphs creates change graphs for tasks in the state when either
// -snapd.export-change-graphs or -snapd.open-change-graphs is set. if
// -snapd.open-change-graphs is set, then the graphs are opened.
func ExportChangeGraphs(c *check.C, st *state.State) {
	if !*exportChangeGraphs && !*openChangeGraphs {
		return
	}

	st.Lock()
	defer st.Unlock()

	tasks := st.AllTasksForTests()
	unlinked := make([]*state.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Change() != nil {
			continue
		}
		unlinked = append(unlinked, t)
	}

	// TODO: for now, we're just handling tasks that aren't attached to a
	// change.
	if len(unlinked) == 0 {
		return
	}

	chg := st.NewChange("unlinked-tasks", c.TestName())
	for _, t := range unlinked {
		chg.AddTask(t)
	}

	g, err := dot.NewChangeGraph(chg, c.TestName())
	c.Assert(err, check.IsNil)

	graphPath, err := g.Export()
	if err != nil {
		c.Logf("cannot export change graph: %v", err)
		return
	}

	fmt.Printf("%s => %s\n", c.TestName(), graphPath)

	if !*openChangeGraphs {
		return
	}
	if err := exec.Command("xdg-open", graphPath).Run(); err != nil {
		c.Logf("cannot open change graph: %v", err)
	}
}
