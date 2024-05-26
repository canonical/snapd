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
	"sort"

	"github.com/ddkwork/golibrary/mylog"
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
	} `positional-args:"true"`
	Pid           int    `long:"pid" description:"Process ID for a plausibly connected process"`
	AppArmorLabel string `long:"apparmor-label" description:"AppArmor label for a plausibly connected process"`
	List          bool   `long:"list" description:"List all connected plugs and slots"`
}

var (
	shortIsConnectedHelp = i18n.G(`Return success if the given plug or slot is connected`)
	longIsConnectedHelp  = i18n.G(`
The is-connected command returns success if the given plug or slot of the
calling snap is connected, and failure otherwise.

$ snapctl is-connected plug
$ echo $?
1

Snaps can only query their own plugs and slots - snap name is implicit and
implied by the snapctl execution context.

The --list option lists all connected plugs and slots.

The --pid and --aparmor-label options can be used to determine whether
a plug or slot is connected to the snap identified by the given
process ID or AppArmor label.  In this mode, additional failure exit
codes may be returned: 10 if the other snap is not connected but uses
classic confinement, or 11 if the other process is not snap confined.

The --pid and --apparmor-label options may only be used with slots of
interface type "pulseaudio", "audio-record", or "cups-control".
`)
)

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

	if plugOrSlot != "" && c.List {
		return fmt.Errorf("cannot specify both a plug/slot name and --list")
	}
	if plugOrSlot == "" && !c.List {
		return fmt.Errorf("must specify either a plug/slot name or --list")
	}

	context := mylog.Check2(c.ensureContext())

	snapName := context.InstanceName()

	st := context.State()
	st.Lock()
	defer st.Unlock()

	info := mylog.Check2(snapstate.CurrentInfo(st, snapName))

	// XXX: This will fail for implicit slots.  In practice, this
	// would only affect calls that used the "core" snap as
	// context.  That snap does not have any hooks using
	// is-connected, so the limitation is probably moot.
	if plugOrSlot != "" && info.Plugs[plugOrSlot] == nil && info.Slots[plugOrSlot] == nil {
		return fmt.Errorf("snap %q has no plug or slot named %q", snapName, plugOrSlot)
	}

	conns := mylog.Check2(ifacestate.ConnectionStates(st))

	var otherSnap *snap.Info
	if c.AppArmorLabel != "" {
		if plugOrSlot == "" {
			return fmt.Errorf("cannot use --apparmor-label check without plug/slot")
		}
		if !isConnectedPidCheckAllowed(info, plugOrSlot) {
			return fmt.Errorf("cannot use --apparmor-label check with %s:%s", snapName, plugOrSlot)
		}
		name, _, _ := mylog.Check4(apparmor.DecodeLabel(c.AppArmorLabel))

		otherSnap = mylog.Check2(snapstate.CurrentInfo(st, name))

	} else if c.Pid != 0 {
		if plugOrSlot == "" {
			return fmt.Errorf("cannot use --pid check without plug/slot")
		}
		if !isConnectedPidCheckAllowed(info, plugOrSlot) {
			return fmt.Errorf("cannot use --pid check with %s:%s", snapName, plugOrSlot)
		}
		name := mylog.Check2(cgroupSnapNameFromPid(c.Pid))

		// Indicate that this pid is not a snap

		otherSnap = mylog.Check2(snapstate.CurrentInfo(st, name))

	}

	if c.List {
		nameSet := make(map[string]struct{})
		for refStr, connState := range conns {
			if !connState.Active() {
				continue
			}
			connRef := mylog.Check2(interfaces.ParseConnRef(refStr))

			if connRef.PlugRef.Snap == snapName {
				nameSet[connRef.PlugRef.Name] = struct{}{}
			}
			if connRef.SlotRef.Snap == snapName {
				nameSet[connRef.SlotRef.Name] = struct{}{}
			}
		}

		names := make([]string, 0, len(nameSet))
		for name := range nameSet {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintln(c.stdout, name)
		}

		return nil
	}

	// snapName is the name of the snap executing snapctl command, it's
	// obtained from the context (ephemeral if run by apps, or full if run by
	// hooks). plug and slot names are unique within a snap, so there is no
	// ambiguity when matching.
	for refStr, connState := range conns {
		if !connState.Active() {
			continue
		}
		connRef := mylog.Check2(interfaces.ParseConnRef(refStr))

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
