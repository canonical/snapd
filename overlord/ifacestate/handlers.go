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
	"github.com/snapcore/snapd/logger"
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

	snapInfo, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	// We no longer do/need core-phase-2, see
	//   https://github.com/snapcore/snapd/pull/5301
	// This code is just here to deal with old state that may still
	// have the 2nd setup-profiles with this flag set.
	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && err != state.ErrNoState {
		return err
	}
	if corePhase2 {
		// nothing to do
		return nil
	}

	opts := confinementOptions(snapsup.Flags)
	return m.setupProfilesForSnap(task, tomb, snapInfo, opts)
}

func (m *InterfaceManager) setupProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapInfo *snap.Info, opts interfaces.ConfinementOptions) error {
	addImplicitSlots(snapInfo)
	snapName := snapInfo.InstanceName()

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
	delete(affectedSet, snapInfo.InstanceName())
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
	snapName := snapSetup.InstanceName()

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
	snapName := snapsup.InstanceName()

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

	snapName := snapSetup.InstanceName()

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

func getDynamicHookAttributes(task *state.Task) (plugAttrs, slotAttrs map[string]interface{}, err error) {
	if err = task.Get("plug-dynamic", &plugAttrs); err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	if err = task.Get("slot-dynamic", &slotAttrs); err != nil && err != state.ErrNoState {
		return nil, nil, err
	}
	if plugAttrs == nil {
		plugAttrs = make(map[string]interface{})
	}
	if slotAttrs == nil {
		slotAttrs = make(map[string]interface{})
	}

	return plugAttrs, slotAttrs, nil
}

func setDynamicHookAttributes(task *state.Task, plugAttrs, slotAttrs map[string]interface{}) {
	task.Set("plug-dynamic", plugAttrs)
	task.Set("slot-dynamic", slotAttrs)
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
	var byGadget bool
	if err := task.Get("by-gadget", &byGadget); err != nil && err != state.ErrNoState {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	connRef := &interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		if autoConnect && err == state.ErrNoState {
			// conflict logic should prevent this
			return fmt.Errorf("internal error: snap %q is no longer available for auto-connecting", plugRef.Snap)
		}
		return err
	}

	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		if autoConnect && err == state.ErrNoState {
			// conflict logic should prevent this
			return fmt.Errorf("internal error: snap %q is no longer available for auto-connecting", slotRef.Snap)
		}
		return err
	}

	plug := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
	if plug == nil {
		// conflict logic should prevent this
		return fmt.Errorf("snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}

	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		// conflict logic should prevent this
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	// attributes are always present, even if there are no hooks (they're initialized by Connect).
	plugDynamicAttrs, slotDynamicAttrs, err := getDynamicHookAttributes(task)
	if err != nil {
		return fmt.Errorf("failed to get hook attributes: %s", err)
	}

	var policyChecker interfaces.PolicyFunc

	// manual connections and connections by the gadget obey the
	// policy "connection" rules, other auto-connections obey the
	// "auto-connection" rules
	if autoConnect && !byGadget {
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
		ByGadget:         byGadget,
		HotplugDeviceKey: slot.HotplugDeviceKey,
	}
	setConns(st, conns)

	// the dynamic attributes might have been updated by the interface's BeforeConnectPlug/Slot code,
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

	cref := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}
	conn, ok := conns[cref.ID()]
	if !ok {
		return fmt.Errorf("internal error: connection %q not found in state", cref.ID())
	}

	// store old connection for undo
	task.Set("old-conn", conn)

	// "auto-disconnect" flag indicates it's a disconnect triggered automatically as part of snap removal;
	// such disconnects should not set undesired flag and instead just remove the connection.
	var autoDisconnect bool
	if err := task.Get("auto-disconnect", &autoDisconnect); err != nil && err != state.ErrNoState {
		return fmt.Errorf("internal error: failed to read 'auto-disconnect' flag: %s", err)
	}
	if conn.Auto && !autoDisconnect {
		conn.Undesired = true
		conn.DynamicPlugAttrs = nil
		conn.DynamicSlotAttrs = nil
		conn.StaticPlugAttrs = nil
		conn.StaticSlotAttrs = nil
		conns[cref.ID()] = conn
	} else {
		delete(conns, cref.ID())
	}
	setConns(st, conns)
	return nil
}

func (m *InterfaceManager) undoDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var oldconn connState
	err := task.Get("old-conn", &oldconn)
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

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		return err
	}
	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		return err
	}

	connRef := &interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	plug := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
	if plug == nil {
		return fmt.Errorf("snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}
	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	_, err = m.repo.Connect(connRef, oldconn.DynamicPlugAttrs, oldconn.DynamicSlotAttrs, nil)
	if err != nil {
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

	conns[connRef.ID()] = oldconn
	setConns(st, conns)

	return nil
}

func (m *InterfaceManager) undoConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	var oldconn connState
	err := task.Get("old-conn", &oldconn)
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

// timeout for shared content retry
var contentLinkRetryTimeout = 30 * time.Second

// defaultContentProviders returns a dict of the default-providers for the
// content plugs for the given snapName
func (m *InterfaceManager) defaultContentProviders(snapName string) map[string]bool {
	plugs := m.repo.Plugs(snapName)
	defaultProviders := make(map[string]bool, len(plugs))
	for _, plug := range plugs {
		if plug.Interface == "content" {
			var s string
			if err := plug.Attr("content", &s); err == nil && s != "" {
				var dprovider string
				if err := plug.Attr("default-provider", &dprovider); err == nil && dprovider != "" {
					defaultProviders[dprovider] = true
				}
			}
		}
	}
	return defaultProviders
}

func checkAutoconnectConflicts(st *state.State, plugSnap, slotSnap string) error {
	for _, task := range st.Tasks() {
		if task.Status().Ready() {
			continue
		}

		k := task.Kind()
		if k == "connect" || k == "disconnect" {
			// retry if we found another connect/disconnect affecting same snap; note we can only encounter
			// connects/disconnects created by doAutoDisconnect / doAutoConnect here as manual interface ops
			// are rejected by conflict check logic in snapstate.
			plugRef, slotRef, err := getPlugAndSlotRefs(task)
			if err != nil {
				return err
			}
			if plugRef.Snap == plugSnap || slotRef.Snap == slotSnap {
				return &state.Retry{After: connectRetryTimeout}
			}
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(task)
		// e.g. hook tasks don't have task snap setup
		if err != nil {
			continue
		}

		otherSnapName := snapsup.InstanceName()

		// different snaps - no conflict
		if otherSnapName != plugSnap && otherSnapName != slotSnap {
			continue
		}

		// other snap that affects us because of plug or slot
		if k == "unlink-snap" || k == "link-snap" || k == "setup-profiles" {
			// if snap is getting removed, we will retry but the snap will be gone and auto-connect becomes no-op
			// if snap is getting installed/refreshed - temporary conflict, retry later
			return &state.Retry{After: connectRetryTimeout}
		}
	}
	return nil
}

func checkDisconnectConflicts(st *state.State, disconnectingSnap, plugSnap, slotSnap string) error {
	for _, task := range st.Tasks() {
		if task.Status().Ready() {
			continue
		}

		k := task.Kind()
		if k == "connect" || k == "disconnect" {
			// retry if we found another connect/disconnect affecting same snap; note we can only encounter
			// connects/disconnects created by doAutoDisconnect / doAutoConnect here as manual interface ops
			// are rejected by conflict check logic in snapstate.
			plugRef, slotRef, err := getPlugAndSlotRefs(task)
			if err != nil {
				return err
			}
			if plugRef.Snap == plugSnap || slotRef.Snap == slotSnap {
				return &state.Retry{After: connectRetryTimeout}
			}
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(task)
		// e.g. hook tasks don't have task snap setup
		if err != nil {
			continue
		}

		otherSnapName := snapsup.InstanceName()

		// different snaps - no conflict
		if otherSnapName != plugSnap && otherSnapName != slotSnap {
			continue
		}

		// another task related to same snap op (unrelated op would be blocked by snapstate conflict logic)
		if otherSnapName == disconnectingSnap {
			continue
		}

		// note, don't care about unlink-snap for the opposite end. This relies
		// on the fact that auto-disconnect will create conflicting "disconnect" tasks that
		// we will retry with the logic above.
		if k == "link-snap" || k == "setup-profiles" {
			// other snap is getting installed/refreshed - temporary conflict
			return &state.Retry{After: connectRetryTimeout}
		}
	}
	return nil
}

// doAutoConnect creates task(s) to connect the given snap to viable candidates.
func (m *InterfaceManager) doAutoConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	// The previous task (link-snap) may have triggered a restart,
	// if this is the case we can only procceed once the restart
	// has happened or we may not have all the interfaces of the
	// new core/base snap.
	if err := snapstate.WaitRestart(task, snapsup); err != nil {
		return err
	}

	snapName := snapsup.InstanceName()

	autots := state.NewTaskSet()
	autochecker, err := newAutoConnectChecker(st)
	if err != nil {
		return err
	}

	// wait for auto-install, started by prerequisites code, for
	// the default-providers of content ifaces so we can
	// auto-connect to them
	defaultProviders := m.defaultContentProviders(snapName)
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		for _, t := range chg.Tasks() {
			if t.Status().Ready() {
				continue
			}
			if t.Kind() != "link-snap" && t.Kind() != "setup-profiles" {
				continue
			}
			if snapsup, err := snapstate.TaskSnapSetup(t); err == nil {
				if defaultProviders[snapsup.InstanceName()] {
					return &state.Retry{After: contentLinkRetryTimeout}
				}
			}
		}
	}

	plugs := m.repo.Plugs(snapName)
	slots := m.repo.Slots(snapName)
	newconns := make(map[string]*interfaces.ConnRef, len(plugs)+len(slots))

	// Auto-connect all the plugs
	for _, plug := range plugs {
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
			case candidates[0].Snap.InstanceName() == "ubuntu-core" && candidates[1].Snap.InstanceName() == "core":
				candidates = candidates[1:2]
			case candidates[1].Snap.InstanceName() == "ubuntu-core" && candidates[0].Snap.InstanceName() == "core":
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
			// Suggested connection already exist (or has Undesired flag set) so don't clobber it.
			// NOTE: we don't log anything here as this is a normal and common condition.
			continue
		}

		ignore, err := findSymmetricAutoconnectTask(st, plug.Snap.InstanceName(), slot.Snap.InstanceName(), task)
		if err != nil {
			return err
		}

		if ignore {
			continue
		}

		if err := checkAutoconnectConflicts(st, plug.Snap.InstanceName(), slot.Snap.InstanceName()); err != nil {
			if _, retry := err.(*state.Retry); retry {
				logger.Debugf("auto-connect of snap %q will be retried because of %q - %q conflict", snapName, plug.Snap.InstanceName(), slot.Snap.InstanceName())
				task.Logf("Waiting for conflicting change in progress...")
				return err // will retry
			}
			return fmt.Errorf("auto-connect conflict check failed: %s", err)
		}
		newconns[connRef.ID()] = connRef
	}
	// Auto-connect all the slots
	for _, slot := range slots {
		candidates := m.repo.AutoConnectCandidatePlugs(snapName, slot.Name, autochecker.check)
		if len(candidates) == 0 {
			continue
		}

		for _, plug := range candidates {
			// make sure slot is the only viable
			// connection for plug, same check as if we were
			// considering auto-connections from plug
			candSlots := m.repo.AutoConnectCandidateSlots(plug.Snap.InstanceName(), plug.Name, autochecker.check)

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
				// Suggested connection already exist (or has Undesired flag set) so don't clobber it.
				// NOTE: we don't log anything here as this is a normal and common condition.
				continue
			}
			if _, ok := newconns[key]; ok {
				continue
			}

			ignore, err := findSymmetricAutoconnectTask(st, plug.Snap.InstanceName(), slot.Snap.InstanceName(), task)
			if err != nil {
				return err
			}

			if ignore {
				continue
			}

			if err := checkAutoconnectConflicts(st, plug.Snap.InstanceName(), slot.Snap.InstanceName()); err != nil {
				if _, retry := err.(*state.Retry); retry {
					logger.Debugf("auto-connect of snap %q will be retried because of %q - %q conflict", snapName, plug.Snap.InstanceName(), slot.Snap.InstanceName())
					task.Logf("Waiting for conflicting change in progress...")
					return err // will retry
				}
				return fmt.Errorf("auto-connect conflict check failed: %s", err)
			}
			newconns[connRef.ID()] = connRef
		}
	}

	// Create connect tasks and interface hooks
	for _, conn := range newconns {
		ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: true})
		if err != nil {
			return fmt.Errorf("internal error: auto-connect of %q failed: %s", conn, err)
		}
		autots.AddAll(ts)
	}

	if len(autots.Tasks()) > 0 {
		snapstate.InjectTasks(task, autots)

		st.EnsureBefore(0)
	}

	task.SetStatus(state.DoneStatus)
	return nil
}

