// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/schema"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/timings"
)

var snapstateFinishRestart = snapstate.FinishRestart

// journalQuotaLayout returns the necessary journal quota mount layouts
// to mimick what systemd does for services with log namespaces.
func journalQuotaLayout(quotaGroup *quota.Group) []snap.Layout {
	if quotaGroup.JournalLimit == nil {
		return nil
	}

	// bind mount the journal namespace folder on top of the journal folder
	// /run/systemd/journal.<ns> -> /run/systemd/journal
	layouts := []snap.Layout{{
		Bind: path.Join(dirs.SnapSystemdRunDir, fmt.Sprintf("journal.%s", quotaGroup.JournalNamespaceName())),
		Path: path.Join(dirs.SnapSystemdRunDir, "journal"),
		Mode: 0755,
	}}
	return layouts
}

// getExtraLayouts helper function to dynamically calculate the extra mount layouts for
// a snap instance. These are the layouts which can change during the lifetime of a snap
// like for instance mimicking systemd journal namespace mount layouts.
func getExtraLayouts(st *state.State, snapInfo *snap.Info) ([]snap.Layout, error) {
	snapOpts, err := servicestate.SnapServiceOptions(st, snapInfo, nil)
	if err != nil {
		return nil, err
	}

	var extraLayouts []snap.Layout
	if snapOpts.QuotaGroup != nil {
		extraLayouts = append(extraLayouts, journalQuotaLayout(snapOpts.QuotaGroup)...)
	}

	return extraLayouts, nil
}

func (m *InterfaceManager) buildConfinementOptions(st *state.State, snapInfo *snap.Info, flags snapstate.Flags) (interfaces.ConfinementOptions, error) {
	extraLayouts, err := getExtraLayouts(st, snapInfo)
	if err != nil {
		return interfaces.ConfinementOptions{}, fmt.Errorf("cannot get extra mount layouts of snap %q: %s", snapInfo.InstanceName(), err)
	}

	return interfaces.ConfinementOptions{
		DevMode:           flags.DevMode,
		JailMode:          flags.JailMode,
		Classic:           flags.Classic,
		ExtraLayouts:      extraLayouts,
		AppArmorPrompting: m.useAppArmorPrompting,
	}, nil
}

func (m *InterfaceManager) setupAffectedSnaps(task *state.Task, affectingSnap string, affectedSnaps []string, tm timings.Measurer) error {
	st := task.State()

	// Setup security of the affected snaps.
	for _, affectedInstanceName := range affectedSnaps {
		// the snap that triggered the change needs to be skipped
		if affectedInstanceName == affectingSnap {
			continue
		}
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, affectedInstanceName, &snapst); err != nil {
			task.Errorf("skipping security profiles setup for snap %q when handling snap %q: %v", affectedInstanceName, affectingSnap, err)
			continue
		}
		affectedSnapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		if err := addImplicitSlots(st, affectedSnapInfo); err != nil {
			return err
		}

		affectedAppSet, err := appSetForSnapRevision(st, affectedSnapInfo)
		if err != nil {
			return fmt.Errorf("building app set for snap %q: %v", affectingSnap, err)
		}

		opts, err := m.buildConfinementOptions(st, affectedSnapInfo, snapst.Flags)
		if err != nil {
			return err
		}
		if err := m.setupSnapSecurity(task, affectedAppSet, opts, tm); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) doSetupProfiles(task *state.Task, tomb *tomb.Tomb) error {
	task.State().Lock()
	defer task.State().Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(task.State())

	// Get snap.Info from bits handed by the snap manager.
	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	snapInfo, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	if len(snapInfo.BadInterfaces) > 0 {
		task.State().Warnf("%s", snap.BadInterfacesSummary(snapInfo))
	}

	// We no longer do/need core-phase-2, see
	//   https://github.com/snapcore/snapd/pull/5301
	// This code is just here to deal with old state that may still
	// have the 2nd setup-profiles with this flag set.
	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if corePhase2 {
		// nothing to do
		return nil
	}

	opts, err := m.buildConfinementOptions(task.State(), snapInfo, snapsup.Flags)
	if err != nil {
		return err
	}

	if err := addImplicitSlots(task.State(), snapInfo); err != nil {
		return err
	}

	// this app set is derived from the current task, which will include any
	// components that are already installed, with the addition of any new
	// components that are getting setup up by this task
	appSet, err := appSetForTask(task, snapInfo)
	if err != nil {
		return err
	}

	if err := m.setupProfilesForAppSet(task, appSet, opts, perfTimings); err != nil {
		return err
	}
	return setPendingProfilesSideInfo(task.State(), snapsup.InstanceName(), appSet)
}

// setupPendingProfilesSideInfo helps updating information about any
// revision for which security profiles are set up while the snap is
// not yet active.
func setPendingProfilesSideInfo(st *state.State, instanceName string, appSet *interfaces.SnapAppSet) error {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, instanceName, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !snapst.IsInstalled() {
		// not yet visible to the rest of the system, nothing to do here
		return nil
	}
	if snapst.Active {
		// nothing is pending
		return nil
	}

	if appSet != nil {
		csis := make([]*snap.ComponentSideInfo, 0, len(appSet.Components()))
		for _, ci := range appSet.Components() {
			csis = append(csis, &ci.ComponentSideInfo)
		}

		snapst.PendingSecurity = &snapstate.PendingSecurityState{
			SideInfo:   &appSet.Info().SideInfo,
			Components: csis,
		}
	} else {
		snapst.PendingSecurity = &snapstate.PendingSecurityState{}
	}

	snapstate.Set(st, instanceName, &snapst)
	return nil
}

