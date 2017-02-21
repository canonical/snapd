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

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
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
	const fnName = "setupAffectedSnaps"

	st := task.State()

	// Setup security of the affected snaps.
	for _, affectedSnapName := range affectedSnaps {
		// the snap that triggered the change needs to be skipped
		if affectedSnapName == affectingSnap {
			continue
		}
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, affectedSnapName, &snapst); err != nil {
			return fmt.Errorf("(internal error, %s) cannot get state of snap %q (affected by change of %q): %s",
				fnName, affectedSnapName, affectingSnap, err)
		}
		affectedSnapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return fmt.Errorf("(internal error, %s) cannot get current information about snap %q (affected by change of %q): %s",
				fnName, affectedSnapName, affectingSnap, err)
		}
		snap.AddImplicitSlots(affectedSnapInfo)
		opts := confinementOptions(snapst.Flags)
		if err := setupSnapSecurity(task, affectedSnapInfo, opts, m.repo); err != nil {
			return fmt.Errorf("(internal error, %s) cannot setup security profiles for snap %q (affected by change of %q): %s",
				fnName, affectedSnapName, affectingSnap, err)
		}
	}
	return nil
}

func (m *InterfaceManager) doSetupProfiles(task *state.Task, tomb *tomb.Tomb) error {
	const fnName = "doSetupProfiles"

	task.State().Lock()
	defer task.State().Unlock()

	// Get snap.Info from bits handed by the snap manager.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get snap setup data: %s", fnName, err)
	}

	snapInfo, err := snap.ReadInfo(snapsup.Name(), snapsup.SideInfo)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot read info of snap %q: %s", fnName, snapsup.Name(), err)
	}

	opts := confinementOptions(snapsup.Flags)
	if err := m.setupProfilesForSnap(task, tomb, snapInfo, opts); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security profiles for snap %q: %s", fnName, snapInfo.Name(), err)
	}
	return nil
}

func (m *InterfaceManager) setupProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapInfo *snap.Info, opts interfaces.ConfinementOptions) error {
	const fnName = "setupProfilesForSnap"

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
	disconnectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot temporarily disconnect interface repository connections of snap %q: %s", fnName, snapName, err)
	}
	// XXX: what about snap renames? We should remove the old name (or switch
	// to IDs in the interfaces repository)
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot temporarily remove snap %q from the interface repository: %s", fnName, snapName, err)
	}
	if err := m.repo.AddSnap(snapInfo); err != nil {
		if _, ok := err.(*interfaces.BadInterfacesError); ok {
			task.Logf("%s", err)
		} else {
			return fmt.Errorf("(internal error, %s) cannot re-add snap %q to interface repository: %s", fnName, snapName, err)
		}
	}
	if err := m.reloadConnections(snapName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot reload interface repository connections of snap %q: %s", fnName, snapName, err)
	}
	// FIXME: here we should not reconnect auto-connect plug/slot
	// pairs that were explicitly disconnected by the user
	connectedSnaps, err := m.autoConnect(task, snapName, nil)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot auto-connect snap %q: %s", fnName, snapName, err)
	}
	if err := setupSnapSecurity(task, snapInfo, opts, m.repo); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security profiles for snap %q: %s", fnName, snapName, err)
	}
	affectedSet := make(map[string]bool)
	for _, name := range disconnectedSnaps {
		affectedSet[name] = true
	}
	for _, name := range connectedSnaps {
		affectedSet[name] = true
	}
	// The principal snap was already handled above.
	delete(affectedSet, snapInfo.Name())
	affectedSnaps := make([]string, 0, len(affectedSet))
	for name := range affectedSet {
		affectedSnaps = append(affectedSnaps, name)
	}
	sort.Strings(affectedSnaps)
	if err := m.setupAffectedSnaps(task, snapName, affectedSnaps); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security profiles for snaps %q affected by changes to connections of snap %q: %s", fnName, affectedSnaps, snapName, err)
	}
	return nil
}

func (m *InterfaceManager) doRemoveProfiles(task *state.Task, tomb *tomb.Tomb) error {
	const fnName = "doRemoveProfiles"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	// Get SnapSetup for this snap. This is gives us the name of the snap.
	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get snap setup data: %s", fnName, err)
	}
	snapName := snapSetup.Name()

	if err := m.removeProfilesForSnap(task, tomb, snapName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot remove security profiles of snap %q: %s", fnName, snapName, err)
	}
	return nil
}

func (m *InterfaceManager) removeProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapName string) error {
	const fnName = "removeProfilesForSnap"

	// Disconnect the snap entirely.
	// This is required to remove the snap from the interface repository.
	// The returned list of affected snaps will need to have its security setup
	// to reflect the change.
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot disconnect interface repository connections of snap %q: %s", fnName, snapName, err)
	}

	if err := m.setupAffectedSnaps(task, snapName, affectedSnaps); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security of snaps (%s) affected by changes to connections of snap %q: %s", fnName, affectedSnaps, snapName, err)
	}

	// Remove the snap from the interface repository.
	// This discards all the plugs and slots belonging to that snap.
	if err := m.repo.RemoveSnap(snapName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot remove snap %q from the interface repository: %s", fnName, snapName, err)
	}

	// Remove security artefacts of the snap.
	if err := removeSnapSecurity(task, snapName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot remove security profiles of snap %q: %s", fnName, snapName, err)
	}

	return nil
}

