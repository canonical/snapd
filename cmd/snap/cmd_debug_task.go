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

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type cmdDebugTask struct {
	baseOfflineDebugCommand

	TaskID string `long:"task-id" required:"yes"`
}

var shortDebugTaskHelp = i18n.G("Show details of the given task from snapd state file.")
var longDebugTaskHelp = i18n.G("Show details of the given task from snapd state file, bypassing snapd API.")

func init() {
	addDebugCommand("task", shortDebugTaskHelp, longDebugTaskHelp, func() flags.Commander {
		return &cmdDebugTask{}
	}, map[string]string{"task-id": i18n.G("ID of the task to inspect")}, nil)
}

func (c *cmdDebugTask) showTask(st *state.State, taskID string) error {
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
	fmt.Fprintf(Stdout, "id: %s\nkind: %s\nsummary: %s\nstatus: %s\n\n", taskID, task.Kind(), task.Summary(), task.Status().String())
	log := task.Log()
	if len(log) > 0 {
		fmt.Fprintf(Stdout, "log:\n")
		for _, msg := range log {
			if err := wrapLine(Stdout, []rune(msg), "  ", termWidth); err != nil {
				break
			}
		}
		fmt.Fprintln(Stdout)
	}

	fmt.Fprintf(Stdout, "tasks waiting for %s:\n", taskID)
	for _, ht := range task.HaltTasks() {
		fmt.Fprintf(Stdout, "  %s (%s)\n", ht.Kind(), ht.ID())
	}

	return nil
}

func (c *cmdDebugTask) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}
	return c.showTask(st, c.TaskID)
}