func (m *InterfaceManager) setupProfilesForAppSet(task *state.Task, appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, tm timings.Measurer) error {
	st := task.State()

	snapInfo := appSet.Info()
	snapName := appSet.InstanceName()

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
	if err := m.repo.AddAppSet(appSet); err != nil {
		return err
	}
	if len(snapInfo.BadInterfaces) > 0 {
		task.Logf("%s", snap.BadInterfacesSummary(snapInfo))
	}

	// Reload the connections and compute the set of affected snaps. The set
	// affectedSet set contains name of all the affected snap instances.  The
	// arrays affectedNames and affectedSnaps contain, arrays of snap names and
	// snapInfo's, respectively. The arrays are sorted by name with the special
	// exception that the snap being setup is always first. The affectedSnaps
	// array may be shorter than the set of affected snaps in case any of the
	// snaps cannot be found in the state.
	reconnectedSnaps, err := m.reloadConnections(snapName)
	if err != nil {
		return err
	}
	affectedSet := make(map[string]bool)
	for _, name := range disconnectedSnaps {
		affectedSet[name] = true
	}
	for _, name := range reconnectedSnaps {
		affectedSet[name] = true
	}

	// Sort the set of affected names, ensuring that the snap being setup
	// is first regardless of the name it has.
	affectedNames := make([]string, 0, len(affectedSet))
	for name := range affectedSet {
		if name != snapName {
			affectedNames = append(affectedNames, name)
		}
	}
	sort.Strings(affectedNames)
	affectedNames = append([]string{snapName}, affectedNames...)

	// Obtain interfaces.SnapAppSet for each affected snap, skipping those that
	// cannot be found and compute the confinement options that apply to it.
	affectedSnapSets := make([]*interfaces.SnapAppSet, 0, len(affectedSet))
	confinementOpts := make([]interfaces.ConfinementOptions, 0, len(affectedSet))

	// For the snap being setup we know exactly what was requested.
	affectedSnapSets = append(affectedSnapSets, appSet)
	confinementOpts = append(confinementOpts, opts)

	// For remaining snaps we need to interrogate the state.
	for _, name := range affectedNames[1:] {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, name, &snapst); err != nil {
			task.Errorf("cannot obtain state of snap %s: %s", name, err)
			continue
		}
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}
		if err := addImplicitSlots(st, snapInfo); err != nil {
			return err
		}

		appSet, err := appSetForSnapRevision(st, snapInfo)
		if err != nil {
			return fmt.Errorf("building app set for snap %q: %v", name, err)
		}

		opts, err := m.buildConfinementOptions(st, snapInfo, snapst.Flags)
		if err != nil {
			return err
		}

		affectedSnapSets = append(affectedSnapSets, appSet)
		confinementOpts = append(confinementOpts, opts)
	}

	return m.setupSecurityByBackend(task, affectedSnapSets, confinementOpts, tm)
}

func (m *InterfaceManager) doRemoveProfiles(task *state.Task, tomb *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	// Get SnapSetup for this snap. This is gives us the name of the snap.
	snapSetup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}
	snapName := snapSetup.InstanceName()

	if err := m.removeProfilesForSnap(task, tomb, snapName, perfTimings); err != nil {
		return err
	}

	// no pending profiles on disk
	return setPendingProfilesSideInfo(task.State(), snapName, nil)
}

