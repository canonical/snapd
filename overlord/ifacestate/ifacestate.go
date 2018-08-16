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

var noConflictOnConnectTasks = func(task *state.Task) bool {
	// TODO: reconsider this check with regard to interface hooks
	return task.Kind() != "connect" && task.Kind() != "disconnect"
}

var connectRetryTimeout = time.Second * 5

// ErrAlreadyConnected describes the error that occurs when attempting to connect already connected interface.
type ErrAlreadyConnected struct {
	Connection interfaces.ConnRef
}

func (e ErrAlreadyConnected) Error() string {
	return fmt.Sprintf("already connected: %q", e.Connection.ID())
}

// findSymmetricAutoconnectTask checks if there is another auto-connect task affecting same snap because of plug/slot.
func findSymmetricAutoconnectTask(st *state.State, plugSnap, slotSnap string, installTask *state.Task) (bool, error) {
	snapsup, err := snapstate.TaskSnapSetup(installTask)
	if err != nil {
		return false, fmt.Errorf("internal error: cannot obtain snap setup from task: %s", installTask.Summary())
	}
	installedSnap := snapsup.InstanceName()

	// if we find any auto-connect task that's not ready and is affecting our snap, return true to indicate that
	// it should be ignored (we shouldn't create connect tasks for it)
	for _, task := range st.Tasks() {
		if !task.Status().Ready() && task.ID() != installTask.ID() && task.Kind() == "auto-connect" {
			snapsup, err := snapstate.TaskSnapSetup(task)
			if err != nil {
				return false, fmt.Errorf("internal error: cannot obtain snap setup from task: %s", task.Summary())
			}
			otherSnap := snapsup.InstanceName()

			if (otherSnap == plugSnap && installedSnap == slotSnap) || (otherSnap == slotSnap && installedSnap == plugSnap) {
				return true, nil
			}
		}
	}
	return false, nil
}

// Connect returns a set of tasks for connecting an interface.
//
func Connect(st *state.State, plugSnap, plugName, slotSnap, slotName string) (*state.TaskSet, error) {
	if err := snapstate.CheckChangeConflictMany(st, []string{plugSnap, slotSnap}, ""); err != nil {
		return nil, err
	}

	return connect(st, plugSnap, plugName, slotSnap, slotName, nil)
}

