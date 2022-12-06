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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
)

var cgroupSnapNameFromPid = cgroup.SnapNameFromPid

const (
	classicSnapCode = 10
	notASnapCode    = 11
)

type isConnectedCommand struct {
	baseCommand

	Positional struct {
		PlugOrSlotSpec string `positional-args:"true" positional-arg-name:"<plug|slot>"`
	} `positional-args:"true" required:"true"`
	Pid           int    `long:"pid" description:"Process ID for a plausibly connected process"`
	AppArmorLabel string `long:"apparmor-label" description:"AppArmor label for a plausibly connected process"`
}

var shortIsConnectedHelp = i18n.G(`Return success if the given plug or slot is connected`)
var longIsConnectedHelp = i18n.G(`
The is-connected command returns success if the given plug or slot of the
calling snap is connected, and failure otherwise.

$ snapctl is-connected plug
$ echo $?
1

Snaps can only query their own plugs and slots - snap name is implicit and
implied by the snapctl execution context.

The --pid and --aparmor-label options can be used to determine whether
a plug or slot is connected to the snap identified by the given
process ID or AppArmor label.  In this mode, additional failure exit
codes may be returned: 10 if the other snap is not connected but uses
classic confinement, or 11 if the other process is not snap confined.

The --pid and --apparmor-label options may only be used with slots of
interface type "pulseaudio", "audio-record", or "cups-control".
`)

func init() {
	addCommand("is-connected", shortIsConnectedHelp, longIsConnectedHelp, func() command {
		return &isConnectedCommand{}
	})
}

func isConnectedPidCheckAllowed(info *snap.Info, plugOrSlot string) bool {
	slot := info.Slots[plugOrSlot]
	if slot != nil {
		switch slot.Interface {
		case "pulseaudio", "audio-record", "cups-control":
			return true
		}
	}
	return false
}

func (c *isConnectedCommand) Execute(args []string) error {
	plugOrSlot := c.Positional.PlugOrSlotSpec

	context, err := c.ensureContext()
	if err != nil {
		return err
	}

	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	info, err := snapstate.CurrentInfo(st, snapName)
	if err != nil {
		return fmt.Errorf("internal error: cannot get snap info: %s", err)
	}

	// XXX: This will fail for implicit slots.  In practice, this
	// would only affect calls that used the "core" snap as
	// context.  That snap does not have any hooks using
	// is-connected, so the limitation is probably moot.
	if info.Plugs[plugOrSlot] == nil && info.Slots[plugOrSlot] == nil {
		return fmt.Errorf("snap %q has no plug or slot named %q", snapName, plugOrSlot)
	}

	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	var otherSnap *snap.Info
	if c.AppArmorLabel != "" {
		if !isConnectedPidCheckAllowed(info, plugOrSlot) {
			return fmt.Errorf("cannot use --apparmor-label check with %s:%s", snapName, plugOrSlot)
		}
		name, _, _, err := apparmor.DecodeLabel(c.AppArmorLabel)
		if err != nil {
			return &UnsuccessfulError{ExitCode: notASnapCode}
		}
		otherSnap, err = snapstate.CurrentInfo(st, name)
		if err != nil {
			return fmt.Errorf("internal error: cannot get snap info for AppArmor label %q: %s", c.AppArmorLabel, err)
		}
	} else if c.Pid != 0 {
		if !isConnectedPidCheckAllowed(info, plugOrSlot) {
			return fmt.Errorf("cannot use --pid check with %s:%s", snapName, plugOrSlot)
		}
		name, err := cgroupSnapNameFromPid(c.Pid)
		if err != nil {
			// Indicate that this pid is not a snap
			return &UnsuccessfulError{ExitCode: notASnapCode}
		}
		otherSnap, err = snapstate.CurrentInfo(st, name)
		if err != nil {
			return fmt.Errorf("internal error: cannot get snap info for pid %d: %s", c.Pid, err)
		}
	}

	// snapName is the name of the snap executing snapctl command, it's
	// obtained from the context (ephemeral if run by apps, or full if run by
	// hooks). plug and slot names are unique within a snap, so there is no
	// ambiguity when matching.
	for refStr, connState := range conns {
		if !connState.Active() {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return fmt.Errorf("internal error: %s", err)
		}

		matchingPlug := connRef.PlugRef.Snap == snapName && connRef.PlugRef.Name == plugOrSlot
		matchingSlot := connRef.SlotRef.Snap == snapName && connRef.SlotRef.Name == plugOrSlot
		if otherSnap != nil {
			if matchingPlug && connRef.SlotRef.Snap == otherSnap.InstanceName() || matchingSlot && connRef.PlugRef.Snap == otherSnap.InstanceName() {
				return nil
			}
		} else {
			if matchingPlug || matchingSlot {
				return nil
			}
		}
	}

	if otherSnap != nil && otherSnap.Confinement == snap.ClassicConfinement {
		return &UnsuccessfulError{ExitCode: classicSnapCode}
	}

	return &UnsuccessfulError{ExitCode: 1}
}
