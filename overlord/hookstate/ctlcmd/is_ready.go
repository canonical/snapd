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

func init() {
	// TODO: temporarily disabled to prevent partial implementation in release
	//addCommand("is-ready", shortIsReadyHelp, longIsReadyHelp, func() command {
	//	return &isReadyCommand{}
	//})
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