func (m *InterfaceManager) removeProfilesForSnap(task *state.Task, _ *tomb.Tomb, snapName string, tm timings.Measurer) error {
	// Disconnect the snap entirely.
	// This is required to remove the snap from the interface repository.
	// The returned list of affected snaps will need to have its security setup
	// to reflect the change.
	affectedSnaps, err := m.repo.DisconnectSnap(snapName)
	if err != nil {
		return err
	}
	if err := m.setupAffectedSnaps(task, snapName, affectedSnaps, tm); err != nil {
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

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && !errors.Is(err, state.ErrNoState) {
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
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	sideInfo := snapst.CurrentSideInfo()
	if sideInfo == nil {
		// The snap was not installed before so undo should remove security profiles.
		return m.removeProfilesForSnap(task, tomb, snapName, perfTimings)
	} else {
		// The snap was installed before so undo should setup the old security profiles.
		snapInfo, err := snap.ReadInfo(snapName, sideInfo)
		if err != nil {
			return err
		}
		opts, err := m.buildConfinementOptions(task.State(), snapInfo, snapst.Flags)
		if err != nil {
			return err
		}

		if err := addImplicitSlots(st, snapInfo); err != nil {
			return err
		}

		// this app set is derived from the currently installed revision of the
		// snap (not the revision that we are reverting from). it only includes
		// components that were installed with that revision.
		appSet, err := appSetForSnapRevision(st, snapInfo)
		if err != nil {
			return err
		}

		if err := m.setupProfilesForAppSet(task, appSet, opts, perfTimings); err != nil {
			return err
		}
		return setPendingProfilesSideInfo(task.State(), snapName, appSet)
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

	instanceName := snapSetup.InstanceName()

	var snapst snapstate.SnapState
	err = snapstate.Get(st, instanceName, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if err == nil && len(snapst.Sequence.Revisions) != 0 {
		return fmt.Errorf("cannot discard connections for snap %q while it is present", instanceName)
	}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	removed := make(map[string]*schema.ConnState)
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		if connRef.PlugRef.Snap == instanceName || connRef.SlotRef.Snap == instanceName {
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

	var removed map[string]*schema.ConnState
	err := task.Get("removed", &removed)
	if err != nil && !errors.Is(err, state.ErrNoState) {
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
	if err = task.Get("plug-dynamic", &plugAttrs); err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}
	if err = task.Get("slot-dynamic", &slotAttrs); err != nil && !errors.Is(err, state.ErrNoState) {
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

func (m *InterfaceManager) doConnect(task *state.Task, _ *tomb.Tomb) (err error) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	var autoConnect bool
	if err := task.Get("auto", &autoConnect); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	var byGadget bool
	if err := task.Get("by-gadget", &byGadget); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	var delayedSetupProfiles bool
	if err := task.Get("delayed-setup-profiles", &delayedSetupProfiles); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	deviceCtx, err := snapstate.DeviceCtx(st, task, nil)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	connRef := &interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	var plugSnapst snapstate.SnapState
	if err := snapstate.Get(st, plugRef.Snap, &plugSnapst); err != nil {
		if autoConnect && errors.Is(err, state.ErrNoState) {
			// conflict logic should prevent this
			return fmt.Errorf("internal error: snap %q is no longer available for auto-connecting", plugRef.Snap)
		}
		return err
	}

	var slotSnapst snapstate.SnapState
	if err := snapstate.Get(st, slotRef.Snap, &slotSnapst); err != nil {
		if autoConnect && errors.Is(err, state.ErrNoState) {
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

	plugAppSet, err := appSetForSnapRevision(st, plug.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", plug.Snap.InstanceName(), err)
	}

	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		// conflict logic should prevent this
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	slotAppSet, err := appSetForSnapRevision(st, slot.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", slot.Snap.InstanceName(), err)
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
		autochecker, err := newAutoConnectChecker(st, m.repo, deviceCtx)
		if err != nil {
			return err
		}
		policyChecker = func(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, error) {
			ok, _, err := autochecker.check(plug, slot)
			return ok, err
		}
	} else {
		policyCheck, err := newConnectChecker(st, deviceCtx)
		if err != nil {
			return err
		}
		policyChecker = policyCheck.check
	}

	// static attributes of the plug and slot not provided, the ones from snap infos will be used
	conn, err := m.repo.Connect(connRef, nil, plugDynamicAttrs, nil, slotDynamicAttrs, policyChecker)
	if err != nil || conn == nil {
		return err
	}
	defer func() {
		if err != nil {
			if err := m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name); err != nil {
				logger.Noticef("cannot undo failed connection: %v", err)
			}
		}
	}()

	if !delayedSetupProfiles {
		slotSnapInfo, err := slotSnapst.CurrentInfo()
		if err != nil {
			return err
		}
		slotOpts, err := m.buildConfinementOptions(st, slotSnapInfo, slotSnapst.Flags)
		if err != nil {
			return err
		}
		if err := m.setupSnapSecurity(task, slotAppSet, slotOpts, perfTimings); err != nil {
			return err
		}

		plugSnapInfo, err := plugSnapst.CurrentInfo()
		if err != nil {
			return err
		}
		plugOpts, err := m.buildConfinementOptions(st, plugSnapInfo, plugSnapst.Flags)
		if err != nil {
			return err
		}
		if err := m.setupSnapSecurity(task, plugAppSet, plugOpts, perfTimings); err != nil {
			return err
		}
	} else {
		logger.Debugf("Connect handler: skipping setupSnapSecurity for snaps %q and %q", plug.Snap.InstanceName(), slot.Snap.InstanceName())
	}

	// For undo handler. We need to remember old state of the connection only
	// if undesired flag is set because that means there was a remembered
	// inactive connection already and we should restore its properties
	// in case of undo. Otherwise we don't have to keep old-conn because undo
	// can simply delete any trace of the connection.
	if old, ok := conns[connRef.ID()]; ok && old.Undesired {
		task.Set("old-conn", old)
	}

	conns[connRef.ID()] = &schema.ConnState{
		Interface:        conn.Interface(),
		StaticPlugAttrs:  conn.Plug.StaticAttrs(),
		DynamicPlugAttrs: conn.Plug.DynamicAttrs(),
		StaticSlotAttrs:  conn.Slot.StaticAttrs(),
		DynamicSlotAttrs: conn.Slot.DynamicAttrs(),
		Auto:             autoConnect,
		ByGadget:         byGadget,
		HotplugKey:       slot.HotplugKey,
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

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}

	cref := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	// forget flag can be passed with snap disconnect --forget
	var forget bool
	if err := task.Get("forget", &forget); err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot read 'forget' flag: %s", err)
	}

	var snapStates []snapstate.SnapState
	for _, instanceName := range []string{plugRef.Snap, slotRef.Snap} {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, instanceName, &snapst); err != nil {
			if errors.Is(err, state.ErrNoState) {
				task.Logf("skipping disconnect operation for connection %s %s, snap %q doesn't exist", plugRef, slotRef, instanceName)
				return nil
			}
			task.Errorf("skipping security profiles setup for snap %q when disconnecting %s from %s: %v", instanceName, plugRef, slotRef, err)
		} else {
			snapStates = append(snapStates, snapst)
		}
	}

	conn, ok := conns[cref.ID()]
	if !ok {
		return fmt.Errorf("internal error: connection %q not found in state", cref.ID())
	}

	// store old connection for undo
	task.Set("old-conn", conn)

	err = m.repo.Disconnect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
	if err != nil {
		_, notConnected := err.(*interfaces.NotConnectedError)
		_, noPlugOrSlot := err.(*interfaces.NoPlugOrSlotError)
		// not connected, just forget it.
		if forget && (notConnected || noPlugOrSlot) {
			delete(conns, cref.ID())
			setConns(st, conns)
			return nil
		}
		return fmt.Errorf("snapd changed, please retry the operation: %v", err)
	}

	for _, snapst := range snapStates {
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			return err
		}

		appSet, err := appSetForSnapRevision(st, snapInfo)
		if err != nil {
			return fmt.Errorf("building app set for snap %q: %v", snapInfo.InstanceName(), err)
		}

		opts, err := m.buildConfinementOptions(st, snapInfo, snapst.Flags)
		if err != nil {
			return err
		}
		if err := m.setupSnapSecurity(task, appSet, opts, perfTimings); err != nil {
			return err
		}
	}

	// "auto-disconnect" flag indicates it's a disconnect triggered automatically as part of snap removal;
	// such disconnects should not set undesired flag and instead just remove the connection.
	var autoDisconnect bool
	if err := task.Get("auto-disconnect", &autoDisconnect); err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: failed to read 'auto-disconnect' flag: %s", err)
	}

	// "by-hotplug" flag indicates it's a disconnect triggered by hotplug remove event;
	// we want to keep information of the connection and just mark it as hotplug-gone.
	var byHotplug bool
	if err := task.Get("by-hotplug", &byHotplug); err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot read 'by-hotplug' flag: %s", err)
	}

	switch {
	case forget:
		delete(conns, cref.ID())
	case byHotplug:
		conn.HotplugGone = true
		conns[cref.ID()] = conn
	case conn.Auto && !autoDisconnect:
		conn.Undesired = true
		conn.DynamicPlugAttrs = nil
		conn.DynamicSlotAttrs = nil
		conn.StaticPlugAttrs = nil
		conn.StaticSlotAttrs = nil
		conns[cref.ID()] = conn
	default:
		delete(conns, cref.ID())
	}
	setConns(st, conns)

	return nil
}

func (m *InterfaceManager) undoDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	var oldconn schema.ConnState
	err := task.Get("old-conn", &oldconn)
	if errors.Is(err, state.ErrNoState) {
		return nil
	}
	if err != nil {
		return err
	}

	var forget bool
	if err := task.Get("forget", &forget); err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot read 'forget' flag: %s", err)
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
	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)

	if forget && (plug == nil || slot == nil) {
		// we were trying to forget an inactive connection that was
		// referring to a non-existing plug or slot; just restore it
		// in the conns state but do not reconnect via repository.
		conns[connRef.ID()] = &oldconn
		setConns(st, conns)
		return nil
	}
	if plug == nil {
		return fmt.Errorf("snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}
	if slot == nil {
		return fmt.Errorf("snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	plugAppSet, err := appSetForSnapRevision(st, plug.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", plug.Snap.InstanceName(), err)
	}

	slotAppSet, err := appSetForSnapRevision(st, slot.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", slot.Snap.InstanceName(), err)
	}

	_, err = m.repo.Connect(connRef, nil, oldconn.DynamicPlugAttrs, nil, oldconn.DynamicSlotAttrs, nil)
	if err != nil {
		return err
	}

	slotSnapInfo, err := slotSnapst.CurrentInfo()
	if err != nil {
		return err
	}
	slotOpts, err := m.buildConfinementOptions(st, slotSnapInfo, slotSnapst.Flags)
	if err != nil {
		return err
	}
	if err := m.setupSnapSecurity(task, slotAppSet, slotOpts, perfTimings); err != nil {
		return err
	}

	plugSnapInfo, err := plugSnapst.CurrentInfo()
	if err != nil {
		return err
	}
	plugOpts, err := m.buildConfinementOptions(st, plugSnapInfo, plugSnapst.Flags)
	if err != nil {
		return err
	}
	if err := m.setupSnapSecurity(task, plugAppSet, plugOpts, perfTimings); err != nil {
		return err
	}

	conns[connRef.ID()] = &oldconn
	setConns(st, conns)

	return nil
}

func (m *InterfaceManager) undoConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	perfTimings := state.TimingsForTask(task)
	defer perfTimings.Save(st)

	plugRef, slotRef, err := getPlugAndSlotRefs(task)
	if err != nil {
		return err
	}
	connRef := interfaces.ConnRef{PlugRef: plugRef, SlotRef: slotRef}
	conns, err := getConns(st)
	if err != nil {
		return err
	}

	var old schema.ConnState
	err = task.Get("old-conn", &old)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if err == nil {
		conns[connRef.ID()] = &old
	} else {
		delete(conns, connRef.ID())
	}
	setConns(st, conns)

	if err := m.repo.Disconnect(connRef.PlugRef.Snap, connRef.PlugRef.Name, connRef.SlotRef.Snap, connRef.SlotRef.Name); err != nil {
		return err
	}

	var delayedSetupProfiles bool
	if err := task.Get("delayed-setup-profiles", &delayedSetupProfiles); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if delayedSetupProfiles {
		logger.Debugf("Connect undo handler: skipping setupSnapSecurity for snaps %q and %q", connRef.PlugRef.Snap, connRef.SlotRef.Snap)
		return nil
	}

	plug := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
	if plug == nil {
		return fmt.Errorf("internal error: snap %q has no %q plug", connRef.PlugRef.Snap, connRef.PlugRef.Name)
	}

	plugAppSet, err := appSetForSnapRevision(st, plug.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", plug.Snap.InstanceName(), err)
	}

	slot := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)
	if slot == nil {
		return fmt.Errorf("internal error: snap %q has no %q slot", connRef.SlotRef.Snap, connRef.SlotRef.Name)
	}

	slotAppSet, err := appSetForSnapRevision(st, slot.Snap)
	if err != nil {
		return fmt.Errorf("building app set for snap %q: %v", slot.Snap.InstanceName(), err)
	}

	var plugSnapst snapstate.SnapState
	err = snapstate.Get(st, plugRef.Snap, &plugSnapst)
	if errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: snap %q is no longer available", plugRef.Snap)
	}
	if err != nil {
		return err
	}
	var slotSnapst snapstate.SnapState
	err = snapstate.Get(st, slotRef.Snap, &slotSnapst)
	if errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: snap %q is no longer available", slotRef.Snap)
	}
	if err != nil {
		return err
	}

	slotSnapInfo, err := slotSnapst.CurrentInfo()
	if err != nil {
		return err
	}
	slotOpts, err := m.buildConfinementOptions(st, slotSnapInfo, slotSnapst.Flags)
	if err != nil {
		return err
	}
	if err := m.setupSnapSecurity(task, slotAppSet, slotOpts, perfTimings); err != nil {
		return err
	}

	plugSnapInfo, err := plugSnapst.CurrentInfo()
	if err != nil {
		return err
	}
	plugOpts, err := m.buildConfinementOptions(st, plugSnapInfo, plugSnapst.Flags)
	if err != nil {
		return err
	}
	if err := m.setupSnapSecurity(task, plugAppSet, plugOpts, perfTimings); err != nil {
		return err
	}

	return nil
}

