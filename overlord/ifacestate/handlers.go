// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package ifacestate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// confinementOptions returns interfaces.ConfinementOptions from snapstate.Flags.
func confinementOptions(flags snapstate.Flags) interfaces.ConfinementOptions {
	return interfaces.ConfinementOptions{
		DevMode:  flags.DevMode,
		JailMode: flags.JailMode,
		Classic:  flags.Classic,
	}
}

func (m *InterfaceManager) setupAffectedSnaps(task *state.Task, affectingSnap string, affectedSnaps []string) error {
	st := task.State()

	// Setup security of the affected snaps.
	for _, affectedSnapName := range affectedSnaps {
		// the snap that triggered the change needs to be skipped
		if affectedSnapName == affectingSnap {
			continue
		}
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, affectedSnapName, &snapst); err != nil {
			task.Errorf("skipping security profiles setup for snap %q when handling snap %q: %v", affectedSnapName, affectingSnap, err)
			continue
		}
		affectedSnapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		addImplicitSlots(affectedSnapInfo)
		opts := confinementOptions(snapst.Flags)
		if err := m.setupSnapSecurity(task, affectedSnapInfo, opts); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) doSetupProfiles(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapInfo, err := snap.ReadInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	// TODO: this whole bit seems maybe that it should belong (largely) to a snapstate helper
	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && err != state.ErrNoState {
		return err
	}
	if corePhase2 {
		if snapInfo.Type != snap.TypeOS {
			// not core, nothing to do
			return nil
		}
		if task.State().Restarting() {
			// don't continue until we are in the restarted snapd
			task.Logf("Waiting for restart...")
			return &state.Retry{}
		}
		// if not on classic check there was no rollback
		if !release.OnClassic {
			// TODO: double check that we really rebooted
			// otherwise this could be just a spurious restart
			// of snapd
			name, rev, err := snapstate.CurrentBootNameAndRevision(snap.TypeOS)
			if err == snapstate.ErrBootNameAndRevisionAgain {
				return &state.Retry{After: 5 * time.Second}
			}
			if err != nil {
				return err
			}
			if snapsup.Name() != name || snapInfo.Revision != rev {
				return fmt.Errorf("cannot finish core installation, there was a rollback across reboot")
			}
		}
	}

	opts := confinementOptions(snapsup.Flags)
	return m.setupProfilesForSnap(task, tomb, snapInfo, opts)
}

func (m *InterfaceManager) setupProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapInfo *snap.Info, opts interfaces.ConfinementOptions) error {
	addImplicitSlots(snapInfo)
	snapName := snapInfo.Name()

	// The snap may have been updated so perform the following operation to
	// ensure that we are always working on the correct state:
	//
	// - disconnect all connections to/from the given snap
	//   - remembering the snaps that were affected by this operation
	// - remove the (old) snap from the interfaces repository
	// - add the (new) snap to the interfaces repository
	// - restore connections based on what is kept in the state
	//   - if a connection cannot be restored then remove it from the state
	// - setup the security of all the affected snaps
	disconnectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	// XXX: what about snap renames? We should remove the old name (or switch
	// to IDs in the interfaces repository)
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}
	if err := m.repo.AddSnap(snapInfo); err != nil {
		return err
	}
	if len(snapInfo.BadInterfaces) > 0 {
		task.Logf("%s", snap.BadInterfacesSummary(snapInfo))
	}

	reconnectedSnaps, err := m.reloadConnections(snapName)
	if err != nil {
		return err
	}
	if err := m.setupSnapSecurity(task, snapInfo, opts); err != nil {
		return err
	}
	affectedSet := make(map[string]bool)
	for _, name := range disconnectedSnaps {
		affectedSet[name] = true
	}
	for _, name := range reconnectedSnaps {
		affectedSet[name] = true
	}
	// The principal snap was already handled above.
	delete(affectedSet, snapInfo.Name())
	affectedSnaps := make([]string, 0, len(affectedSet))
	for name := range affectedSet {
		affectedSnaps = append(affectedSnaps, name)
	}
	sort.Strings(affectedSnaps)
	return m.setupAffectedSnaps(task, snapName, affectedSnaps)
}