func connect(st *state.State, plugSnap, plugName, slotSnap, slotName string, flags []string) (*state.TaskSet, error) {
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

	// check if the connection already exists
	conns, err := getConns(st)
	if err != nil {
		return nil, err
	}
	connRef := interfaces.ConnRef{PlugRef: interfaces.PlugRef{Snap: plugSnap, Name: plugName}, SlotRef: interfaces.SlotRef{Snap: slotSnap, Name: slotName}}
	if conn, ok := conns[connRef.ID()]; ok && conn.Undesired == false && conn.HotplugRemoved == false {
		return nil, &ErrAlreadyConnected{Connection: connRef}
	}

	plugStatic, slotStatic, err := initialConnectAttributes(st, plugSnap, plugName, slotSnap, slotName)
	if err != nil {
		return nil, err
	}

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
	undoPrepPlugHookSetup := &hookstate.HookSetup{
		Snap:        plugSnap,
		Hook:        "unprepare-plug-" + plugName,
		Optional:    true,
		IgnoreError: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), plugHookSetup.Hook, plugHookSetup.Snap)
	preparePlugConnection := hookstate.HookTaskWithUndo(st, summary, plugHookSetup, undoPrepPlugHookSetup, initialContext)

	slotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "prepare-slot-" + slotName,
		Optional: true,
	}
	undoPrepSlotHookSetup := &hookstate.HookSetup{
		Snap:        slotSnap,
		Hook:        "unprepare-slot-" + slotName,
		Optional:    true,
		IgnoreError: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), slotHookSetup.Hook, slotHookSetup.Snap)
	prepareSlotConnection := hookstate.HookTaskWithUndo(st, summary, slotHookSetup, undoPrepSlotHookSetup, initialContext)
	prepareSlotConnection.WaitFor(preparePlugConnection)

	connectInterface.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	connectInterface.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})
	for _, flag := range flags {
		connectInterface.Set(flag, true)
	}

	// Expose a copy of all plug and slot attributes coming from yaml to interface hooks. The hooks will be able
	// to modify them but all attributes will be checked against assertions after the hooks are run.
	emptyDynamicAttrs := map[string]interface{}{}
	connectInterface.Set("plug-static", plugStatic)
	connectInterface.Set("slot-static", slotStatic)
	connectInterface.Set("plug-dynamic", emptyDynamicAttrs)
	connectInterface.Set("slot-dynamic", emptyDynamicAttrs)

	connectInterface.WaitFor(prepareSlotConnection)

	connectSlotHookSetup := &hookstate.HookSetup{
		Snap:     slotSnap,
		Hook:     "connect-slot-" + slotName,
		Optional: true,
	}
	undoConnectSlotHookSetup := &hookstate.HookSetup{
		Snap:        slotSnap,
		Hook:        "disconnect-slot-" + slotName,
		Optional:    true,
		IgnoreError: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectSlotHookSetup.Hook, connectSlotHookSetup.Snap)
	connectSlotConnection := hookstate.HookTaskWithUndo(st, summary, connectSlotHookSetup, undoConnectSlotHookSetup, initialContext)
	connectSlotConnection.WaitFor(connectInterface)

	connectPlugHookSetup := &hookstate.HookSetup{
		Snap:     plugSnap,
		Hook:     "connect-plug-" + plugName,
		Optional: true,
	}
	undoConnectPlugHookSetup := &hookstate.HookSetup{
		Snap:        plugSnap,
		Hook:        "disconnect-plug-" + plugName,
		Optional:    true,
		IgnoreError: true,
	}

	summary = fmt.Sprintf(i18n.G("Run hook %s of snap %q"), connectPlugHookSetup.Hook, connectPlugHookSetup.Snap)
	connectPlugConnection := hookstate.HookTaskWithUndo(st, summary, connectPlugHookSetup, undoConnectPlugHookSetup, initialContext)
	connectPlugConnection.WaitFor(connectSlotConnection)

	return state.NewTaskSet(preparePlugConnection, prepareSlotConnection, connectInterface, connectSlotConnection, connectPlugConnection), nil
}

func initialConnectAttributes(st *state.State, plugSnap string, plugName string, slotSnap string, slotName string) (plugStatic, slotStatic map[string]interface{}, err error) {
	var plugSnapst snapstate.SnapState

	if err = snapstate.Get(st, plugSnap, &plugSnapst); err != nil {
		return nil, nil, err
	}
	snapInfo, err := plugSnapst.CurrentInfo()
	if err != nil {
		return nil, nil, err
	}

	plug, ok := snapInfo.Plugs[plugName]
	if !ok {
		return nil, nil, fmt.Errorf("snap %q has no plug named %q", plugSnap, plugName)
	}

	var slotSnapst snapstate.SnapState

	if err = snapstate.Get(st, slotSnap, &slotSnapst); err != nil {
		return nil, nil, err
	}
	snapInfo, err = slotSnapst.CurrentInfo()
	if err != nil {
		return nil, nil, err
	}

	addImplicitSlots(snapInfo)
	slot, ok := snapInfo.Slots[slotName]
	if !ok {
		return nil, nil, fmt.Errorf("snap %q has no slot named %q", slotSnap, slotName)
	}

	return plug.Attrs, slot.Attrs, nil
}

// Disconnect returns a set of tasks for  disconnecting an interface.
func Disconnect(st *state.State, conn *interfaces.Connection) (*state.TaskSet, error) {
	plugSnap := conn.Plug.Snap().InstanceName()
	slotSnap := conn.Slot.Snap().InstanceName()
	if err := snapstate.CheckChangeConflictMany(st, []string{plugSnap, slotSnap}, ""); err != nil {
		return nil, err
	}

	return disconnectTasks(st, conn, disconnectOpts{})
}

type disconnectOpts struct {
	AutoDisconnect bool
}

