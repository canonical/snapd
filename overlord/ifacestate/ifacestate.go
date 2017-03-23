// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Package ifacestate implements the manager and state aspects
// responsible for the maintenance of interfaces the system.
package ifacestate

import (
	"fmt"

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// Connect returns a set of tasks for connecting an interface.
//
func Connect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflict(st, plugSnap, nil); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflict(st, slotSnap, nil); err != nil {
		return nil, err
	}

	// TODO: Store the intent-to-connect in the state so that we automatically
	// try to reconnect on reboot (reconnection can fail or can connect with
	// different parameters so we cannot store the actual connection details).

	// Create a series of tasks:
	//  - prepare-plug-<plug> hook
	//  - prepare-slot-<slot> hook
	//  - connect task
	//  - connect-slot-<slot> hook
	//  - connect-plug-<plug> hook
	// The tasks run in sequence (are serialized by WaitFor).
	// The prepare- hooks collect attributes via snapctl set.
	// 'snapctl set' can only modify own attributes (plug's attributes in the *-plug-* hook and
	// slot's attributes in the *-slot-* hook).
	// 'snapctl get' can read both slot's and plug's attributes.
	summary := fmt.Sprintf(i18n.G("Connect %s:%s to %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	connectInterface := st.NewTask("connect", summary)

	initialContext := make(map[string]interface{})
	initialContext["attrs-task"] = connectInterface.ID()

	plugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "prepare-plug-" + plugName,
		Optional: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), plugHookSetup.Hook, plugHookSetup.Snap)
	preparePlugConnection := hookstate.HookTask(st, summary, plugHookSetup, initialContext)

	slotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "prepare-slot-" + slotName,
		Optional: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), slotHookSetup.Hook, slotHookSetup.Snap)
	prepareSlotConnection := hookstate.HookTask(st, summary, slotHookSetup, initialContext)
	prepareSlotConnection.WaitFor(preparePlugConnection)

	connectInterface.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	connectInterface.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	if err := setInitialConnectAttributes(connectInterface, plugSnap, plugName, slotSnap, slotName); err != nil {
		return nil, err
	}
	connectInterface.WaitFor(prepareSlotConnection)

	connectSlotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "connect-slot-" + slotName,
		Optional: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectSlotHookSetup.Hook, connectSlotHookSetup.Snap)
	connectSlotConnection := hookstate.HookTask(st, summary, connectSlotHookSetup, initialContext)
	connectSlotConnection.WaitFor(connectInterface)

	connectPlugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "connect-plug-" + plugName,
		Optional: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectPlugHookSetup.Hook, connectPlugHookSetup.Snap)
	connectPlugConnection := hookstate.HookTask(st, summary, connectPlugHookSetup, initialContext)
	connectPlugConnection.WaitFor(connectSlotConnection)

	return state.NewTaskSet(preparePlugConnection, prepareSlotConnection, connectInterface, connectSlotConnection, connectPlugConnection), nil
}

func setInitialConnectAttributes(ts *state.Task, plugSnap string, plugName string, slotSnap string, slotName string) error {
	// Set initial interface attributes for the plug and slot snaps in connect task.
	var snapst snapstate.SnapState
	var err error

	st := ts.State()
	if err = snapstate.Get(st, plugSnap, &snapst); err != nil {
		return err
	}
	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	if plug, ok := snapInfo.Plugs[plugName]; ok {
		ts.Set("plug-attrs", plug.Attrs)
	} else {
		return fmt.Errorf("snap %q has no plug named %q", plugSnap, plugName)
	}

	if err = snapstate.Get(st, slotSnap, &snapst); err != nil {
		return err
	}
	snapInfo, err = snapst.CurrentInfo()
	if err != nil {
		return err
	}
	snap.AddImplicitSlots(snapInfo)
	if slot, ok := snapInfo.Slots[slotName]; ok {
		ts.Set("slot-attrs", slot.Attrs)
	} else {
		return fmt.Errorf("snap %q has no slot named %q", slotSnap, slotName)
	}

	return nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflict(st, plugSnap, nil); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflict(st, slotSnap, nil); err != nil {
		return nil, err
	}

	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	task := st.NewTask("disconnect", summary)
	task.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	task.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	return state.NewTaskSet(task), nil
}

// CheckInterfaces checks whether plugs and slots of snap are allowed for installation.
func CheckInterfaces(st *state.State, snapInfo *snap.Info) error {
	// XXX: AddImplicitSlots is really a brittle interface
	snap.AddImplicitSlots(snapInfo)

	if snapInfo.SnapID == "" {
		// no SnapID means --dangerous was given, so skip interface checks
		return nil
	}

	baseDecl, err := assertstate.BaseDeclaration(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot find base declaration: %v", err)
	}

	snapDecl, err := assertstate.SnapDeclaration(st, snapInfo.SnapID)
	if err != nil {
		return fmt.Errorf("cannot find snap declaration for %q: %v", snapInfo.Name(), err)
	}

	ic := policy.InstallCandidate{
		Snap:            snapInfo,
		SnapDeclaration: snapDecl,
		BaseDeclaration: baseDecl,
	}

	return ic.Check()
}

func init() {
	// hook interface checks into snapstate installation logic
	snapstate.AddCheckSnapCallback(func(st *state.State, snapInfo, _ *snap.Info, _ snapstate.Flags) error {
		return CheckInterfaces(st, snapInfo)
	})
}
