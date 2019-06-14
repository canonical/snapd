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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type taskCommand struct {
	baseCommand

	TaskID string `long:"task-id" required:"yes"`
}

var shortTaskHelp = i18n.G("The task command prints detailed information about the given task.")

func init() {
	addCommand("task", shortTaskHelp, "", func() command {
		return &taskCommand{}
	})
}

func (c *taskCommand) showTask(st *state.State, taskID string) error {
	st.Lock()
	defer st.Unlock()

	task := st.Task(taskID)
	if task == nil {
		return fmt.Errorf("no such task: %s", taskID)
	}

	fmt.Fprintf(c.stdOut, "id: %s\nkind: %s\nsummary: %s\nstatus: %s\n\n", taskID, task.Kind(), task.Summary(), task.Status().String())
	log := task.Log()
	if len(log) > 0 {
		fmt.Fprintf(c.stdOut, "log:\n")
		for _, msg := range log {
			fmt.Fprintf(c.stdOut, "  %s\n", msg)
		}
		fmt.Fprintln(c.stdOut)
	}

	fmt.Fprintf(c.stdOut, "tasks waiting for %s:\n", taskID)
	for _, ht := range task.HaltTasks() {
		fmt.Fprintf(c.stdOut, "  %s (%s)\n", ht.Kind(), ht.ID())
	}

	return nil
}

func (c *taskCommand) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}
	return c.showTask(st, c.TaskID)
}