// doAutoDisconnect creates tasks for disconnecting all interfaces of a snap and running its interface hooks.
func (m *InterfaceManager) doAutoDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapName := snapsup.InstanceName()
	connections, err := m.repo.Connections(snapName)
	if err != nil {
		return err
	}

	// check for conflicts on all connections first before creating disconnect hooks
	for _, connRef := range connections {
		const auto = true
		if err := checkDisconnectConflicts(st, snapName, connRef.PlugRef.Snap, connRef.SlotRef.Snap); err != nil {
			if _, retry := err.(*state.Retry); retry {
				logger.Debugf("disconnecting interfaces of snap %q will be retried because of %q - %q conflict", snapName, connRef.PlugRef.Snap, connRef.SlotRef.Snap)
				task.Logf("Waiting for conflicting change in progress...")
				return err // will retry
			}
			return fmt.Errorf("cannot check conflicts when disconnecting interfaces: %s", err)
		}
	}

	hookTasks := state.NewTaskSet()
	for _, connRef := range connections {
		conn, err := m.repo.Connection(connRef)
		if err != nil {
			break
		}
		// "auto-disconnect" flag indicates it's a disconnect triggered as part of snap removal, in which
		// case we want to skip the logic of marking auto-connections as 'undesired' and instead just remove
		// them so they can be automatically connected if the snap is installed again.
		ts, err := disconnectTasks(st, conn, disconnectOpts{AutoDisconnect: true})
		if err != nil {
			return err
		}
		hookTasks.AddAll(ts)
	}

	snapstate.InjectTasks(task, hookTasks)

	// make sure that we add tasks and mark this task done in the same atomic write, otherwise there is a risk of re-adding tasks again
	task.SetStatus(state.DoneStatus)
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