func (m *InterfaceManager) doRemoveProfiles(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	// Get SnapSetup for this snap. This is gives us the name of the snap.
	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapName := snapSetup.Name()

	return m.removeProfilesForSnap(task, tomb, snapName)
}

func (m *InterfaceManager) removeProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapName string) error {
	// Disconnect the snap entirely.
	// This is required to remove the snap from the interface repository.
	// The returned list of affected snaps will need to have its security setup
	// to reflect the change.
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	if err := m.setupAffectedSnaps(task, snapName, affectedSnaps); err != nil {
		return err
	}

	// Remove the snap from the interface repository.
	// This discards all the plugs and slots belonging to that snap.
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}

	// Remove security artefacts of the snap.
	if err := m.removeSnapSecurity(task, snapName); err != nil {
		return err
	}

	return nil
}

func (m *InterfaceManager) undoSetupProfiles(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && err != state.ErrNoState {
		return err
	}
	if corePhase2 {
		// let the first setup-profiles deal with this
		return nil
	}

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	// Get the name from SnapSetup and use it to find the current SideInfo
	// about the snap, if there is one.
	var snapst snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapst)
	if err != nil && err != state.ErrNoState {
		return err
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil {
		// The snap was not installed before so undo should remove security profiles.
		return m.removeProfilesForSnap(task, tomb, snapName)
	} else {
		// The snap was installed before so undo should setup the old security profiles.
		snapInfo, err := snap.ReadInfo(snapName, sideInfo)
		if err != nil {
			return err
		}
		opts := confinementOptions(snapst.Flags)
		return m.setupProfilesForSnap(task, tomb, snapInfo, opts)
	}
}

func (m *InterfaceManager) doDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapSetup.Name()

	var snapst snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapst)
	if err != nil && err != state.ErrNoState {
		return err
	}

	if err == nil && len(snapst.Sequence) != 0 {
		return fmt.Errorf("cannot discard connections for snap %q while it is present", snapName)
	}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	removed := make(map[string]connState)
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		if connRef.PlugRef.Snap == snapName || connRef.SlotRef.Snap == snapName {
			removed[id] = conns[id]
			delete(conns, id)
		}
	}
	task.Set("removed", removed)
	setConns(st, conns)
	return nil
}

func (m *InterfaceManager) undoDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var removed map[string]connState
	err := task.Get("removed", &removed)
	if err != nil && err != state.ErrNoState {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	for id, connState := range removed {
		conns[id] = connState
	}
	setConns(st, conns)
	task.Set("removed", nil)
	return nil
}

func getDynamicHookAttributes(task *state.Task) (map[string]interface{}, map[string]interface{}, error) {
	var plugAttrs, slotAttrs map[string]interface{}

	if err := task.Get("plug-dynamic", &plugAttrs); err != nil {
		return nil, nil, err
	}
	if err := task.Get("slot-dynamic", &slotAttrs); err != nil {
		return nil, nil, err
	}

	return plugAttrs, slotAttrs, nil
}

func setDynamicHookAttributes(task *state.Task, dynamicPlugAttrs map[string]interface{}, dynamicSlotAttrs map[string]interface{}) {
	task.Set("plug-dynamic", dynamicPlugAttrs)
	task.Set("slot-dynamic", dynamicSlotAttrs)
}

