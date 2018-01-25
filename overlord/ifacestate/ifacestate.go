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
	"sync"
	"time"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func noConflictOnConnectTasks(chg *state.Change) func(task *state.Task) bool {
	checkConflict := func(task *state.Task) bool {
		if chg != nil && task.Change() != nil {
			// if same change, then return false (no conflict)
			return chg.ID() != task.Change().ID()
		}
		return task.Kind() != "connect" && task.Kind() != "disconnect"
	}
	return checkConflict
}

type changeConflictError struct {
	snapName string
}

func (e changeConflictError) Error() string {
	return fmt.Sprintf("snap %q has changes in progress", e.snapName)
}

var connectRetryTimeout = time.Second * 5

func checkConnectConflicts(st *state.State, change *state.Change, plugSnap, slotSnap string) error {
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		if chg.Kind() == "transition-ubuntu-core" {
			return fmt.Errorf("ubuntu-core to core transition in progress, no other changes allowed until this is done")
		}
	}

	for _, task := range st.Tasks() {
		k := task.Kind()
		chg := task.Change()

		// there is no conflict if same change
		if change != nil && chg != nil && change.ID() == chg.ID() {
			continue
		}

		if k == "connect" || k == "disconnect" {
			continue
		}

		// task already executed, no conflict
		if task.Status() == state.DoneStatus || task.Status() == state.ErrorStatus {
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(task)
		if err != nil {
			return fmt.Errorf("internal error: cannot obtain snap setup from task: %s", task.Summary())
		}

		snapName := snapsup.Name()
		// different snaps - no conflict
		if snapName != plugSnap && snapName != slotSnap {
			continue
		}

		switch k {
		case "unlink-snap":
			// snap getting removed - conflict
			var snap string
			if snapName == plugSnap {
				snap = plugSnap
			} else {
				snap = slotSnap
			}
			return &changeConflictError{snapName: snap}
		case "link-snap":
			// snap getting installed/refreshed - temporary conflict, retry later
			return &state.Retry{After: connectRetryTimeout}
		}
	}
	return nil
}

// AutoConnect returns a set of tasks for connection an interface as part of snap installation
// and auto-connect handling.
func AutoConnect(st *state.State, change *state.Change, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	return connect(st, change, plugSnap, plugName, slotSnap, slotName, true)
}

// Connect returns a set of tasks for connecting an interface.
//
func Connect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	return connect(st, nil, plugSnap, plugName, slotSnap, slotName, false)
}

func connect(st *state.State, change *state.Change, plugSnap, plugName, slotSnap, slotName string, autoConnect bool) (*state.TaskSet, error) {
	if err := checkConnectConflicts(st, change, plugSnap, slotSnap); err != nil {
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
	connectInterface.Set("auto", autoConnect)

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
	addImplicitSlots(snapInfo)
	if slot, ok := snapInfo.Slots[slotName]; ok {
		ts.Set("slot-attrs", slot.Attrs)
	} else {
		return fmt.Errorf("snap %q has no slot named %q", slotSnap, slotName)
	}

	return nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflict(st, plugSnap, noConflictOnConnectTasks(nil), nil); err != nil {
		return nil, err
	}
	if err := snapstate.CheckChangeConflict(st, slotSnap, noConflictOnConnectTasks(nil), nil); err != nil {
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
	// XXX: addImplicitSlots is really a brittle interface
	addImplicitSlots(snapInfo)

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

var once sync.Once

func delayedCrossMgrInit() {
	// hook interface checks into snapstate installation logic
	once.Do(func() {
		snapstate.AddCheckSnapCallback(func(st *state.State, snapInfo, _ *snap.Info, _ snapstate.Flags) error {
			return CheckInterfaces(st, snapInfo)
		})
	})
}