// doGadgetConnect creates task(s) to follow the interface connection instructions from the gadget.
func (m *InterfaceManager) doGadgetConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	gconns, err := snapstate.GadgetConnections(st)
	if err != nil {
		return err
	}

	gconnts := state.NewTaskSet()
	var newconns []*interfaces.ConnRef

	// consider the gadget connect instructions
	for _, gconn := range gconns {
		plugSnapName, err := resolveSnapIDToName(st, gconn.Plug.SnapID)
		if err != nil {
			return err
		}
		plug := m.repo.Plug(plugSnapName, gconn.Plug.Plug)
		if plug == nil {
			task.Logf("gadget connect: ignoring missing plug %s:%s", gconn.Plug.SnapID, gconn.Plug.Plug)
			continue
		}

		slotSnapName, err := resolveSnapIDToName(st, gconn.Slot.SnapID)
		if err != nil {
			return err
		}
		slot := m.repo.Slot(slotSnapName, gconn.Slot.Slot)
		if slot == nil {
			task.Logf("gadget connect: ignoring missing slot %s:%s", gconn.Slot.SnapID, gconn.Slot.Slot)
			continue
		}

		connRef := interfaces.NewConnRef(plug, slot)
		key := connRef.ID()
		if _, ok := conns[key]; ok {
			// Gadget connection already exist (or has Undesired flag set) so don't clobber it.
			continue
		}

		if err := checkAutoconnectConflicts(st, plug.Snap.InstanceName(), slot.Snap.InstanceName()); err != nil {
			if _, retry := err.(*state.Retry); retry {
				task.Logf("gadget connect will be retried because of %q - %q conflict", plug.Snap.InstanceName(), slot.Snap.InstanceName())
				return err // will retry
			}
			return fmt.Errorf("gadget connect conflict check failed: %s", err)
		}
		newconns = append(newconns, connRef)
	}

	// Create connect tasks and interface hooks
	for _, conn := range newconns {
		ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: true, ByGadget: true})
		if err != nil {
			return fmt.Errorf("internal error: connect of %q failed: %s", conn, err)
		}
		gconnts.AddAll(ts)
	}

	if len(gconnts.Tasks()) > 0 {
		snapstate.InjectTasks(task, gconnts)

		st.EnsureBefore(0)
	}

	task.SetStatus(state.DoneStatus)
	return nil
}