func markConnectHooksDone(connectTask *state.Task) error {
	for _, t := range []string{"connect-plug-task", "connect-slot-task"} {
		var tid string
		err := connectTask.Get(t, &tid)
		if err == nil {
			t := connectTask.State().Task(tid)
			if t != nil {
				t.SetStatus(state.DoneStatus)
			}
		}
		if err != nil && err != state.ErrNoState {
			return fmt.Errorf("internal error: failed to determine %s id: %s", t, err)
		}
	}
	return nil
}

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	var autoConnect bool
	if err := task.Get("auto", &autoConnect); err != nil && err != state.ErrNoState {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	connRef := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		if autoConnect && err == state.ErrNoState {
			// ignore the error if auto-connecting
			task.Logf("snap %q is no longer available for auto-connecting", plugRef.Snap)
			return markConnectHooksDone(task)
		}
		return err
	}

	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		if autoConnect && err == state.ErrNoState {
			// ignore the error if auto-connecting
			task.Logf("snap %q is no longer available for auto-connecting", slotRef.Snap)
			return markConnectHooksDone(task)
		}
		return err
	}

	plug := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
	if plug == nil {
		if autoConnect {
			// ignore the error if auto-connecting
			task.Logf("snap %q no longer has %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
			return markConnectHooksDone(task)
		}
		return fmt.Errorf("snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}

	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		if autoConnect {
			// ignore the error if auto-connecting
			task.Logf("snap %q no longer has %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
			return markConnectHooksDone(task)
		}
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	// attributes are always present, even if there are no hooks (they're initialized by Connect).
	plugDynamicAttrs, slotDynamicAttrs, err := getDynamicHookAttributes(task)
	if err != nil {
		return fmt.Errorf("failed to get hook attributes: %s", err)
	}

	var policyChecker func(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, error)

	if autoConnect {
		autochecker, err := newAutoConnectChecker(st)
		if err != nil {
			return err
		}
		policyChecker = autochecker.check
	} else {
		policyCheck, err := newConnectChecker(st)
		if err != nil {
			return err
		}
		policyChecker = policyCheck.check
	}

	conn, err := m.repo.Connect(connRef, plugDynamicAttrs, slotDynamicAttrs, policyChecker)
	if err != nil || conn == nil {
		return err
	}

	slotOpts := confinementOptions(slotSnapst.Flags)
	if err := m.setupSnapSecurity(task, slot.Snap, slotOpts); err != nil {
		return err
	}
	plugOpts := confinementOptions(plugSnapst.Flags)
	if err := m.setupSnapSecurity(task, plug.Snap, plugOpts); err != nil {
		return err
	}

	// if reconnecting, store old connection info for undo
	if oldconn, ok := conns[connRef.ID()]; ok {
		task.Set("old-conn", oldconn)
	}

	conns[connRef.ID()] = connState{
		Interface:        conn.Interface(),
		StaticPlugAttrs:  conn.Plug.StaticAttrs(),
		DynamicPlugAttrs: conn.Plug.DynamicAttrs(),
		StaticSlotAttrs:  conn.Slot.StaticAttrs(),
		DynamicSlotAttrs: conn.Slot.DynamicAttrs(),
		Auto:             autoConnect,
	}
	setConns(st, conns)

	// the dynamic attributes might have been updated by interface's BeforeConnectPlug/Slot code,
	// so we need to update the task for connect-plug- and connect-slot- hooks to see new values.
	setDynamicHookAttributes(task, conn.Plug.DynamicAttrs(), conn.Slot.DynamicAttrs())
	return nil
}

func (m *InterfaceManager) doDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	var snapStates []snapstate.SnapState
	for _, snapName := range []string{plugRef.Snap, slotRef.Snap} {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, snapName, &snapst); err != nil {
			if err == state.ErrNoState {
				task.Logf("skipping disconnect operation for connection %s %s, snap %q doesn't exist", plugRef, slotRef, snapName)
				return nil
			}
			task.Errorf("skipping security profiles setup for snap %q when disconnecting %s from %s: %v", snapName, plugRef, slotRef, err)
		} else {
			snapStates = append(snapStates, snapst)
		}
	}

	err = m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return fmt.Errorf("snapd changed, please retry the operation: %v", err)
	}
	for _, snapst := range snapStates {
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		opts := confinementOptions(snapst.Flags)
		if err := m.setupSnapSecurity(task, snapInfo, opts); err != nil {
			return err
		}
	}

	conn := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}
	delete(conns, conn.ID())

	setConns(st, conns)
	return nil
}