// timeout for shared content retry
var contentLinkRetryTimeout = 30 * time.Second

// timeout for retrying hotplug-related tasks
var hotplugRetryTimeout = 300 * time.Millisecond

func obsoleteCorePhase2SetupProfiles(kind string, task *state.Task) (bool, error) {
	if kind != "setup-profiles" {
		return false, nil
	}

	var corePhase2 bool
	if err := task.Get("core-phase-2", &corePhase2); err != nil && !errors.Is(err, state.ErrNoState) {
		return false, err
	}
	return corePhase2, nil
}

func checkAutoconnectConflicts(st *state.State, autoconnectTask *state.Task, plugSnap, slotSnap string) error {
	for _, task := range st.Tasks() {
		if task.Status().Ready() {
			continue
		}

		k := task.Kind()
		if k == "connect" || k == "disconnect" {
			// if the task depends on the auto-connect in some way, then we assume that it is safe to go ahead
			// as it was scheduled by that exact task, and we should not block for that.
			if inSameChangeWaitChain(autoconnectTask, task) {
				continue
			}

			// retry if we found another connect/disconnect affecting same snap; note we can only encounter
			// connects/disconnects created by doAutoDisconnect / doAutoConnect here as manual interface ops
			// are rejected by conflict check logic in snapstate.
			plugRef, slotRef, err := getPlugAndSlotRefs(task)
			if err != nil {
				return err
			}
			if plugRef.Snap == plugSnap {
				return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting plug snap %s, task %q", plugSnap, k)}
			}
			if slotRef.Snap == slotSnap {
				return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting slot snap %s, task %q", slotSnap, k)}
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

		// setup-profiles core-phase-2 is now no-op, we shouldn't
		// conflict on it; note, old snapd would create this task even
		// for regular snaps if installed with the dangerous flag.
		obsoleteCorePhase2, err := obsoleteCorePhase2SetupProfiles(k, task)
		if err != nil {
			return err
		}
		if obsoleteCorePhase2 {
			continue
		}

		// other snap that affects us because of plug or slot
		if k == "unlink-snap" || k == "link-snap" || k == "setup-profiles" || k == "discard-snap" {
			// discard-snap is scheduled as part of garbage collection during refresh, if multiple revsions are already installed.
			// this revision check avoids conflict with own discard tasks created as part of install/refresh.
			if k == "discard-snap" && autoconnectTask.Change() != nil && autoconnectTask.Change().ID() == task.Change().ID() {
				continue
			}

			// setup-profiles will be scheduled when we schedule the connect hooks during pre-seed, and they are postponed until
			// after pre-seeding. They will still cause a conflict here, so take that into account.
			if k == "setup-profiles" && inSameChangeWaitChain(autoconnectTask, task) {
				continue
			}

			// if snap is getting removed, we will retry but the snap will be gone and auto-connect becomes no-op
			// if snap is getting installed/refreshed - temporary conflict, retry later
			return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting snap %s with task %q", otherSnapName, k)}
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

func checkHotplugDisconnectConflicts(st *state.State, plugSnap, slotSnap string) error {
	for _, task := range st.Tasks() {
		if task.Status().Ready() {
			continue
		}

		k := task.Kind()
		if k == "connect" || k == "disconnect" {
			plugRef, slotRef, err := getPlugAndSlotRefs(task)
			if err != nil {
				return err
			}
			if plugRef.Snap == plugSnap {
				return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting plug snap %s, task %q", plugSnap, k)}
			}
			if slotRef.Snap == slotSnap {
				return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting slot snap %s, task %q", slotSnap, k)}
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

		if k == "link-snap" || k == "setup-profiles" || k == "unlink-snap" {
			// other snap is getting installed/refreshed/removed - temporary conflict
			return &state.Retry{After: connectRetryTimeout, Reason: fmt.Sprintf("conflicting snap %s with task %q", otherSnapName, k)}
		}
	}
	return nil
}

// inSameChangeWaitChains returns true if there is a wait chain so
// that `startT` is run before `searchT` in the same state.Change.
func inSameChangeWaitChain(startT, searchT *state.Task) bool {
	// Trivial case, tasks in different changes (they could in theory
	// still have cross-change waits but we don't do these today).
	// In this case, return quickly.
	if startT.Change() != searchT.Change() {
		return false
	}
	seenTasks := make(map[string]bool)
	// Do a recursive check if its in the same change
	return waitChainSearch(startT, searchT, seenTasks)
}

func waitChainSearch(startT, searchT *state.Task, seenTasks map[string]bool) bool {
	if seenTasks[startT.ID()] {
		return false
	}
	seenTasks[startT.ID()] = true
	for _, cand := range startT.HaltTasks() {
		if cand == searchT {
			return true
		}
		if waitChainSearch(cand, searchT, seenTasks) {
			return true
		}
	}

	return false
}

// batchConnectTasks creates connect tasks and interface hooks for
// conns and sets their wait chain with regard to the setupProfiles
// task.
//
// The tasks are chained so that: - prepare-plug-, prepare-slot- and
// connect tasks are all executed before setup-profiles -
// connect-plug-, connect-slot- are all executed after setup-profiles.
// The "delayed-setup-profiles" flag is set on the connect tasks to
// indicate that doConnect handler should not set security backends up
// because this will be done later by the setup-profiles task.
func batchConnectTasks(st *state.State, snapsup *snapstate.SnapSetup, conns map[string]*interfaces.ConnRef, connOpts map[string]*connectOpts) (ts *state.TaskSet, hasInterfaceHooks bool, err error) {
	if len(conns) == 0 {
		return nil, false, nil
	}

	setupProfiles := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles for auto-connections"), snapsup.InstanceName(), snapsup.Revision()))
	setupProfiles.Set("snap-setup", snapsup)

	ts = state.NewTaskSet()
	for connID, conn := range conns {
		var opts connectOpts
		if providedOpts := connOpts[connID]; providedOpts != nil {
			opts = *providedOpts
		} else {
			// default
			opts.AutoConnect = true
		}
		opts.DelayedSetupProfiles = true
		connectTs, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, opts)
		if err != nil {
			return nil, false, fmt.Errorf("internal error: auto-connect of %q failed: %s", conn, err)
		}

		if len(connectTs.Tasks()) > 1 {
			hasInterfaceHooks = true
		}

		// setup-profiles needs to wait for the main "connect" task
		connectTask, _ := connectTs.Edge(ConnectTaskEdge)
		if connectTask == nil {
			return nil, false, fmt.Errorf("internal error: no 'connect' task found for %q", conn)
		}
		setupProfiles.WaitFor(connectTask)

		// setup-profiles must be run before the task that marks the end of connect-plug- and connect-slot- hooks
		afterConnectTask, _ := connectTs.Edge(AfterConnectHooksEdge)
		if afterConnectTask != nil {
			afterConnectTask.WaitFor(setupProfiles)
		}
		ts.AddAll(connectTs)
	}
	if len(ts.Tasks()) > 0 {
		ts.AddTask(setupProfiles)
	}
	return ts, hasInterfaceHooks, nil
}

// firstTaskAfterBootWhenPreseeding finds the first task to be run for thisSnap
// on first boot after mark-preseeded task, this is always the install hook.
// It is an internal error if install hook for thisSnap cannot be found.
func firstTaskAfterBootWhenPreseeding(thisSnap string, markPreseeded *state.Task) (*state.Task, error) {
	if markPreseeded.Change() == nil {
		return nil, fmt.Errorf("internal error: %s task not in change", markPreseeded.Kind())
	}
	for _, ht := range markPreseeded.HaltTasks() {
		if ht.Kind() == "run-hook" {
			var hs hookstate.HookSetup
			if err := ht.Get("hook-setup", &hs); err != nil {
				return nil, fmt.Errorf("internal error: cannot get hook setup: %v", err)
			}
			if hs.Hook == "install" && hs.Snap == thisSnap {
				return ht, nil
			}
		}
	}
	return nil, fmt.Errorf("internal error: cannot find install hook for snap %q", thisSnap)
}

func filterForSlot(slot *snap.SlotInfo) func(candSlots []*snap.SlotInfo) []*snap.SlotInfo {
	return func(candSlots []*snap.SlotInfo) []*snap.SlotInfo {
		for _, candSlot := range candSlots {
			if candSlot.String() == slot.String() {
				return []*snap.SlotInfo{slot}
			}
		}
		return nil
	}
}

// doAutoConnect creates task(s) to connect the given snap to viable candidates.
func (m *InterfaceManager) doAutoConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(task)
	if err != nil {
		return err
	}

	deviceCtx, err := snapstate.DeviceCtx(st, task, nil)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	// The previous task (link-snap) may have triggered a restart,
	// if this is the case we can only proceed once the restart
	// has happened or we may not have all the interfaces of the
	// new core/base snap.
	if err := snapstateFinishRestart(task, snapsup); err != nil {
		return err
	}

	snapName := snapsup.InstanceName()

	autochecker, err := newAutoConnectChecker(st, m.repo, deviceCtx)
	if err != nil {
		return err
	}

	gadgectConnect := newGadgetConnect(st, task, m.repo, snapName, deviceCtx)

	// wait for auto-install, started by prerequisites code, for
	// the default-providers of content ifaces so we can
	// auto-connect to them; snapstate prerequisites does a bit
	// more filtering than this so defaultProviders here can
	// contain some more snaps; should not be an issue in practice
	// given the check below checks for same chain and we don't
	// forcefully wait for defaultProviders; we just retry for
	// things in the intersection between defaultProviders here and
	// snaps with not ready link-snap|setup-profiles tasks
	defaultProviders := snap.DefaultContentProviders(m.repo.Plugs(snapName))
	for _, chg := range st.Changes() {
		if chg.IsReady() {
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
				// Only retry if the task that installs the
				// content provider is not waiting for us
				// (or this will just hang forever).
				_, ok := defaultProviders[snapsup.InstanceName()]
				if ok && !inSameChangeWaitChain(task, t) {
					return &state.Retry{After: contentLinkRetryTimeout}
				}
			}
		}
	}

	plugs := m.repo.Plugs(snapName)
	slots := m.repo.Slots(snapName)
	newconns := make(map[string]*interfaces.ConnRef, len(plugs)+len(slots))
	var connOpts map[string]*connectOpts

	conflictError := func(retry *state.Retry, err error) error {
		if retry != nil {
			task.Logf("Waiting for conflicting change in progress: %s", retry.Reason)
			return retry // will retry
		}
		return fmt.Errorf("auto-connect conflict check failed: %v", err)
	}

	// Consider gadget connections, we want to remember them in
	// any case with "by-gadget" set, so they should be processed
	// before the auto-connection ones.
	if err := gadgectConnect.addGadgetConnections(newconns, conns, conflictError); err != nil {
		return err
	}
	if len(newconns) > 0 {
		connOpts = make(map[string]*connectOpts, len(newconns))
		byGadgetOpts := &connectOpts{AutoConnect: true, ByGadget: true}
		for key := range newconns {
			connOpts[key] = byGadgetOpts
		}
	}

	// Auto-connect all the plugs
	cannotAutoConnectLog := func(plug *snap.PlugInfo, candRefs []string) string {
		return fmt.Sprintf("cannot auto-connect plug %s, candidates found: %s", plug, strings.Join(candRefs, ", "))
	}
	if err := autochecker.addAutoConnections(task, newconns, plugs, nil, conns, cannotAutoConnectLog, conflictError); err != nil {
		return err
	}
	// Auto-connect all the slots
	for _, slot := range slots {
		candidates := m.repo.AutoConnectCandidatePlugs(snapName, slot.Name, autochecker.check)
		if len(candidates) == 0 {
			continue
		}

		cannotAutoConnectLog := func(plug *snap.PlugInfo, candRefs []string) string {
			return fmt.Sprintf("cannot auto-connect slot %s to plug %s, candidates found: %s", slot, plug, strings.Join(candRefs, ", "))
		}
		if err := autochecker.addAutoConnections(task, newconns, candidates, filterForSlot(slot), conns, cannotAutoConnectLog, conflictError); err != nil {
			return err
		}
	}

	autots, hasInterfaceHooks, err := batchConnectTasks(st, snapsup, newconns, connOpts)
	if err != nil {
		return err
	}

	// If interface hooks are not present then connects can be executed during
	// preseeding.
	// Otherwise we will run all connects, their hooks and setup-profiles after
	// preseeding (on first boot). Note, we may be facing multiple connections
	// here where only some have hooks; however there is no point in running
	// those without hooks before mark-preseeded, because only setup-profiles is
	// performance-critical and it still needs to run after those with hooks.
	if m.preseed && hasInterfaceHooks {
		// note, hasInterfaceHooks implies autots != nil, so no extra check
		for _, t := range st.Tasks() {
			if t.Kind() == "mark-preseeded" {
				markPreseeded := t
				// consistency check
				if markPreseeded.Status() != state.DoStatus {
					return fmt.Errorf("internal error: unexpected state of mark-preseeded task: %s", markPreseeded.Status())
				}

				firstTaskAfterBoot, err := firstTaskAfterBootWhenPreseeding(snapsup.InstanceName(), markPreseeded)
				if err != nil {
					return err
				}
				// first task of the snap that normally runs on first boot
				// needs to wait on connects & interface hooks.
				firstTaskAfterBoot.WaitAll(autots)

				// connect tasks and interface hooks need to wait for end of preseeding
				// (they need to run on first boot, not during preseeding).
				autots.WaitFor(markPreseeded)
				t.Change().AddAll(autots)
				task.SetStatus(state.DoneStatus)
				st.EnsureBefore(0)
				return nil
			}
		}
		return fmt.Errorf("internal error: mark-preseeded task not found in preseeding mode")
	}

	if autots != nil && len(autots.Tasks()) > 0 {
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

	// After migrating connections in state, remove them from repo so they stay in sync and we don't
	// attempt to run disconnects on when the old core gets removed as part of the transition.
	if err := m.removeConnections(oldName); err != nil {
		return err
	}

	// The reloadConnections() just modifies the repository object, it
	// has no effect on the running system, i.e. no security profiles
	// on disk are rewritten. This is ok because core/ubuntu-core have
	// exactly the same profiles and nothing in the generated policies
	// has the core snap-name encoded.
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

// doHotplugConnect creates task(s) to (re)create old connections or auto-connect viable slots in response to hotplug "add" event.
func (m *InterfaceManager) doHotplugConnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	deviceCtx, err := snapstate.DeviceCtx(st, task, nil)
	if err != nil {
		return err
	}

	conns, err := getConns(st)
	if err != nil {
		return err
	}

	ifaceName, hotplugKey, err := getHotplugAttrs(task)
	if err != nil {
		return fmt.Errorf("internal error: cannot get hotplug task attributes: %s", err)
	}

	slot, err := m.repo.SlotForHotplugKey(ifaceName, hotplugKey)
	if err != nil {
		return err
	}
	if slot == nil {
		return fmt.Errorf("cannot find hotplug slot for interface %s and hotplug key %q", ifaceName, hotplugKey)
	}

	// find old connections for slots of this device - note we can't ask the repository since we need
	// to recreate old connections that are only remembered in the state.
	connsForDevice := findConnsForHotplugKey(conns, ifaceName, hotplugKey)

	conflictError := func(retry *state.Retry, err error) error {
		if retry != nil {
			task.Logf("hotplug connect will be retried: %s", retry.Reason)
			return retry // will retry
		}
		return fmt.Errorf("hotplug-connect conflict check failed: %v", err)
	}

	// find old connections to recreate
	var recreate []*interfaces.ConnRef
	for _, id := range connsForDevice {
		conn := conns[id]
		// device was not unplugged, this is the case if snapd is restarted and we enumerate devices.
		// note, the situation where device was not unplugged but has changed is handled
		// by hotplugDeviceAdded handler - updateDevice.
		if !conn.HotplugGone || conn.Undesired {
			continue
		}

		// the device was unplugged while connected, so it had disconnect hooks run; recreate the connection
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}

		if err := checkAutoconnectConflicts(st, task, connRef.PlugRef.Snap, connRef.SlotRef.Snap); err != nil {
			retry, _ := err.(*state.Retry)
			return conflictError(retry, err)
		}
		recreate = append(recreate, connRef)
	}

	// find new auto-connections
	autochecker, err := newAutoConnectChecker(st, m.repo, deviceCtx)
	if err != nil {
		return err
	}

	instanceName := slot.Snap.InstanceName()
	candidates := m.repo.AutoConnectCandidatePlugs(instanceName, slot.Name, autochecker.check)

	newconns := make(map[string]*interfaces.ConnRef, len(candidates))
	// Auto-connect the plugs
	cannotAutoConnectLog := func(plug *snap.PlugInfo, candRefs []string) string {
		return fmt.Sprintf("cannot auto-connect hotplug slot %s to plug %s, candidates found: %s", slot, plug, strings.Join(candRefs, ", "))
	}
	if err := autochecker.addAutoConnections(task, newconns, candidates, filterForSlot(slot), conns, cannotAutoConnectLog, conflictError); err != nil {
		return err
	}

	if len(recreate) == 0 && len(newconns) == 0 {
		return nil
	}

	// Create connect tasks and interface hooks for old connections
	connectTs := state.NewTaskSet()
	for _, conn := range recreate {
		wasAutoconnected := conns[conn.ID()].Auto
		ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: wasAutoconnected})
		if err != nil {
			return fmt.Errorf("internal error: connect of %q failed: %s", conn, err)
		}
		connectTs.AddAll(ts)
	}
	// Create connect tasks and interface hooks for new auto-connections
	for _, conn := range newconns {
		ts, err := connect(st, conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name, connectOpts{AutoConnect: true})
		if err != nil {
			return fmt.Errorf("internal error: auto-connect of %q failed: %s", conn, err)
		}
		connectTs.AddAll(ts)
	}

	if len(connectTs.Tasks()) > 0 {
		snapstate.InjectTasks(task, connectTs)
		st.EnsureBefore(0)
	}

	// make sure that we add tasks and mark this task done in the same atomic write, otherwise there is a risk of re-adding tasks again
	task.SetStatus(state.DoneStatus)

	return nil
}

// doHotplugUpdateSlot updates static attributes of a hotplug slot for given device.
func (m *InterfaceManager) doHotplugUpdateSlot(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	ifaceName, hotplugKey, err := getHotplugAttrs(task)
	if err != nil {
		return fmt.Errorf("internal error: cannot get hotplug task attributes: %s", err)
	}

	var attrs map[string]interface{}
	if err := task.Get("slot-attrs", &attrs); err != nil {
		return fmt.Errorf("internal error: cannot get slot-attrs attribute for device %s, interface %s: %s", hotplugKey, ifaceName, err)
	}

	stateSlots, err := getHotplugSlots(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain hotplug slots: %v", err)
	}

	slot, err := m.repo.UpdateHotplugSlotAttrs(ifaceName, hotplugKey, attrs)
	if err != nil {
		return err
	}

	if slotSpec, ok := stateSlots[slot.Name]; ok {
		slotSpec.StaticAttrs = attrs
		stateSlots[slot.Name] = slotSpec
		setHotplugSlots(st, stateSlots)
	} else {
		return fmt.Errorf("internal error: cannot find slot %s for device %q", slot.Name, hotplugKey)
	}

	return nil
}

// doHotplugRemoveSlot removes hotplug slot for given device from the repository in response to udev "remove" event.
// This task must necessarily be run after all affected slot gets disconnected in the repo.
func (m *InterfaceManager) doHotplugRemoveSlot(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	ifaceName, hotplugKey, err := getHotplugAttrs(task)
	if err != nil {
		return fmt.Errorf("internal error: cannot get hotplug task attributes: %s", err)
	}

	slot, err := m.repo.SlotForHotplugKey(ifaceName, hotplugKey)
	if err != nil {
		return fmt.Errorf("internal error: cannot determine slots: %v", err)
	}
	if slot != nil {
		if err := m.repo.RemoveSlot(slot.Snap.InstanceName(), slot.Name); err != nil {
			return fmt.Errorf("cannot remove hotplug slot: %v", err)
		}
	}

	stateSlots, err := getHotplugSlots(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain hotplug slots: %v", err)
	}

	// remove the slot from hotplug-slots in the state as long as there are no connections referencing it,
	// including connection with hotplug-gone=true.
	slotDef := findHotplugSlot(stateSlots, ifaceName, hotplugKey)
	if slotDef == nil {
		return fmt.Errorf("internal error: cannot find hotplug slot for interface %s, hotplug key %q", ifaceName, hotplugKey)
	}
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	for _, conn := range conns {
		if conn.Interface == slotDef.Interface && conn.HotplugKey == slotDef.HotplugKey {
			// there is a connection referencing this slot, do not remove it, only mark as "gone"
			slotDef.HotplugGone = true
			stateSlots[slotDef.Name] = slotDef
			setHotplugSlots(st, stateSlots)
			return nil
		}
	}
	delete(stateSlots, slotDef.Name)
	setHotplugSlots(st, stateSlots)

	return nil
}

// doHotplugDisconnect creates task(s) to disconnect connections and remove slots in response to hotplug "remove" event.
func (m *InterfaceManager) doHotplugDisconnect(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	ifaceName, hotplugKey, err := getHotplugAttrs(task)
	if err != nil {
		return fmt.Errorf("internal error: cannot get hotplug task attributes: %s", err)
	}

	connections, err := m.repo.ConnectionsForHotplugKey(ifaceName, hotplugKey)
	if err != nil {
		return err
	}
	if len(connections) == 0 {
		return nil
	}

	// check for conflicts on all connections first before creating disconnect hooks
	for _, connRef := range connections {
		if err := checkHotplugDisconnectConflicts(st, connRef.PlugRef.Snap, connRef.SlotRef.Snap); err != nil {
			if retry, ok := err.(*state.Retry); ok {
				task.Logf("Waiting for conflicting change in progress: %s", retry.Reason)
				return err // will retry
			}
			return fmt.Errorf("cannot check conflicts when disconnecting interfaces: %s", err)
		}
	}

	dts := state.NewTaskSet()
	for _, connRef := range connections {
		conn, err := m.repo.Connection(connRef)
		if err != nil {
			// this should never happen since we get all connections from the repo
			return fmt.Errorf("internal error: cannot get connection %q: %s", connRef, err)
		}
		// "by-hotplug" flag indicates it's a disconnect triggered as part of hotplug removal.
		ts, err := disconnectTasks(st, conn, disconnectOpts{ByHotplug: true})
		if err != nil {
			return fmt.Errorf("internal error: cannot create disconnect tasks: %s", err)
		}
		dts.AddAll(ts)
	}

	snapstate.InjectTasks(task, dts)
	st.EnsureBefore(0)

	// make sure that we add tasks and mark this task done in the same atomic write, otherwise there is a risk of re-adding tasks again
	task.SetStatus(state.DoneStatus)

	return nil
}

func (m *InterfaceManager) doHotplugAddSlot(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	systemSnap, err := systemSnapInfo(st)
	if err != nil {
		return fmt.Errorf("system snap not available")
	}

	ifaceName, hotplugKey, err := getHotplugAttrs(task)
	if err != nil {
		return fmt.Errorf("internal error: cannot get hotplug task attributes: %s", err)
	}

	var proposedSlot hotplug.ProposedSlot
	if err := task.Get("proposed-slot", &proposedSlot); err != nil {
		return fmt.Errorf("internal error: cannot get proposed hotplug slot from task attributes: %s", err)
	}
	var devinfo hotplug.HotplugDeviceInfo
	if err := task.Get("device-info", &devinfo); err != nil {
		return fmt.Errorf("internal error: cannot get hotplug device info from task attributes: %s", err)
	}

	stateSlots, err := getHotplugSlots(st)
	if err != nil {
		return fmt.Errorf("internal error obtaining hotplug slots: %v", err.Error())
	}

	iface := m.repo.Interface(ifaceName)
	if iface == nil {
		return fmt.Errorf("internal error: cannot find interface %s", ifaceName)
	}

	slot := findHotplugSlot(stateSlots, ifaceName, hotplugKey)

	// if we know this slot already, restore / update it.
	if slot != nil {
		if slot.HotplugGone {
			// hotplugGone means the device was unplugged, so its disconnect hooks were run and can now
			// simply recreate the slot with potentially new attributes, and old connections will be re-created
			newSlot := &snap.SlotInfo{
				Name:       slot.Name,
				Label:      proposedSlot.Label,
				Snap:       systemSnap,
				Interface:  ifaceName,
				Attrs:      proposedSlot.Attrs,
				HotplugKey: hotplugKey,
			}
			return addHotplugSlot(st, m.repo, stateSlots, iface, newSlot)
		}

		// else - not gone, restored already by reloadConnections, but may need updating.
		if !reflect.DeepEqual(proposedSlot.Attrs, slot.StaticAttrs) {
			ts := updateDevice(st, iface.Name(), hotplugKey, proposedSlot.Attrs)
			snapstate.InjectTasks(task, ts)
			st.EnsureBefore(0)
			task.SetStatus(state.DoneStatus)
		} // else - nothing to do
		return nil
	}

	// New slot.
	slotName := hotplugSlotName(hotplugKey, systemSnap.InstanceName(), proposedSlot.Name, iface.Name(), &devinfo, m.repo, stateSlots)
	newSlot := &snap.SlotInfo{
		Name:       slotName,
		Label:      proposedSlot.Label,
		Snap:       systemSnap,
		Interface:  iface.Name(),
		Attrs:      proposedSlot.Attrs,
		HotplugKey: hotplugKey,
	}
	return addHotplugSlot(st, m.repo, stateSlots, iface, newSlot)
}

// doHotplugSeqWait returns Retry error if there is another change for same hotplug key and a lower sequence number.
// Sequence numbers control the order of execution of hotplug-related changes, which would otherwise be executed in
// arbitrary order by task runner, leading to unexpected results if multiple events for same device are in flight
// (e.g. plugging, followed by immediate unplugging, or snapd restart with pending hotplug changes).
// The handler expects "hotplug-key" and "hotplug-seq" values set on own and other hotplug-related changes.
func (m *InterfaceManager) doHotplugSeqWait(task *state.Task, _ *tomb.Tomb) error {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	chg := task.Change()
	if chg == nil || !isHotplugChange(chg) {
		return fmt.Errorf("internal error: task %q not in a hotplug change", task.Kind())
	}

	seq, hotplugKey, err := getHotplugChangeAttrs(chg)
	if err != nil {
		return err
	}

	for _, otherChg := range st.Changes() {
		if otherChg.IsReady() || otherChg.ID() == chg.ID() {
			continue
		}

		// only inspect hotplug changes
		if !isHotplugChange(otherChg) {
			continue
		}

		otherSeq, otherKey, err := getHotplugChangeAttrs(otherChg)
		if err != nil {
			return err
		}

		// conflict with retry if there another change affecting same device and has lower sequence number
		if hotplugKey == otherKey && otherSeq < seq {
			task.Logf("Waiting processing of earlier hotplug event change %q affecting device with hotplug key %q", otherChg.Kind(), hotplugKey)
			// TODO: consider introducing a new task that runs last and does EnsureBefore(0) for hotplug changes
			return &state.Retry{After: hotplugRetryTimeout}
		}
	}

	// no conflicting change for same hotplug key found
	return nil
}
