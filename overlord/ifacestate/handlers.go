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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

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
			return err
		}
		affectedSnapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		snap.AddImplicitSlots(affectedSnapInfo)
		if err := setupSnapSecurity(task, affectedSnapInfo, snapst.DevModeAllowed(), m.repo); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) doSetupProfiles(task *state.Task, _ *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	ss, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapInfo, err := snap.ReadInfo(ss.Name(), ss.SideInfo)
	if err != nil {
		return err
	}
	snap.AddImplicitSlots(snapInfo)
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
	if err := setupSnapSecurity(task, snapInfo, ss.DevModeAllowed(), m.repo); err != nil {
		return err
	}

	return m.setupAffectedSnaps(task, snapName, affectedSnaps)
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
	snapName := snapSetup.Name()

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
	if err := removeSnapSecurity(task, snapName); err != nil {
		// TODO: how long to wait?
		return &state.Retry{}
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

	snapName := snapSetup.Name()

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
	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		return err
	}
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		return err
	}

	if err := setupSnapSecurity(task, plug.Snap, plugSnapst.DevModeAllowed(), m.repo); err != nil {
		return err
	}
	if err := setupSnapSecurity(task, slot.Snap, slotSnapst.DevModeAllowed(), m.repo); err != nil {
		return err
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
	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		return err
	}
	slot := m.repo.Slot(slotRef.Snap, slotRef.Name)
	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		return err
	}

	if err := setupSnapSecurity(task, plug.Snap, plugSnapst.DevModeAllowed(), m.repo); err != nil {
		return err
	}
	if err := setupSnapSecurity(task, slot.Snap, slotSnapst.DevModeAllowed(), m.repo); err != nil {
		return err
	}

	delete(conns, connID(plugRef, slotRef))
	setConns(st, conns)
	return nil
}
