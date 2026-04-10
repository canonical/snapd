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

package ctlcmd

import (
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type tasksCommand struct {
	baseCommand
	json bool
}

var shortTasksHelp = i18n.G(`Return a list of information associated with all change-ids.`)
var longTasksHelp = i18n.G(`
The tasks command is used to query the status of all change ids associated with
snapctl commands running in asynchronous mode.

$ snapctl tasks [--json]
  0: successfully reported change information, regardless of state of change
  1: any error (invalid change ID, permissions error)
stdout: table of tasks, mirroring "snap tasks <change-id>" output
stderr: empty for exit code 0. Contains relevant errors for exit code 1.
`)

func init() {
	addCommand("tasks", shortTasksHelp, longTasksHelp, func() command {
		return &tasksCommand{}
	})
}

func (c *tasksCommand) Execute(args []string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return fmt.Errorf("invalid number of arguments: expected 1, got %d", len(args))
	}

	c.json = c.flagSet.Lookup("json").Value.String() == "true"

	ready, err := isReady(ctx, c.changeID)

	if err != nil {
		fmt.Fprintf(c.stderr, err.Error())
		return &UnsuccessfulError{ExitCode: otherErrorExitCode}
	}

	if !ready.Ready() {
		return &UnsuccessfulError{ExitCode: changeNotReadyExitCode}
	}

	if ready != state.DoneStatus {
		return &UnsuccessfulError{ExitCode: changeUnsuccessfulExitCode}
	}

	return nil
}