// doHotplugConnect creates task(s) to (re)create connections in response to hotplug "add" event.
func (m *InterfaceManager) doHotplugConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	coreSnapName, err := m.repo.GuessSystemSnapName()
	if err != nil {
		return err
	}

	deviceKey, ifaceName, err := hotplugTaskGetAttrs(task)
	if err != nil {
		return err
	}

	// find old connections for slots of this device - note we can't ask the repository since we need
	// to recreate old connections that are only remembered in the state.
	connsForDevice := findConnsForDeviceKey(&conns, coreSnapName, ifaceName, deviceKey)

	// we see this device for the first time (or it didn't have any connected slots before)
	if len(connsForDevice) == 0 {
		slots, err := m.repo.SlotsForDeviceKey(deviceKey, ifaceName)
		if err != nil {
			return err
		}

		autochecker, err := newAutoConnectChecker(st)
		if err != nil {
			return err
		}

		var newconns []*interfaces.ConnRef
		// Auto-connect the slots
		for _, slot := range slots {
			snapName := slot.Snap.InstanceName()
			candidates := m.repo.AutoConnectCandidatePlugs(snapName, slot.Name, autochecker.check)
			if len(candidates) == 0 {
				continue
			}

			for _, plug := range candidates {
				// make sure slot is the only viable
				// connection for plug, same check as if we were
				// considering auto-connections from plug
				candSlots := m.repo.AutoConnectCandidateSlots(plug.Snap.InstanceName(), plug.Name, autochecker.check)

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
					// Suggested connection already exist (or has Undesired flag set) so don't clobber it.
					// NOTE: we don't log anything here as this is a normal and common condition.
					continue
				}

				ignore, err := findSymmetricAutoconnectTask(st, plug.Snap.InstanceName(), slot.Snap.InstanceName(), task)
				if err != nil {
					return err
				}

				if ignore {
					continue
				}

				if err := checkAutoconnectConflicts(st, plug.Snap.InstanceName(), slot.Snap.InstanceName()); err != nil {
					if _, retry := err.(*state.Retry); retry {
						logger.Debugf("auto-connect of snap %q will be retried because of %q - %q conflict", snapName, plug.Snap.InstanceName(), slot.Snap.InstanceName())
						task.Logf("Waiting for conflicting change in progress...")
						return err // will retry
					}
					return fmt.Errorf("auto-connect conflict check failed: %s", err)
				}
				newconns = append(newconns, connRef)
			}
		}

		autots := state.NewTaskSet()
		// Create connect tasks and interface hooks
		for _, conn := range newconns {
			ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: true})
			if err != nil {
				return fmt.Errorf("internal error: auto-connect of %q failed: %s", conn, err)
			}
			autots.AddAll(ts)
		}

		if len(autots.Tasks()) > 0 {
			snapstate.InjectTasks(task, autots)

			st.EnsureBefore(0)
		}

		task.SetStatus(state.DoneStatus)
		return nil
	}

	// recreate old connections for the device.
	var recreate []string
	for _, id := range connsForDevice {
		conn := conns[id]
		// the device was unplugged while connected, so it had disconnect hooks run; recreate the connection from scratch.
		if conn.HotplugRemoved {
			recreate = append(recreate, id)
		} else {
			// TODO: we have never observed remove event for this device: check if any attributes of the slot changed and if so,
			// disconnect and connect again. if attributes haven't changed, there is nothing to do.
		}
	}

	var newconns []*interfaces.ConnRef
	for _, id := range recreate {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}

		if err := checkAutoconnectConflicts(st, connRef.PlugRef.Snap, connRef.SlotRef.Snap); err != nil {
			if _, retry := err.(*state.Retry); retry {
				task.Logf("hotplug connect will be retried because of %q - %q conflict", connRef.PlugRef.Snap, connRef.SlotRef.Snap)
				return err // will retry
			}
			return fmt.Errorf("hotplug connect conflict check failed: %s", err)
		}
		newconns = append(newconns, connRef)
	}

	// Create connect tasks and interface hooks
	if len(newconns) > 0 {
		ts := state.NewTaskSet()
		for _, conn := range newconns {
			ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: true, ByHotplug: true})
			if err != nil {
				return fmt.Errorf("internal error: connect of %q failed: %s", conn, err)
			}
			ts.AddAll(ts)
		}

		if len(ts.Tasks()) > 0 {
			snapstate.InjectTasks(task, ts)
			st.EnsureBefore(0)
		}

		// make sure that we add tasks and mark this task done in the same atomic write, otherwise there is a risk of re-adding tasks again
		task.SetStatus(state.DoneStatus)
	}

	return nil
}