func (m *InterfaceManager) undoSetupProfiles(task *state.Task, tomb *tomb.Tomb) error {
	const fnName = "undoSetupProfiles"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get snap setup data: %s", fnName, err)
	}
	snapName := snapsup.Name()

	// Get the name from SnapSetup and use it to find the current SideInfo
	// about the snap, if there is one.
	var snapst snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapst)
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("(internal error, %s) cannot get state of snap %q: %s", fnName, snapName, err)
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil {
		// The snap was not installed before so undo should remove security profiles.
		if err := m.removeProfilesForSnap(task, tomb, snapName); err != nil {
			return fmt.Errorf("(internal error, %s) cannot remove security profiles of snap %q: %s", fnName, snapName, err)
		}
	} else {
		// The snap was installed before so undo should setup the old security profiles.
		snapInfo, err := snap.ReadInfo(snapName, sideInfo)
		if err != nil {
			return fmt.Errorf("(internal error, %s) cannot read info of snap %q: %s", fnName, snapName, err)
		}
		opts := confinementOptions(snapst.Flags)
		if err := m.setupProfilesForSnap(task, tomb, snapInfo, opts); err != nil {
			return fmt.Errorf("(internal error, %s) cannot setup security profiles of snap %q: %s", fnName, snapName, err)
		}
	}
	return nil
}

func (m *InterfaceManager) doDiscardConns(task *state.Task, _ *tomb.Tomb) error {
	const fnName = "doDiscardConns"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get snap setup data: %s", fnName, err)
	}

	snapName := snapSetup.Name()

	var snapst snapstate.SnapState
	err = snapstate.Get(st, snapName, &snapst)
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("(internal error, %s) cannot get state of snap %q: %s", fnName, snapName, err)
	}

	if err == nil && len(snapst.Sequence) != 0 {
		return fmt.Errorf("cannot discard connections for snap %q while it is present", snapName)
	}
	conns, err := getConns(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get state of connections: %s", fnName, err)
	}
	removed := make(map[string]connState)
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return fmt.Errorf("(internal error, %s) cannot parse connection reference %q: %s", fnName, id, err)
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
	const fnName = "undoDiscardConns"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	var removed map[string]connState
	err := task.Get("removed", &removed)
	if err != nil && err != state.ErrNoState {
		return fmt.Errorf("(internal error, %s) cannot get removed connections: %s", fnName, err)
	}

	conns, err := getConns(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get state of connections: %s", fnName, err)
	}

	for id, connState := range removed {
		conns[id] = connState
	}
	setConns(st, conns)
	task.Set("removed", nil)
	return nil
}

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) error {
	const fnName = "doConnect"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get plug and slot references: %s", fnName, err)
	}

	conns, err := getConns(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get state of connections: %s", fnName, err)
	}

	connRef := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	plug := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
	if plug == nil {
		return fmt.Errorf("snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap.SnapID != "" {
		var err error
		plugDecl, err = assertstate.SnapDeclaration(st, plug.Snap.SnapID)
		if err != nil {
			return fmt.Errorf("cannot find snap declaration for %q: %v", plug.Snap.Name(), err)
		}
	}

	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}
	var slotDecl *asserts.SnapDeclaration
	if slot.Snap.SnapID != "" {
		var err error
		slotDecl, err = assertstate.SnapDeclaration(st, slot.Snap.SnapID)
		if err != nil {
			return fmt.Errorf("cannot find snap declaration for %q: %v", slot.Snap.Name(), err)
		}
	}

	baseDecl, err := assertstate.BaseDeclaration(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot find base declaration: %v", fnName, err)
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug.PlugInfo,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot.SlotInfo,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     baseDecl,
	}

	// if either of plug or slot snaps don't have a declaration it
	// means they were installed with "dangerous", so the security
	// check should be skipped at this point.
	if plugDecl != nil && slotDecl != nil {
		err = ic.Check()
		if err != nil {
			return err
		}
	}

	err = m.repo.Connect(connRef)
	if err != nil {
		// NOTE: repository.Connect already produces useful error messages
		return err
	}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, connRef.PlugRef.Snap, &plugSnapst); err != nil {
		// XXX: ErrNoState not handled (but unexpected in doConnect)
		return fmt.Errorf("(internal error, %s) cannot get state of snap %q (plug-side, just-connected): %s",
			fnName, connRef.PlugRef.Snap, err)
	}

	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, connRef.SlotRef.Snap, &slotSnapst); err != nil {
		// XXX: ErrNoState not handled (but unexpected in doConnect)
		return fmt.Errorf("(internal error, %s) cannot get state of snap %q (slot-side, just-connected): %s",
			fnName, connRef.SlotRef.Snap, err)
	}

	slotOpts := confinementOptions(slotSnapst.Flags)
	if err := setupSnapSecurity(task, slot.Snap, slotOpts, m.repo); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security of snap %q (slot-side, just-connected): %s",
			fnName, slot.Snap.Name(), err)
	}
	plugOpts := confinementOptions(plugSnapst.Flags)
	if err := setupSnapSecurity(task, plug.Snap, plugOpts, m.repo); err != nil {
		return fmt.Errorf("(internal error, %s) cannot setup security of snap %q (plug-side, just-connected): %s",
			fnName, plug.Snap.Name(), err)
	}

	conns[connRef.ID()] = connState{Interface: plug.Interface}
	setConns(st, conns)

	return nil
}

