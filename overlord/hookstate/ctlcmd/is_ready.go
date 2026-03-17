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
	changeID string
}

var shortIsReadyHelp = i18n.G(`Return the status of the associated change id.`)
var longIsReadyHelp = i18n.G(`
The is-ready command is used to query the status of a change id associated with
snapctl commands running in asynchronous mode. It returns success if the change
is ready, and failure otherwise.

$ snapctl is-ready <change-id>
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

	c.changeID = args[0]

	status, err := getChangeStatus(ctx, c.changeID)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "%s", status)

	if status == state.DoneStatus.String() {
		return nil
	}

	return &UnsuccessfulError{ExitCode: 1}
}
