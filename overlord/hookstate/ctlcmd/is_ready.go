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

type isReadyCommand struct {
	baseCommand
}

const (
	changeReadyExitCode = iota
	changeNotReadyExitCode
	changeUnsuccessfulExitCode
	otherErrorExitCode
)

var shortIsReadyHelp = i18n.G(`Return the status of the associated change id.`)
var longIsReadyHelp = i18n.G(`
The is-ready command is used to query the status of change ids that are returned
by asynchronous snapctl commands.

$ snapctl is-ready <change-id>
  0: change completed successfully (Done)
  1: change is not ready
  2: change is ready but did not complete successfully (Undone, Error, Hold)
  3: other errors (invalid change id, permissions error)
stdout: empty, exit code conveys change readiness
stderr: empty for exit codes 0 and 1. Contains relevant errors for exit codes 2 and 3.
`)

func init() {
	addCommand("is-ready", shortIsReadyHelp, longIsReadyHelp, func() command {
		return &isReadyCommand{}
	})
}

func (c *isReadyCommand) Execute(args []string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return fmt.Errorf("invalid number of arguments: expected 1, got %d", len(args))
	}

	changeID := args[0]

	ready, err := isReady(ctx, changeID)

	if err != nil {
		fmt.Fprint(c.stderr, err.Error())
		return &UnsuccessfulError{ExitCode: otherErrorExitCode}
	}

	if !ready.Ready() {
		return &UnsuccessfulError{ExitCode: changeNotReadyExitCode}
	}

	if ready != state.DoneStatus {
		fmt.Fprintf(c.stderr, "change finished with status %s", ready)
		return &UnsuccessfulError{ExitCode: changeUnsuccessfulExitCode}
	}

	return nil
}