// doHotplugDisconnect creates task(s) to disconnect connections and remove slots in response to hotplug "remove" event.
func (m *InterfaceManager) doHotplugDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	coreSnapName, err := m.repo.GuessSystemSnapName()
	if err != nil {
		return err
	}

	deviceKey, ifaceName, err := hotplugTaskGetAttrs(task)
	if err != nil {
		return err
	}

	connections, err := m.repo.ConnectionsForDeviceKey(deviceKey, ifaceName)
	if err != nil {
		return err
	}

	// check for conflicts on all connections first before creating disconnect hooks
	for _, connRef := range connections {
		const auto = true
		if err := checkDisconnectConflicts(st, coreSnapName, connRef.PlugRef.Snap, connRef.SlotRef.Snap); err != nil {
			if _, retry := err.(*state.Retry); retry {
				logger.Debugf("disconnecting interfaces of snap %q will be retried because of %q - %q conflict", coreSnapName, connRef.PlugRef.Snap, connRef.SlotRef.Snap)
				task.Logf("Waiting for conflicting change in progress...")
				return err // will retry
			}
			return fmt.Errorf("cannot check conflicts when disconnecting interfaces: %s", err)
		}
	}

	if len(connections) > 0 {
		dts := state.NewTaskSet()
		for _, connRef := range connections {
			conn, err := m.repo.Connection(connRef)
			if err != nil {
				break
			}
			// "auto-disconnect" flag indicates it's a disconnect triggered as part of hotplug removal, in which
			// case we want to skip the logic of marking auto-connections as 'undesired' and instead just remove
			// them so they can be automatically connected if the snap is installed again.
			ts, err := disconnectTasks(st, conn, disconnectOpts{AutoDisconnect: true})
			if err != nil {
				return err
			}
			dts.AddAll(ts)
		}

		snapstate.InjectTasks(task, dts)

		// make sure that we add tasks and mark this task done in the same atomic write, otherwise there is a risk of re-adding tasks again
		task.SetStatus(state.DoneStatus)
	}
	return nil
}

// doHotplugRemoveSlots removes all slots of given hotplug device and interface from the repository.
// Note, this task must necessarily be run after all affected slots get disconnected.
func (m *InterfaceManager) doHotplugRemoveSlots(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	deviceKey, ifaceName, err := hotplugTaskGetAttrs(task)
	if err != nil {
		return err
	}

	stateSlots, err := getHotplugSlots(st)
	if err != nil {
		return err
	}

	slots, err := m.repo.SlotsForDeviceKey(deviceKey, ifaceName)
	if err != nil {
		return fmt.Errorf("cannot determine slots: %s", err)
	}

	for _, slot := range slots {
		if err := m.repo.RemoveSlot(slot.Snap.InstanceName(), slot.Name); err != nil {
			return fmt.Errorf("cannot remove slot %s of snap %q: %s", slot.Snap.InstanceName(), slot.Name, err)
		}
		delete(stateSlots, slot.Name)
	}

	setHotplugSlots(st, stateSlots)

	return nil
}