// disconnectTasks creates a set of tasks for disconnect, including hooks, but does not do any conflict checking.
func disconnectTasks(st *state.State, conn *interfaces.Connection, flags disconnectOpts) (*state.TaskSet, error) {
	plugSnap := conn.Plug.Snap().InstanceName()
	slotSnap := conn.Slot.Snap().InstanceName()
	plugName := conn.Plug.Name()
	slotName := conn.Slot.Name()

	var plugSnapst, slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotSnap, &slotSnapst); err != nil {
		return nil, err
	}
	if err := snapstate.Get(st, plugSnap, &plugSnapst); err != nil {
		return nil, err
	}

	summary := fmt.Sprintf(i18n.G("Disconnect %s:%s from %s:%s"),
		plugSnap, plugName, slotSnap, slotName)
	disconnectTask := st.NewTask("disconnect", summary)
	disconnectTask.Set("slot", interfaces.SlotRef{Snap: slotSnap, Name: slotName})
	disconnectTask.Set("plug", interfaces.PlugRef{Snap: plugSnap, Name: plugName})

	disconnectTask.Set("slot-static", conn.Slot.StaticAttrs())
	disconnectTask.Set("slot-dynamic", conn.Slot.DynamicAttrs())
	disconnectTask.Set("plug-static", conn.Plug.StaticAttrs())
	disconnectTask.Set("plug-dynamic", conn.Plug.DynamicAttrs())

	if flags.AutoDisconnect {
		disconnectTask.Set("auto-disconnect", true)
	}

	ts := state.NewTaskSet()

	initialContext := make(map[string]interface{})
	initialContext["attrs-task"] = disconnectTask.ID()

	var disconnectSlot *state.Task

	// only run slot hooks if slotSnap is active
	if slotSnapst.Active {
		disconnectSlotHookSetup := &hookstate.HookSetup{
			Snap:     slotSnap,
			Hook:     "disconnect-slot-" + slotName,
			Optional: true,
		}
		undoDisconnectSlotHookSetup := &hookstate.HookSetup{
			Snap:     slotSnap,
			Hook:     "connect-slot-" + slotName,
			Optional: true,
		}

		summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), disconnectSlotHookSetup.Hook, disconnectSlotHookSetup.Snap)
		disconnectSlot = hookstate.HookTaskWithUndo(st, summary, disconnectSlotHookSetup, undoDisconnectSlotHookSetup, initialContext)

		ts.AddTask(disconnectSlot)
		disconnectTask.WaitFor(disconnectSlot)
	}

	// only run plug hooks if plugSnap is active
	if plugSnapst.Active {
		disconnectPlugHookSetup := &hookstate.HookSetup{
			Snap:     plugSnap,
			Hook:     "disconnect-plug-" + plugName,
			Optional: true,
		}
		undoDisconnectPlugHookSetup := &hookstate.HookSetup{
			Snap:     plugSnap,
			Hook:     "connect-plug-" + plugName,
			Optional: true,
		}

		summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), disconnectPlugHookSetup.Hook, disconnectPlugHookSetup.Snap)
		disconnectPlug := hookstate.HookTaskWithUndo(st, summary, disconnectPlugHookSetup, undoDisconnectPlugHookSetup, initialContext)
		disconnectPlug.WaitAll(ts)

		if disconnectSlot != nil {
			disconnectPlug.WaitFor(disconnectSlot)
		}

		ts.AddTask(disconnectPlug)
		disconnectTask.WaitFor(disconnectPlug)
	}

	ts.AddTask(disconnectTask)
	return ts, nil
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
		return fmt.Errorf("cannot find snap declaration for %q: %v", snapInfo.InstanceName(), err)
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
	once.Do(func() {
		// hook interface checks into snapstate installation logic

		snapstate.AddCheckSnapCallback(func(st *state.State, snapInfo, _ *snap.Info, _ snapstate.Flags) error {
			return CheckInterfaces(st, snapInfo)
		})

		// hook into conflict checks mechanisms
		snapstate.AddAffectedSnapsByKind("connect", connectDisconnectAffectedSnaps)
		snapstate.AddAffectedSnapsByKind("disconnect", connectDisconnectAffectedSnaps)
	})
}
