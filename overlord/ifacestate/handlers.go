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
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/overlord/assertstate"
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

	connectedSnaps, err := m.autoConnect(task, snapName, nil)
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
	autoConnectDisabled, err := getAutoconnectDisabled(st)
	if err != nil {
		return err
	}
	removed := make(map[string]connState)
	var disabled []string
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		if connRef.PlugRef.Snap == snapName || connRef.SlotRef.Snap == snapName {
			removed[id] = conns[id]
			delete(conns, id)
			// if autoconnect was disabled by the user, remember it in the disabled list for undoing
			if autoConnectDisabled[id] {
				delete(autoConnectDisabled, id)
				disabled = append(disabled, id)
			}
		}
	}
	task.Set("removed", removed)
	if len(disabled) > 0 {
		task.Set("disabled", disabled)
		setAutoConnectDisabled(st, autoConnectDisabled)
	}
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

	var disabled []string
	err = task.Get("disabled", &disabled)
	if err != nil && err != state.ErrNoState {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	autoConnectDisabled, err := getAutoconnectDisabled(st)
	if err != nil {
		return err
	}

	for id, connState := range removed {
		conns[id] = connState
	}

	// restore the list of disabled auto-connections
	if len(disabled) > 0 {
		for _, id := range disabled {
			autoConnectDisabled[id] = true
		}
		setAutoConnectDisabled(st, autoConnectDisabled)
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
		return fmt.Errorf("internal error: cannot find base declaration: %v", err)
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot,
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

	// TODO: pass dynamic attributes from hooks
	err = m.repo.Connect(connRef)
	if err != nil {
		return err
	}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, connRef.PlugRef.Snap, &plugSnapst); err != nil {
		return err
	}

	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, connRef.SlotRef.Snap, &slotSnapst); err != nil {
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

	conns[connRef.ID()] = connState{Interface: plug.Interface}
	setConns(st, conns)

	// remove the connection from the autoconnect-disabled map.
	// TODO: when autoconnect-tasks branch lands, this needs to be conditional (only done
	// for manually-triggered connect tasks).
	autoConnectDisabled, err := getAutoconnectDisabled(st)
	if err != nil {
		return err
	}
	if autoConnectDisabled[connRef.ID()] {
		delete(autoConnectDisabled, connRef.ID())
		setAutoConnectDisabled(st, autoConnectDisabled)
	}

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
	if cs, ok := conns[conn.ID()]; ok && cs.Auto {
		autoConnectDisabled, err := getAutoconnectDisabled(st)
		if err != nil {
			return err
		}
		autoConnectDisabled[conn.ID()] = true
		setAutoConnectDisabled(st, autoConnectDisabled)
	}

	delete(conns, conn.ID())
	setConns(st, conns)

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
