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

	"gopkg.in/tomb.v2"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
)

func (m *InterfaceManager) doSetupProfiles(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapInfo, err := snapstate.Info(task.State(), ss.Name, ss.Revision)
	if err != nil {
		return err
	}
	snap.AddImplicitSlots(snapInfo)
	snapName := snapInfo.Name()
	var snapState snapstate.SnapState
	if err := snapstate.Get(task.State(), snapName, &snapState); err != nil {
		task.Errorf("cannot get state of snap %q: %s", snapName, err)
		return err
	}

	// Set DevMode flag if SnapSetup.Flags indicates it should be done
	// but remember the old value in the task in case we undo.
	task.Set("old-devmode", snapState.DevMode())
	if ss.DevMode() {
		snapState.Flags |= snapstate.DevMode
	} else {
		snapState.Flags &= ^snapstate.DevMode
	}
	snapstate.Set(task.State(), snapName, &snapState)

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
	blacklist := m.repo.AutoConnectBlacklist(snapName)
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	// XXX: what about snap renames? We should remove the old name (or switch
	// to IDs in the interfaces repository)
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}
	if err := m.repo.AddSnap(snapInfo); err != nil {
		if _, ok := err.(*interfaces.BadInterfacesError); ok {
			logger.Noticef("%s", err)
		} else {
			return err
		}
	}
	if err := m.reloadConnections(snapName); err != nil {
		return err
	}
	if err := m.autoConnect(task, snapName, blacklist); err != nil {
		return err
	}
	if err := setupSnapSecurity(task, snapInfo, m.repo); err != nil {
		return state.Retry
	}
	for _, snapName := range affectedSnaps {
		// The affected snap is setup explicitly so skip it here.
		if snapName == snapInfo.Name() {
			continue
		}
		snapInfo, err := snapstate.Current(task.State(), snapName)
		if err != nil {
			return err
		}
		snap.AddImplicitSlots(snapInfo)
		if err := setupSnapSecurity(task, snapInfo, m.repo); err != nil {
			return state.Retry
		}
	}
	return nil
}

func (m *InterfaceManager) doRemoveProfiles(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	// Get SnapSetup for this snap. This is gives us the name of the snap.
	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapName := snapSetup.Name

	// Get SnapState for this snap
	var snapState snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapState)
	if err != nil && err != state.ErrNoState {
		return err
	}

	// Get the old-devmode flag from the task.
	// This flag is set by setup-profiles in case we have to undo.
	var oldDevMode bool
	err = task.Get("old-devmode", &oldDevMode)
	if err != nil && err != state.ErrNoState {
		return err
	}
	// Restore the state of DevMode flag if old-devmode was saved in the task.
	if err == nil {
		if oldDevMode {
			snapState.Flags |= snapstate.DevMode
		} else {
			snapState.Flags &= ^snapstate.DevMode
		}
		snapstate.Set(st, snapName, &snapState)
	}

	// Disconnect the snap entirely.
	// This is required to remove the snap from the interface repository.
	// The returned list of affected snaps will need to have its security setup
	// to reflect the change.
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}

	// Setup security of the affected snaps.
	for _, affectedSnapName := range affectedSnaps {
		if affectedSnapName == snapName {
			// Skip setup for the snap being removed as this is handled below.
			continue
		}
		affectedSnapInfo, err := snapstate.Current(task.State(), affectedSnapName)
		if err != nil {
			return err
		}
		if err := setupSnapSecurity(task, affectedSnapInfo, m.repo); err != nil {
			return state.Retry
		}
	}

	// Remove the snap from the interface repository.
	// This discards all the plugs and slots belonging to that snap.
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return err
	}

	// Remove security artefacts of the snap.
	if err := removeSnapSecurity(task, snapName); err != nil {
		return state.Retry
	}

	return nil
}

func (m *InterfaceManager) doDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapSetup.Name

	var snapState snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapState)
	if err != nil && err != state.ErrNoState {
		return err
	}

	if err == nil && len(snapState.Sequence) != 0 {
		return fmt.Errorf("cannot discard connections for snap %q while it is present", snapName)
	}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	removed := make(map[string]connState)
	for id := range conns {
		plugRef, slotRef, err := parseConnID(id)
		if err != nil {
			return err
		}
		if plugRef.Snap == snapName || slotRef.Snap == snapName {
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

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
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

	err = m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return err
	}

	plug := m.repo.Plug(plugRef.Snap, plugRef.Name)
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	if err := setupSnapSecurity(task, plug.Snap, m.repo); err != nil {
		return state.Retry
	}
	if err := setupSnapSecurity(task, slot.Snap, m.repo); err != nil {
		return state.Retry
	}

	conns[connID(plugRef, slotRef)] = connState{Interface: plug.Interface}
	setConns(st, conns)

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

	err = m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return err
	}

	plug := m.repo.Plug(plugRef.Snap, plugRef.Name)
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	if err := setupSnapSecurity(task, plug.Snap, m.repo); err != nil {
		return state.Retry
	}
	if err := setupSnapSecurity(task, slot.Snap, m.repo); err != nil {
		return state.Retry
	}

	delete(conns, connID(plugRef, slotRef))
	setConns(st, conns)
	return nil
}