// doReconnect creates a set of tasks for connecting the interface and running its hooks
func (m *InterfaceManager) doReconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapsup.Name()
	connections, err := m.repo.Connections(snapName)

	connectts := state.NewTaskSet()
	chg := task.Change()
	for _, conn := range connections {
		ts, err := Connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name)
		if err != nil {
			return err
		}
		connectts.AddAll(ts)
	}

	task.SetStatus(state.DoneStatus)

	lanes := task.Lanes()
	if len(lanes) == 1 && lanes[0] == 0 {
		lanes = nil
	}
	ht := task.HaltTasks()

	// add all connect tasks to the change of main "reconnect" task and to the same lane.
	for _, l := range lanes {
		connectts.JoinLane(l)
	}
	chg.AddAll(connectts)
	// make all halt tasks of the main 'reconnect' task wait on connect tasks
	for _, t := range ht {
		t.WaitAll(connectts)
	}

	st.EnsureBefore(0)
	return nil
}

func (m *InterfaceManager) undoConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var oldconn connState
	err := st.Get("old-conn", &oldconn)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	connRef := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	conns[connRef.ID()] = oldconn
	setConns(st, conns)
	return nil
}

// doAutoConnect creates task(s) to connect the given snap to viable candidates.
func (m *InterfaceManager) doAutoConnect(task *state.Task, _ *tomb.Tomb) error {
	// FIXME: here we should not reconnect auto-connect plug/slot
	// pairs that were explicitly disconnected by the user

	st := task.State()
	st.Lock()
	defer st.Unlock()

	var conns map[string]connState
	err := st.Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if conns == nil {
		conns = make(map[string]connState)
	}

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapsup.Name()

	autots := state.NewTaskSet()
	autochecker, err := newAutoConnectChecker(st)
	if err != nil {
		return err
	}

	chg := task.Change()

	// Auto-connect all the plugs
	for _, plug := range m.repo.Plugs(snapName) {
		candidates := m.repo.AutoConnectCandidateSlots(snapName, plug.Name, autochecker.check)
		if len(candidates) == 0 {
			continue
		}
		// If we are in a core transition we may have both the old ubuntu-core
		// snap and the new core snap providing the same interface. In that
		// situation we want to ignore any candidates in ubuntu-core and simply
		// go with those from the new core snap.
		if len(candidates) == 2 {
			switch {
			case candidates[0].Snap.Name() == "ubuntu-core" && candidates[1].Snap.Name() == "core":
				candidates = candidates[1:2]
			case candidates[1].Snap.Name() == "ubuntu-core" && candidates[0].Snap.Name() == "core":
				candidates = candidates[0:1]
			}
		}
		if len(candidates) != 1 {
			crefs := make([]string, len(candidates))
			for i, candidate := range candidates {
				crefs[i] = candidate.String()
			}
			task.Logf("cannot auto-connect plug %s, candidates found: %s", plug, strings.Join(crefs, ", "))
			continue
		}
		slot := candidates[0]
		connRef := interfaces.NewConnRef(plug, slot)
		key := connRef.ID()
		if _, ok := conns[key]; ok {
			// Suggested connection already exist so don't clobber it.
			// NOTE: we don't log anything here as this is a normal and common condition.
			continue
		}

		ts, err := AutoConnect(st, chg, task, plug.Snap.Name(), plug.Name, slot.Snap.Name(), slot.Name)
		if err != nil {
			task.Logf("cannot auto-connect plug %s to %s: %s", connRef.PlugRef, connRef.SlotRef, err)
			continue
		}
		autots.AddAll(ts)
	}
	// Auto-connect all the slots
	for _, slot := range m.repo.Slots(snapName) {
		candidates := m.repo.AutoConnectCandidatePlugs(snapName, slot.Name, autochecker.check)
		if len(candidates) == 0 {
			continue
		}

		for _, plug := range candidates {
			// make sure slot is the only viable
			// connection for plug, same check as if we were
			// considering auto-connections from plug
			candSlots := m.repo.AutoConnectCandidateSlots(plug.Snap.Name(), plug.Name, autochecker.check)

			if len(candSlots) != 1 || candSlots[0].String() != slot.String() {
				crefs := make([]string, len(candSlots))
				for i, candidate := range candSlots {
					crefs[i] = candidate.String()
				}
				task.Logf("cannot auto-connect slot %s to %s, candidates found: %s", slot, plug, strings.Join(crefs, ", "))
				continue
			}

			connRef := interfaces.NewConnRef(plug, slot)
			key := connRef.ID()
			if _, ok := conns[key]; ok {
				// Suggested connection already exist so don't clobber it.
				// NOTE: we don't log anything here as this is a normal and common condition.
				continue
			}
			ts, err := AutoConnect(st, chg, task, plug.Snap.Name(), plug.Name, slot.Snap.Name(), slot.Name)
			if err != nil {
				task.Logf("cannot auto-connect slot %s to %s: %s", connRef.SlotRef, connRef.PlugRef, err)
				continue
			}
			autots.AddAll(ts)
		}
	}

	task.SetStatus(state.DoneStatus)

	lanes := task.Lanes()
	if len(lanes) == 1 && lanes[0] == 0 {
		lanes = nil
	}
	ht := task.HaltTasks()

	// add all connect tasks to the change of main "auto-connect" task and to the same lane.
	for _, l := range lanes {
		autots.JoinLane(l)
	}
	chg.AddAll(autots)
	// make all halt tasks of the main 'auto-connect' task wait on connect tasks
	for _, t := range ht {
		t.WaitAll(autots)
	}

	st.EnsureBefore(0)
	return nil
}

