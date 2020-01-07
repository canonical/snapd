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

package ctlcmd

import (
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate"
)

type isConnectedCommand struct {
	baseCommand

	Positional struct {
		PlugOrSlotSpec string `positional-args:"true" positional-arg-name:"<plug|slot>"`
	} `positional-args:"true" required:"true"`
}

var shortIsConnectedHelp = i18n.G(`Return success if the given plug or slot is connected, and failure otherwise`)
var longIsConnectedHelp = i18n.G(`
The is-connected command returns success if the given plug or slot of the
calling snap is connected, and failure otherwise.

$ snapctl is-connected plug
$ echo $?
1

Snaps can only query their own plugs and slots - snap name is implicit and
implied by the snapctl execution context.
`)

func init() {
	addCommand("is-connected", shortIsConnectedHelp, longIsConnectedHelp, func() command {
		return &isConnectedCommand{}
	})
}

func (c *isConnectedCommand) Execute(args []string) error {
	plugOrSlot := c.Positional.PlugOrSlotSpec

	context := c.context()
	if context == nil {
		return fmt.Errorf("cannot check connection status without a context")
	}

	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	// XXX should we check if plug/slot exists? We don't differentiate between
	// non-connected/not-existing at the moment.

	// snapName is the name of the snap executing snapctl command, it's
	// obtained from the context (ephemeral if run by apps, or full if run by
	// hooks). plug and slot names are unique within a snap, so there is no
	// ambiguity when matching.
	for refStr, connState := range conns {
		if connState.Undesired || connState.HotplugGone {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return fmt.Errorf("internal error: %s", err)
		}

		matchingPlug := connRef.PlugRef.Snap == snapName && connRef.PlugRef.Name == plugOrSlot
		matchingSlot := connRef.SlotRef.Snap == snapName && connRef.SlotRef.Name == plugOrSlot
		if matchingPlug || matchingSlot {
			return nil
		}
	}

	return &UnsuccessfulError{ExitCode: 1}
}