func snapNamesFromConns(conns []interfaces.ConnRef) []string {
	m := make(map[string]bool)
	for _, conn := range conns {
		m[conn.PlugRef.Snap] = true
		m[conn.SlotRef.Snap] = true
	}
	l := make([]string, 0, len(m))
	for name := range m {
		l = append(l, name)
	}
	sort.Strings(l)
	return l
}

func (m *InterfaceManager) doDisconnect(task *state.Task, _ *tomb.Tomb) error {
	const fnName = "doDisconnect"

	st := task.State()
	st.Lock()
	defer st.Unlock()

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get plug and slot references: %s", fnName, err)
	}

	conns, err := getConns(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get state of connections: %s", fnName, err)
	}

	affectedConns, err := m.repo.ResolveDisconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get resolve disconnect %q:%q %q:%q: %s",
			fnName, plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name, err)
	}
	m.repo.DisconnectAll(affectedConns)
	affectedSnaps := snapNamesFromConns(affectedConns)
	for _, snapName := range affectedSnaps {
		var snapst snapstate.SnapState
		// XXX: ErrNoState not handled (but unexpected in doDisconnect)
		if err := snapstate.Get(st, snapName, &snapst); err != nil {
			return fmt.Errorf("(internal error, %s) cannot get state of snap %q (affected by disconnect): %s",
				fnName, snapName, err)
		}
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return fmt.Errorf("(internal error, %s) cannot get current info of snap %q (affected by disconnect): %s",
				fnName, snapName, err)
		}
		opts := confinementOptions(snapst.Flags)
		if err := setupSnapSecurity(task, snapInfo, opts, m.repo); err != nil {
			return fmt.Errorf("(internal error, %s) cannot setup security of snap %q (affected by disconnect): %s",
				fnName, snapInfo.Name(), err)
		}
	}
	for _, conn := range affectedConns {
		delete(conns, conn.ID())
	}

	setConns(st, conns)
	return nil
}

// transitionConnectionsCoreMigration will transition all connections
// from oldName to newName. Note that this is only useful when you
// know that newName supports everything that oldName supports,
// otherwise you will be in a world of pain.
func (m *InterfaceManager) transitionConnectionsCoreMigration(st *state.State, oldName, newName string) error {
	const fnName = "transitionConnectionsCoreMigration"

	// transition over, ubuntu-core has only slots
	conns, err := getConns(st)
	if err != nil {
		return fmt.Errorf("(internal error, %s) cannot get state of connections: %s", fnName, err)
	}

	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return fmt.Errorf("(internal error, %s) cannot parse connection reference %q: %s", fnName, id, err)
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
	if err := m.reloadConnections(oldName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot reload connections of old core %q: %s", fnName, oldName, err)
	}
	if err := m.reloadConnections(newName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot reload connections of new core %q: %s", fnName, newName, err)
	}

	return nil
}

func (m *InterfaceManager) doTransitionUbuntuCore(t *state.Task, _ *tomb.Tomb) error {
	const fnName = "doTransitionUbuntuCore"

	st := t.State()
	st.Lock()
	defer st.Unlock()

	var oldName, newName string
	if err := t.Get("old-name", &oldName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot get name of the old core: %s", fnName, err)
	}
	if err := t.Get("new-name", &newName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot get name of the new core: %s", fnName, err)
	}

	if err := m.transitionConnectionsCoreMigration(st, oldName, newName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot transition connections for core migration %q -> %q: %s", fnName, oldName, newName, err)
	}

	return nil
}

func (m *InterfaceManager) undoTransitionUbuntuCore(t *state.Task, _ *tomb.Tomb) error {
	const fnName = "undoTransitionUbuntuCore"

	st := t.State()
	st.Lock()
	defer st.Unlock()

	// symmetrical to the "do" method, just reverse them again
	var oldName, newName string
	if err := t.Get("old-name", &oldName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot get name of the old core: %s", fnName, err)
	}
	if err := t.Get("new-name", &newName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot get name of the new core: %s", fnName, err)
	}

	if err := m.transitionConnectionsCoreMigration(st, newName, oldName); err != nil {
		return fmt.Errorf("(internal error, %s) cannot transition connections for core migration %q -> %q: %s", fnName, newName, oldName, err)
	}

	return nil
}