func (m *InterfaceManager) undoAutoConnect(task *state.Task, _ *tomb.Tomb) error {
	// TODO Introduce disconnection hooks, and run them here as well to give a chance
	// for the snap to undo whatever it did when the connection was established.
	return nil
}

// transitionConnectionsCoreMigration will transition all connections
// from oldName to newName. Note that this is only useful when you
// know that newName supports everything that oldName supports,
// otherwise you will be in a world of pain.
func (m *InterfaceManager) transitionConnectionsCoreMigration(st *state.State, oldName, newName string) error {
	// transition over, ubuntu-core has only slots
	conns, err := getConns(st)
	if err != nil {
		return err
	}

	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		if connRef.SlotRef.Snap == oldName {
			connRef.SlotRef.Snap = newName
			conns[connRef.ID()] = conns[id]
			delete(conns, id)
		}
	}
	setConns(st, conns)

	// The reloadConnections() just modifies the repository object, it
	// has no effect on the running system, i.e. no security profiles
	// on disk are rewriten. This is ok because core/ubuntu-core have
	// exactly the same profiles and nothing in the generated policies
	// has the slot-name encoded.
	if _, err := m.reloadConnections(oldName); err != nil {
		return err
	}
	if _, err := m.reloadConnections(newName); err != nil {
		return err
	}

	return nil
}

func (m *InterfaceManager) doTransitionUbuntuCore(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	var oldName, newName string
	if err := t.Get("old-name", &oldName); err != nil {
		return err
	}
	if err := t.Get("new-name", &newName); err != nil {
		return err
	}

	return m.transitionConnectionsCoreMigration(st, oldName, newName)
}

func (m *InterfaceManager) undoTransitionUbuntuCore(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// symmetrical to the "do" method, just reverse them again
	var oldName, newName string
	if err := t.Get("old-name", &oldName); err != nil {
		return err
	}
	if err := t.Get("new-name", &newName); err != nil {
		return err
	}

	return m.transitionConnectionsCoreMigration(st, newName, oldName)
}
