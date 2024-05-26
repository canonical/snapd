// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/interfaces/utils"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/ifacestate/schema"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/timings"
)

func init() {
	snapstate.HasActiveConnection = hasActiveConnection
}

var (
	snapdAppArmorServiceIsDisabled = snapdAppArmorServiceIsDisabledImpl
	profilesNeedRegeneration       = profilesNeedRegenerationImpl

	writeSystemKey = interfaces.WriteSystemKey
)

func (m *InterfaceManager) selectInterfaceMapper(snaps []*snap.Info) {
	for _, snapInfo := range snaps {
		if snapInfo.Type() == snap.TypeSnapd {
			mapper = &CoreSnapdSystemMapper{}
			break
		}
	}
}

func (m *InterfaceManager) addInterfaces(extra []interfaces.Interface) error {
	for _, iface := range builtin.Interfaces() {
		mylog.Check(m.repo.AddInterface(iface))
	}
	for _, iface := range extra {
		mylog.Check(m.repo.AddInterface(iface))
	}
	return nil
}

func (m *InterfaceManager) addBackends(extra []interfaces.SecurityBackend) error {
	// get the snapd snap info if it is installed
	var snapdSnap snapstate.SnapState
	var snapdSnapInfo *snap.Info
	mylog.Check(snapstate.Get(m.state, "snapd", &snapdSnap))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("cannot access snapd snap state: %v", err)
	}
	if err == nil {
		snapdSnapInfo = mylog.Check2(snapdSnap.CurrentInfo())
		if err != nil && err != snapstate.ErrNoCurrent {
			return fmt.Errorf("cannot access snapd snap info: %v", err)
		}
	}

	// get the core snap info if it is installed
	var coreSnap snapstate.SnapState
	var coreSnapInfo *snap.Info
	mylog.Check(snapstate.Get(m.state, "core", &coreSnap))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("cannot access core snap state: %v", err)
	}
	if err == nil {
		coreSnapInfo = mylog.Check2(coreSnap.CurrentInfo())
		if err != nil && err != snapstate.ErrNoCurrent {
			return fmt.Errorf("cannot access core snap info: %v", err)
		}
	}

	opts := interfaces.SecurityBackendOptions{
		Preseed:       m.preseed,
		CoreSnapInfo:  coreSnapInfo,
		SnapdSnapInfo: snapdSnapInfo,
	}
	for _, backend := range allSecurityBackends() {
		mylog.Check(backend.Initialize(&opts))
		mylog.Check(m.repo.AddBackend(backend))

	}
	for _, backend := range extra {
		mylog.Check(backend.Initialize(&opts))
		mylog.Check(m.repo.AddBackend(backend))

	}
	return nil
}

func (m *InterfaceManager) addSnaps(snaps []*snap.Info) error {
	for _, snapInfo := range snaps {
		mylog.Check(addImplicitSlots(m.state, snapInfo))
		mylog.Check(m.repo.AddSnap(snapInfo))

	}
	return nil
}

func profilesNeedRegenerationImpl() bool {
	mismatch := mylog.Check2(interfaces.SystemKeyMismatch())

	return mismatch
}

// snapdAppArmorServiceIsDisabledImpl returns true if the snapd.apparmor
// service unit exists but is disabled
func snapdAppArmorServiceIsDisabledImpl() bool {
	sysd := systemd.New(systemd.SystemMode, nil)
	isEnabled := mylog.Check2(sysd.IsEnabled("snapd.apparmor"))
	return err == nil && !isEnabled
}

// regenerateAllSecurityProfiles will regenerate all security profiles.
func (m *InterfaceManager) regenerateAllSecurityProfiles(tm timings.Measurer) error {
	// Get all the security backends
	securityBackends := m.repo.Backends()

	// Get all the snap infos
	snaps := mylog.Check2(snapsWithSecurityProfiles(m.state))

	// TODO: should snapsWithSecurityProfiles return app sets instead of snap infos?
	appSets := make([]*interfaces.SnapAppSet, 0, len(snaps))
	for _, sn := range snaps {
		set := mylog.Check2(appSetForSnapRevision(m.state, sn))

		appSets = append(appSets, set)
	}

	// Add implicit slots to all snaps
	for _, snapInfo := range snaps {
		mylog.Check(addImplicitSlots(m.state, snapInfo))
	}

	// The reason the system key is unlinked is to prevent snapd from believing
	// that an old system key is valid and represents security setup
	// established in the system. If snapd is reverted following a failed
	// startup then system key may match the system key that used to be on disk
	// but some of the system security may have been changed by the new snapd,
	// the one that was reverted. Unlinking avoids such possibility, forcing
	// old snapd to re-establish proper security view.
	shouldWriteSystemKey := true
	os.Remove(dirs.SnapSystemKeyFile)

	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		var snapst snapstate.SnapState
		mylog.Check(snapstate.Get(m.state, snapName, &snapst))

		snapInfo := mylog.Check2(snapst.CurrentInfo())

		opts := mylog.Check2(buildConfinementOptions(m.state, snapInfo, snapst.Flags))

		return opts
	}

	// For each backend:
	for _, backend := range securityBackends {
		if backend.Name() == "" {
			continue // Test backends have no name, skip them to simplify testing.
		}
		if errors := interfaces.SetupMany(m.repo, backend, appSets, confinementOpts, tm); len(errors) > 0 {
			logger.Noticef("cannot regenerate %s profiles", backend.Name())
			for _, err := range errors {
				logger.Noticef(err.Error())
			}
			shouldWriteSystemKey = false
		}
	}

	if shouldWriteSystemKey {
		mylog.Check(writeSystemKey())
	}
	return nil
}

// renameCorePlugConnection renames one connection from "core-support" plug to
// slot so that the plug name is "core-support-plug" while the slot is
// unchanged. This matches a change introduced in 2.24, where the core snap no
// longer has the "core-support" plug as that was clashing with the slot with
// the same name.
func (m *InterfaceManager) renameCorePlugConnection() error {
	conns := mylog.Check2(getConns(m.state))

	const oldPlugName = "core-support"
	const newPlugName = "core-support-plug"
	// old connection, note that slotRef is the same in both
	slotRef := interfaces.SlotRef{Snap: "core", Name: oldPlugName}
	oldPlugRef := interfaces.PlugRef{Snap: "core", Name: oldPlugName}
	oldConnRef := interfaces.ConnRef{PlugRef: oldPlugRef, SlotRef: slotRef}
	oldID := oldConnRef.ID()
	// if the old connection is saved, replace it with the new connection
	if cState, ok := conns[oldID]; ok {
		newPlugRef := interfaces.PlugRef{Snap: "core", Name: newPlugName}
		newConnRef := interfaces.ConnRef{PlugRef: newPlugRef, SlotRef: slotRef}
		newID := newConnRef.ID()
		delete(conns, oldID)
		conns[newID] = cState
		setConns(m.state, conns)
	}
	return nil
}

// removeStaleConnections removes stale connections left by some older versions of snapd.
// Connection is considered stale if the snap on either end of the connection doesn't exist anymore.
// XXX: this code should eventually go away.
var removeStaleConnections = func(st *state.State) error {
	conns := mylog.Check2(getConns(st))

	var staleConns []string
	for id := range conns {
		connRef := mylog.Check2(interfaces.ParseConnRef(id))

		var snapst snapstate.SnapState
		mylog.Check(snapstate.Get(st, connRef.PlugRef.Snap, &snapst))
		mylog.Check(snapstate.Get(st, connRef.SlotRef.Snap, &snapst))

	}
	if len(staleConns) > 0 {
		for _, id := range staleConns {
			delete(conns, id)
		}
		setConns(st, conns)
		logger.Noticef("removed stale connections: %s", strings.Join(staleConns, ", "))
	}
	return nil
}

func isBroken(st *state.State, snapName string) (bool, error) {
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, snapName, &snapst))
	if errors.Is(err, state.ErrNoState) {
		return false, nil
	}

	snapInfo, _ := snapst.CurrentInfo()
	if snapInfo != nil && snapInfo.Broken != "" {
		return true, nil
	}
	return false, nil
}

// reloadConnections reloads connections stored in the state in the repository.
// Using non-empty snapName the operation can be scoped to connections
// affecting a given snap.
//
// The return value is the list of affected snap names.
func (m *InterfaceManager) reloadConnections(snapName string) ([]string, error) {
	conns := mylog.Check2(getConns(m.state))

	connStateChanged := false
	affected := make(map[string]bool)
ConnsLoop:
	for connId, connState := range conns {
		// Skip entries that just mark a connection as undesired. Those don't
		// carry attributes that can go stale. In the same spirit, skip
		// information about hotplug connections that don't have the associated
		// hotplug hardware.
		if connState.Undesired || connState.HotplugGone {
			continue
		}
		connRef := mylog.Check2(interfaces.ParseConnRef(connId))

		// Apply filtering, this allows us to reload only a subset of
		// connections (and similarly, refresh the static attributes of only a
		// subset of connections).
		if snapName != "" && connRef.PlugRef.Snap != snapName && connRef.SlotRef.Snap != snapName {
			continue
		}

		plugInfo := m.repo.Plug(connRef.PlugRef.Snap, connRef.PlugRef.Name)
		slotInfo := m.repo.Slot(connRef.SlotRef.Snap, connRef.SlotRef.Name)

		// The connection refers to a plug or slot that doesn't exist anymore, e.g. because of a refresh
		// to a new snap revision that doesn't have the given plug/slot.
		if plugInfo == nil || slotInfo == nil {
			// automatic connection can simply be removed (it will be re-created automatically if needed)
			// as long as it wasn't disconnected manually; note that undesired flag is taken care of at
			// the beginning of the loop.
			if connState.Auto && !connState.ByGadget && connState.Interface != "core-support" {
				// only do anything about this connection if snap isn't in a broken state, otherwise
				// leave the connection untouched.
				for _, snapName := range []string{connRef.PlugRef.Snap, connRef.SlotRef.Snap} {
					broken := mylog.Check2(isBroken(m.state, snapName))

					if broken {
						logger.Noticef("Snap %q is broken, ignored by reloadConnections", snapName)
						continue ConnsLoop
					}
				}
				delete(conns, connId)
				connStateChanged = true
			}
			// otherwise keep it and silently ignore, e.g. in case of a revert.
			continue
		}

		var updateStaticAttrs bool
		staticPlugAttrs := connState.StaticPlugAttrs
		staticSlotAttrs := connState.StaticSlotAttrs

		// XXX: Refresh the copy of the static connection attributes for "content"
		// and "system-files" interfaces.
		// This is a partial and temporary solution to https://bugs.launchpad.net/snapd/+bug/1825883
		// and https://bugs.launchpad.net/snapd/+bug/1942266.
		switch plugInfo.Interface {
		case "content":
			var plugContent, slotContent string
			plugInfo.Attr("content", &plugContent)
			slotInfo.Attr("content", &slotContent)

			if plugContent != "" && plugContent == slotContent {
				staticPlugAttrs = utils.NormalizeInterfaceAttributes(plugInfo.Attrs).(map[string]interface{})
				staticSlotAttrs = utils.NormalizeInterfaceAttributes(slotInfo.Attrs).(map[string]interface{})
				updateStaticAttrs = true
			} else {
				logger.Noticef("cannot refresh static attributes of the connection %q", connId)
			}
		case "system-files":
			staticPlugAttrs = utils.NormalizeInterfaceAttributes(plugInfo.Attrs).(map[string]interface{})
			staticSlotAttrs = utils.NormalizeInterfaceAttributes(slotInfo.Attrs).(map[string]interface{})
			updateStaticAttrs = true
		}
		mylog.Check2(

			// Note: reloaded connections are not checked against policy again, and also we don't call BeforeConnect* methods on them.
			m.repo.Connect(connRef, staticPlugAttrs, connState.DynamicPlugAttrs, staticSlotAttrs, connState.DynamicSlotAttrs, nil))

		// If the connection succeeded update the connection state and keep
		// track of the snaps that were affected.

	}
	if connStateChanged {
		setConns(m.state, conns)
	}

	result := make([]string, 0, len(affected))
	for name := range affected {
		result = append(result, name)
	}
	return result, nil
}

// removeConnections disconnects all connections of the snap in the repo. It should only be used if the snap
// has no connections in the state. State must be locked by the caller.
func (m *InterfaceManager) removeConnections(snapName string) error {
	conns := mylog.Check2(getConns(m.state))

	for id := range conns {
		connRef := mylog.Check2(interfaces.ParseConnRef(id))

		if connRef.PlugRef.Snap == snapName || connRef.SlotRef.Snap == snapName {
			return fmt.Errorf("internal error: cannot remove connections of snap %s from the repository while its connections are present in the state", snapName)
		}
	}

	repoConns := mylog.Check2(m.repo.Connections(snapName))

	for _, conn := range repoConns {
		mylog.Check(m.repo.Disconnect(conn.PlugRef.Snap, conn.PlugRef.Name, conn.SlotRef.Snap, conn.SlotRef.Name))
	}
	return nil
}

func (m *InterfaceManager) setupSecurityByBackend(task *state.Task, appSets []*interfaces.SnapAppSet, opts []interfaces.ConfinementOptions, tm timings.Measurer) error {
	if len(appSets) != len(opts) {
		return fmt.Errorf("internal error: setupSecurityByBackend received an unexpected number of snaps (expected: %d, got %d)", len(opts), len(appSets))
	}
	confOpts := make(map[string]interfaces.ConfinementOptions, len(appSets))
	for i, set := range appSets {
		confOpts[set.InstanceName()] = opts[i]
	}

	st := task.State()
	st.Unlock()
	defer st.Lock()

	// Setup all affected snaps, start with the most important security
	// backend and run it for all snaps. See LP: 1802581
	for _, backend := range m.repo.Backends() {
		errs := interfaces.SetupMany(m.repo, backend, appSets, func(snapName string) interfaces.ConfinementOptions {
			return confOpts[snapName]
		}, tm)
		if len(errs) > 0 {
			// SetupMany processes all profiles and returns all encountered errors; report just the first one
			return errs[0]
		}
	}

	return nil
}

func (m *InterfaceManager) setupSnapSecurity(task *state.Task, appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, tm timings.Measurer) error {
	return m.setupSecurityByBackend(task, []*interfaces.SnapAppSet{appSet}, []interfaces.ConfinementOptions{opts}, tm)
}

func (m *InterfaceManager) removeSnapSecurity(task *state.Task, instanceName string) error {
	st := task.State()
	for _, backend := range m.repo.Backends() {
		st.Unlock()
		mylog.Check(backend.Remove(instanceName))
		st.Lock()

	}
	return nil
}

func addHotplugSlot(st *state.State, repo *interfaces.Repository, stateSlots map[string]*HotplugSlotInfo, iface interfaces.Interface, slot *snap.SlotInfo) error {
	if slot.HotplugKey == "" {
		return fmt.Errorf("internal error: cannot store slot %q, not a hotplug slot", slot.Name)
	}
	if iface, ok := iface.(interfaces.SlotSanitizer); ok {
		mylog.Check(iface.BeforePrepareSlot(slot))
	}
	mylog.Check(repo.AddSlot(slot))

	stateSlots[slot.Name] = &HotplugSlotInfo{
		Name:        slot.Name,
		Interface:   slot.Interface,
		StaticAttrs: slot.Attrs,
		HotplugKey:  slot.HotplugKey,
		HotplugGone: false,
	}
	setHotplugSlots(st, stateSlots)
	logger.Debugf("added hotplug slot %s:%s of interface %s, hotplug key %q", slot.Snap.InstanceName(), slot.Name, slot.Interface, slot.HotplugKey)
	return nil
}

type gadgetConnect struct {
	st   *state.State
	task *state.Task
	repo *interfaces.Repository

	instanceName string

	deviceCtx snapstate.DeviceContext
}

func newGadgetConnect(s *state.State, task *state.Task, repo *interfaces.Repository, instanceName string, deviceCtx snapstate.DeviceContext) *gadgetConnect {
	return &gadgetConnect{
		st:           s,
		task:         task,
		repo:         repo,
		instanceName: instanceName,
		deviceCtx:    deviceCtx,
	}
}

// addGadgetConnections adds to newconns any applicable connections
// from the gadget connections stanza.
// conflictError is called to handle checkAutoconnectConflicts errors.
func (gc *gadgetConnect) addGadgetConnections(newconns map[string]*interfaces.ConnRef, conns map[string]*schema.ConnState, conflictError func(*state.Retry, error) error) error {
	var seeded bool
	mylog.Check(gc.st.Get("seeded", &seeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	// we apply gadget connections only during seeding or a remodeling
	if seeded && !gc.deviceCtx.ForRemodeling() {
		return nil
	}

	task := gc.task
	snapName := gc.instanceName

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(gc.st, snapName, &snapst))

	snapInfo := mylog.Check2(snapst.CurrentInfo())

	snapID := snapInfo.SnapID
	if snapID == "" {
		// not a snap-id identifiable snap, skip
		return nil
	}

	gconns := mylog.Check2(snapstate.GadgetConnections(gc.st, gc.deviceCtx))

	// no gadget yet, nothing to do

	// consider the gadget connect instructions
	for _, gconn := range gconns {
		var plugSnapName, slotSnapName string
		if gconn.Plug.SnapID == snapID {
			plugSnapName = snapName
		}
		if gconn.Slot.SnapID == snapID {
			slotSnapName = snapName
		}

		if plugSnapName == "" && slotSnapName == "" {
			// no match, nothing to do
			continue
		}

		if plugSnapName == "" {
			plugSnapName = mylog.Check2(resolveSnapIDToName(gc.st, gconn.Plug.SnapID))
		}
		plug := gc.repo.Plug(plugSnapName, gconn.Plug.Plug)
		if plug == nil {
			task.Logf("gadget connections: ignoring missing plug %s:%s", gconn.Plug.SnapID, gconn.Plug.Plug)
			continue
		}

		if slotSnapName == "" {
			slotSnapName = mylog.Check2(resolveSnapIDToName(gc.st, gconn.Slot.SnapID))
		}
		slot := gc.repo.Slot(slotSnapName, gconn.Slot.Slot)
		if slot == nil {
			task.Logf("gadget connections: ignoring missing slot %s:%s", gconn.Slot.SnapID, gconn.Slot.Slot)
			continue
		}
		mylog.Check(addNewConnection(gc.st, task, newconns, conns, plug, slot, conflictError))

	}

	return nil
}

func addNewConnection(st *state.State, task *state.Task, newconns map[string]*interfaces.ConnRef, conns map[string]*schema.ConnState, plug *snap.PlugInfo, slot *snap.SlotInfo, conflictError func(*state.Retry, error) error) error {
	connRef := interfaces.NewConnRef(plug, slot)
	key := connRef.ID()
	if _, ok := conns[key]; ok {
		// Suggested connection already exist (or has
		// Undesired flag set) so don't clobber it.
		// NOTE: we don't log anything here as this is
		// a normal and common condition.
		return nil
	}
	if _, ok := newconns[key]; ok {
		return nil
	}

	if task.Kind() == "auto-connect" {
		ignore := mylog.Check2(findSymmetricAutoconnectTask(st, plug.Snap.InstanceName(), slot.Snap.InstanceName(), task))

		if ignore {
			return nil
		}
	}
	mylog.Check(checkAutoconnectConflicts(st, task, plug.Snap.InstanceName(), slot.Snap.InstanceName()))

	newconns[key] = connRef
	return nil
}

// DebugAutoConnectCheck is a hook that can be set to debug auto-connection
// candidates as they are checked.
var DebugAutoConnectCheck func(*policy.ConnectCandidate, interfaces.SideArity, error)

type autoConnectChecker struct {
	st   *state.State
	task *state.Task
	repo *interfaces.Repository

	deviceCtx snapstate.DeviceContext
	cache     map[string]*asserts.SnapDeclaration
	baseDecl  *asserts.BaseDeclaration
}

func newAutoConnectChecker(s *state.State, task *state.Task, repo *interfaces.Repository, deviceCtx snapstate.DeviceContext) (*autoConnectChecker, error) {
	baseDecl := mylog.Check2(assertstate.BaseDeclaration(s))

	return &autoConnectChecker{
		st:        s,
		task:      task,
		repo:      repo,
		deviceCtx: deviceCtx,
		cache:     make(map[string]*asserts.SnapDeclaration),
		baseDecl:  baseDecl,
	}, nil
}

func (c *autoConnectChecker) snapDeclaration(snapID string) (*asserts.SnapDeclaration, error) {
	snapDecl := c.cache[snapID]
	if snapDecl != nil {
		return snapDecl, nil
	}
	snapDecl := mylog.Check2(assertstate.SnapDeclaration(c.st, snapID))

	c.cache[snapID] = snapDecl
	return snapDecl, nil
}

func (c *autoConnectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, interfaces.SideArity, error) {
	modelAs := c.deviceCtx.Model()

	var storeAs *asserts.Store
	if modelAs.Store() != "" {

		storeAs = mylog.Check2(assertstate.Store(c.st, modelAs.Store()))
		if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
			return false, nil, err
		}
	}

	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		plugDecl = mylog.Check2(c.snapDeclaration(plug.Snap().SnapID))
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		slotDecl = mylog.Check2(c.snapDeclaration(slot.Snap().SnapID))
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     c.baseDecl,
		Model:               modelAs,
		Store:               storeAs,
	}

	arity := mylog.Check2(ic.CheckAutoConnect())
	if DebugAutoConnectCheck != nil {
		DebugAutoConnectCheck(&ic, arity, err)
	}
	if err == nil {
		return true, arity, nil
	}

	return false, nil, nil
}

// filterUbuntuCoreSlots filters out any ubuntu-core slots,
// if there are both ubuntu-core and core slots. This would occur
// during a ubuntu-core -> core transition.
func filterUbuntuCoreSlots(candidates []*snap.SlotInfo, arities []interfaces.SideArity) ([]*snap.SlotInfo, []interfaces.SideArity) {
	hasCore := false
	hasUbuntuCore := false
	var withoutUbuntuCore []*snap.SlotInfo
	var withoutUbuntuCoreArities []interfaces.SideArity
	for i, candSlot := range candidates {
		switch candSlot.Snap.InstanceName() {
		case "ubuntu-core":
			if !hasUbuntuCore {
				hasUbuntuCore = true
				withoutUbuntuCore = append(withoutUbuntuCore, candidates[:i]...)
				withoutUbuntuCoreArities = append(withoutUbuntuCoreArities, arities[:i]...)
			}
		case "core":
			hasCore = true
			fallthrough
		default:
			if hasUbuntuCore {
				withoutUbuntuCore = append(withoutUbuntuCore, candSlot)
				withoutUbuntuCoreArities = append(withoutUbuntuCoreArities, arities[i])
			}
		}
	}
	if hasCore && hasUbuntuCore {
		candidates = withoutUbuntuCore
		arities = withoutUbuntuCoreArities
	}
	return candidates, arities
}

// addAutoConnections adds to newconns any applicable auto-connections
// from the given plugs to corresponding candidates slots after
// filtering them with optional filter and against preexisting
// conns. cannotAutoConnectLog is called to build a log message in
// case no applicable pair was found. conflictError is called
// to handle checkAutoconnectConflicts errors.
func (c *autoConnectChecker) addAutoConnections(newconns map[string]*interfaces.ConnRef, plugs []*snap.PlugInfo, filter func([]*snap.SlotInfo) []*snap.SlotInfo, conns map[string]*schema.ConnState, cannotAutoConnectLog func(plug *snap.PlugInfo, candRefs []string) string, conflictError func(*state.Retry, error) error) error {
	for _, plug := range plugs {
		candSlots, arities := c.repo.AutoConnectCandidateSlots(plug.Snap.InstanceName(), plug.Name, c.check)

		if len(candSlots) == 0 {
			continue
		}

		// If we are in a core transition we may have both the
		// old ubuntu-core snap and the new core snap
		// providing the same interface. In that situation we
		// want to ignore any candidates in ubuntu-core and
		// simply go with those from the new core snap.
		candSlots, arities = filterUbuntuCoreSlots(candSlots, arities)

		applicable := candSlots
		// candidate arity check
		for _, arity := range arities {
			if !arity.SlotsPerPlugAny() {
				// ATM not any (*) => none or exactly one
				if len(candSlots) != 1 {
					applicable = nil
				}
				break
			}
		}

		if filter != nil {
			applicable = filter(applicable)
		}

		if len(applicable) == 0 {
			crefs := make([]string, len(candSlots))
			for i, candidate := range candSlots {
				crefs[i] = candidate.String()
			}
			c.task.Logf(cannotAutoConnectLog(plug, crefs))
			continue
		}

		for _, slot := range applicable {
			mylog.Check(addNewConnection(c.st, c.task, newconns, conns, plug, slot, conflictError))
		}
	}

	return nil
}

type connectChecker struct {
	st        *state.State
	deviceCtx snapstate.DeviceContext
	baseDecl  *asserts.BaseDeclaration
}

func newConnectChecker(s *state.State, deviceCtx snapstate.DeviceContext) (*connectChecker, error) {
	baseDecl := mylog.Check2(assertstate.BaseDeclaration(s))

	return &connectChecker{
		st:        s,
		deviceCtx: deviceCtx,
		baseDecl:  baseDecl,
	}, nil
}

func (c *connectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, error) {
	modelAs := c.deviceCtx.Model()

	var storeAs *asserts.Store
	if modelAs.Store() != "" {

		storeAs = mylog.Check2(assertstate.Store(c.st, modelAs.Store()))
		if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
			return false, err
		}
	}

	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		plugDecl = mylog.Check2(assertstate.SnapDeclaration(c.st, plug.Snap().SnapID))
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		slotDecl = mylog.Check2(assertstate.SnapDeclaration(c.st, slot.Snap().SnapID))
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     c.baseDecl,
		Model:               modelAs,
		Store:               storeAs,
	}

	// if either of plug or slot snaps don't have a declaration it
	// means they were installed with "dangerous", so the security
	// check should be skipped at this point.
	if plugDecl != nil && slotDecl != nil {
		mylog.Check(ic.Check())
	}
	return true, nil
}

func getPlugAndSlotRefs(task *state.Task) (interfaces.PlugRef, interfaces.SlotRef, error) {
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	mylog.Check(task.Get("plug", &plugRef))
	mylog.Check(task.Get("slot", &slotRef))

	return plugRef, slotRef, nil
}

// getConns returns information about connections from the state.
//
// Connections are transparently re-mapped according to remapIncomingConnRef
func getConns(st *state.State) (conns map[string]*schema.ConnState, err error) {
	var raw *json.RawMessage
	mylog.Check(st.Get("conns", &raw))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("cannot obtain raw data about existing connections: %s", err)
	}
	if raw != nil {
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &conns))
	}
	if conns == nil {
		conns = make(map[string]*schema.ConnState)
	}
	remapped := make(map[string]*schema.ConnState, len(conns))
	for id, cstate := range conns {
		cref := mylog.Check2(interfaces.ParseConnRef(id))

		cref.PlugRef.Snap = RemapSnapFromState(cref.PlugRef.Snap)
		cref.SlotRef.Snap = RemapSnapFromState(cref.SlotRef.Snap)
		cstate.StaticSlotAttrs = utils.NormalizeInterfaceAttributes(cstate.StaticSlotAttrs).(map[string]interface{})
		cstate.DynamicSlotAttrs = utils.NormalizeInterfaceAttributes(cstate.DynamicSlotAttrs).(map[string]interface{})
		cstate.StaticPlugAttrs = utils.NormalizeInterfaceAttributes(cstate.StaticPlugAttrs).(map[string]interface{})
		cstate.DynamicPlugAttrs = utils.NormalizeInterfaceAttributes(cstate.DynamicPlugAttrs).(map[string]interface{})
		remapped[cref.ID()] = cstate
	}
	return remapped, nil
}

// setConns sets information about connections in the state.
//
// Connections are transparently re-mapped according to remapOutgoingConnRef
func setConns(st *state.State, conns map[string]*schema.ConnState) {
	remapped := make(map[string]*schema.ConnState, len(conns))
	for id, cstate := range conns {
		cref := mylog.Check2(interfaces.ParseConnRef(id))

		// We cannot fail here

		cref.PlugRef.Snap = RemapSnapToState(cref.PlugRef.Snap)
		cref.SlotRef.Snap = RemapSnapToState(cref.SlotRef.Snap)
		remapped[cref.ID()] = cstate
	}
	st.Set("conns", remapped)
}

// snapsWithSecurityProfiles returns all snaps that have active
// security profiles: these are either snaps that are active,
// inactive snaps that are being operated on, whose profile state
// is tracked with SnapState.PendingSecurity,
// or snap about to be active (pending link-snap) with a done
// setup-profiles
func snapsWithSecurityProfiles(st *state.State) ([]*snap.Info, error) {
	all := mylog.Check2(snapstate.All(st))

	infos := make([]*snap.Info, 0, len(all))
	seen := make(map[string]bool, len(all))
	for instanceName, snapst := range all {
		if snapst.Active {
			snapInfo := mylog.Check2(snapst.CurrentInfo())

			infos = append(infos, snapInfo)
			seen[instanceName] = true
		} else if snapst.PendingSecurity != nil {
			// we tracked any pending security profiles for the snap
			seen[instanceName] = true
			si := snapst.PendingSecurity.SideInfo
			if si == nil {
				// profiles removed (already)
				continue
			}
			snapInfo := mylog.Check2(snap.ReadInfo(instanceName, si))

			infos = append(infos, snapInfo)
		}
	}
	// look at the changes for old snapds and also
	// the situation that are being installed, so they do not
	// have SnapState yet
	for _, t := range st.Tasks() {
		if t.Kind() != "link-snap" || t.Status().Ready() {
			continue
		}
		snapsup := mylog.Check2(snapstate.TaskSnapSetup(t))

		instanceName := snapsup.InstanceName()
		if seen[instanceName] {
			continue
		}

		doneProfiles := false
		for _, t1 := range t.WaitTasks() {
			if t1.Kind() == "setup-profiles" && t1.Status() == state.DoneStatus {
				snapsup1 := mylog.Check2(snapstate.TaskSnapSetup(t1))

				if snapsup1.InstanceName() == instanceName {
					doneProfiles = true
					break
				}
			}
		}
		if !doneProfiles {
			continue
		}

		seen[instanceName] = true
		snapInfo := mylog.Check2(snap.ReadInfo(instanceName, snapsup.SideInfo))

		infos = append(infos, snapInfo)
	}

	return infos, nil
}

func resolveSnapIDToName(st *state.State, snapID string) (name string, err error) {
	if snapID == "system" {
		// Resolve the system nickname to a concrete snap.
		return mapper.RemapSnapFromRequest(snapID), nil
	}
	decl := mylog.Check2(assertstate.SnapDeclaration(st, snapID))
	if errors.Is(err, &asserts.NotFoundError{}) {
		return "", nil
	}

	return decl.SnapName(), nil
}

// SnapMapper offers APIs for re-mapping snap names in interfaces and the
// configuration system. The mapper is designed to apply transformations around
// the edges of snapd (state interactions and API interactions) to offer one
// view on the inside of snapd and another view on the outside.
type SnapMapper interface {
	// re-map functions for loading and saving objects in the state.
	RemapSnapFromState(snapName string) string
	RemapSnapToState(snapName string) string
	// RamapSnapFromRequest can replace snap names in API requests.
	// There is no corresponding mapping function for API responses anymore.
	// The API responses always reflect the real system state.
	RemapSnapFromRequest(snapName string) string
	// Returns actual name of the system snap.
	SystemSnapName() string
}

// IdentityMapper implements SnapMapper and performs no transformations at all.
type IdentityMapper struct{}

// RemapSnapFromState doesn't change the snap name in any way.
func (m *IdentityMapper) RemapSnapFromState(snapName string) string {
	return snapName
}

// RemapSnapToState doesn't change the snap name in any way.
func (m *IdentityMapper) RemapSnapToState(snapName string) string {
	return snapName
}

// RemapSnapFromRequest  doesn't change the snap name in any way.
func (m *IdentityMapper) RemapSnapFromRequest(snapName string) string {
	return snapName
}

// CoreCoreSystemMapper implements SnapMapper and makes implicit slots
// appear to be on "core" in the state and in memory but as "system" in the API.
//
// NOTE: This mapper can be used to prepare, as an intermediate step, for the
// transition to "snapd" mapper. Using it the state and API layer will look
// exactly the same as with the "snapd" mapper. This can be used to make any
// necessary adjustments the test suite.
type CoreCoreSystemMapper struct {
	IdentityMapper // Embedding the identity mapper allows us to cut on boilerplate.
}

// RemapSnapFromRequest renames the "system" snap to the "core" snap.
//
// This allows us to accept connection and disconnection requests that
// explicitly refer to "core" or using the "system" nickname.
func (m *CoreCoreSystemMapper) RemapSnapFromRequest(snapName string) string {
	if snapName == "system" {
		return m.SystemSnapName()
	}
	return snapName
}

// SystemSnapName returns actual name of the system snap.
func (m *CoreCoreSystemMapper) SystemSnapName() string {
	return "core"
}

// CoreSnapdSystemMapper implements SnapMapper and makes implicit slots
// appear to be on "core" in the state and on "system" in the API while they
// are on "snapd" in memory.
type CoreSnapdSystemMapper struct {
	IdentityMapper // Embedding the identity mapper allows us to cut on boilerplate.
}

// RemapSnapFromState renames the "core" snap to the "snapd" snap.
//
// This allows modern snapd to load an old state that remembers connections
// between slots on the "core" snap and other snaps. In memory we are actually
// using "snapd" snap for hosting those slots and this lets us stay compatible.
func (m *CoreSnapdSystemMapper) RemapSnapFromState(snapName string) string {
	if snapName == "core" {
		return m.SystemSnapName()
	}
	return snapName
}

// RemapSnapToState renames the "snapd" snap to the "core" snap.
//
// This allows the state to stay backwards compatible as all the connections
// seem to refer to the "core" snap, as in pre core{16,18} days where there was
// only one core snap.
func (m *CoreSnapdSystemMapper) RemapSnapToState(snapName string) string {
	if snapName == m.SystemSnapName() {
		return "core"
	}
	return snapName
}

// RemapSnapFromRequest renames the "core" or "system" snaps to the "snapd" snap.
//
// This allows us to accept connection and disconnection requests that
// explicitly refer to "core" or "system" even though we really want them to
// refer to "snapd". Note that this is not fully symmetric with
// RemapSnapToResponse as we explicitly always talk about "system" snap,
// even if the request used "core".
func (m *CoreSnapdSystemMapper) RemapSnapFromRequest(snapName string) string {
	if snapName == "system" || snapName == "core" {
		return m.SystemSnapName()
	}
	return snapName
}

// SystemSnapName returns actual name of the system snap.
func (m *CoreSnapdSystemMapper) SystemSnapName() string {
	return "snapd"
}

// mapper contains the currently active snap mapper.
var mapper SnapMapper = &CoreCoreSystemMapper{}

// MockSnapMapper mocks the currently used snap mapper.
func MockSnapMapper(new SnapMapper) (restore func()) {
	old := mapper
	mapper = new
	return func() { mapper = old }
}

// RemapSnapFromState renames a snap when loaded from state according to the current mapper.
func RemapSnapFromState(snapName string) string {
	return mapper.RemapSnapFromState(snapName)
}

// RemapSnapToState renames a snap when saving to state according to the current mapper.
func RemapSnapToState(snapName string) string {
	return mapper.RemapSnapToState(snapName)
}

// RemapSnapFromRequest renames a snap as received from an API request according to the current mapper.
func RemapSnapFromRequest(snapName string) string {
	return mapper.RemapSnapFromRequest(snapName)
}

// SystemSnapName returns actual name of the system snap.
func SystemSnapName() string {
	return mapper.SystemSnapName()
}

// systemSnapInfo returns current info for system snap.
func systemSnapInfo(st *state.State) (*snap.Info, error) {
	return snapstate.CurrentInfo(st, SystemSnapName())
}

func connectDisconnectAffectedSnaps(t *state.Task) ([]string, error) {
	plugRef, slotRef := mylog.Check3(getPlugAndSlotRefs(t))

	return []string{plugRef.Snap, slotRef.Snap}, nil
}

func checkSystemSnapIsPresent(st *state.State) bool {
	st.Lock()
	defer st.Unlock()
	_ := mylog.Check2(systemSnapInfo(st))
	return err == nil
}

func setHotplugAttrs(task *state.Task, ifaceName string, hotplugKey snap.HotplugKey) {
	task.Set("interface", ifaceName)
	task.Set("hotplug-key", hotplugKey)
}

func getHotplugAttrs(task *state.Task) (ifaceName string, hotplugKey snap.HotplugKey, err error) {
	mylog.Check(task.Get("interface", &ifaceName))
	mylog.Check(task.Get("hotplug-key", &hotplugKey))

	return ifaceName, hotplugKey, err
}

func allocHotplugSeq(st *state.State) (int, error) {
	var seq int
	if mylog.Check(st.Get("hotplug-seq", &seq)); err != nil && !errors.Is(err, state.ErrNoState) {
		return 0, fmt.Errorf("internal error: cannot allocate hotplug sequence number: %s", err)
	}
	seq++
	st.Set("hotplug-seq", seq)
	return seq, nil
}

func isHotplugChange(chg *state.Change) bool {
	return strings.HasPrefix(chg.Kind(), "hotplug-")
}

func getHotplugChangeAttrs(chg *state.Change) (seq int, hotplugKey snap.HotplugKey, err error) {
	mylog.Check(chg.Get("hotplug-key", &hotplugKey))
	mylog.Check(chg.Get("hotplug-seq", &seq))

	return seq, hotplugKey, nil
}

func setHotplugChangeAttrs(chg *state.Change, seq int, hotplugKey snap.HotplugKey) {
	chg.Set("hotplug-seq", seq)
	chg.Set("hotplug-key", hotplugKey)
}

// addHotplugSeqWaitTask sets mandatory hotplug attributes on the hotplug change, adds "hotplug-seq-wait" task
// and makes all existing tasks of the change wait for it.
func addHotplugSeqWaitTask(hotplugChange *state.Change, hotplugKey snap.HotplugKey, hotplugSeq int) {
	st := hotplugChange.State()
	setHotplugChangeAttrs(hotplugChange, hotplugSeq, hotplugKey)
	seqControl := st.NewTask("hotplug-seq-wait", fmt.Sprintf("Serialize hotplug change for hotplug key %q", hotplugKey))
	tss := state.NewTaskSet(hotplugChange.Tasks()...)
	tss.WaitFor(seqControl)
	hotplugChange.AddTask(seqControl)
}

type HotplugSlotInfo struct {
	Name        string                 `json:"name"`
	Interface   string                 `json:"interface"`
	StaticAttrs map[string]interface{} `json:"static-attrs,omitempty"`
	HotplugKey  snap.HotplugKey        `json:"hotplug-key"`

	// device was unplugged but has connections, so slot is remembered
	HotplugGone bool `json:"hotplug-gone"`
}

func getHotplugSlots(st *state.State) (map[string]*HotplugSlotInfo, error) {
	var slots map[string]*HotplugSlotInfo
	mylog.Check(st.Get("hotplug-slots", &slots))

	return slots, nil
}

func setHotplugSlots(st *state.State, slots map[string]*HotplugSlotInfo) {
	st.Set("hotplug-slots", slots)
}

func findHotplugSlot(stateSlots map[string]*HotplugSlotInfo, ifaceName string, hotplugKey snap.HotplugKey) *HotplugSlotInfo {
	for _, slot := range stateSlots {
		if slot.HotplugKey == hotplugKey && slot.Interface == ifaceName {
			return slot
		}
	}
	return nil
}

func findConnsForHotplugKey(conns map[string]*schema.ConnState, ifaceName string, hotplugKey snap.HotplugKey) []string {
	var connsForDevice []string
	for id, connSt := range conns {
		if connSt.Interface != ifaceName || connSt.HotplugKey != hotplugKey {
			continue
		}
		connsForDevice = append(connsForDevice, id)
	}
	sort.Strings(connsForDevice)
	return connsForDevice
}

func (m *InterfaceManager) discardSecurityProfilesLate(name string, rev snap.Revision, typ snap.Type) error {
	for _, backend := range m.repo.Backends() {
		lateDiscardBackend, ok := backend.(interfaces.SecurityBackendDiscardingLate)
		if !ok {
			continue
		}
		mylog.Check(lateDiscardBackend.RemoveLate(name, rev, typ))

	}
	return nil
}

func hasActiveConnection(st *state.State, iface string) (bool, error) {
	conns := mylog.Check2(getConns(st))

	for _, cstate := range conns {
		// look for connected interface
		if !cstate.Undesired && !cstate.HotplugGone && cstate.Interface == iface {
			return true, nil
		}
	}
	return false, nil
}

func appSetForTask(t *state.Task, info *snap.Info) (*interfaces.SnapAppSet, error) {
	compsups := mylog.Check2(snapstate.ComponentSetupsForTask(t))

	compInfos := make([]*snap.ComponentInfo, 0, len(compsups))
	for _, compsup := range compsups {
		compInfo := mylog.Check2(snapstate.ComponentInfoFromComponentSetup(compsup, info))

		compInfos = append(compInfos, compInfo)
	}

	st := t.State()

	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, info.InstanceName(), &snapst))
	// if the snap isn't in the state, then we know that there aren't any
	// pre-existing components to consider

	// if we're installing/refreshing a component then we need to consider the
	// components that are already installed
	if snapst.LastIndex(info.Revision) != -1 {
		compsForRevision := mylog.Check2(snapst.ComponentInfosForRevision(info.Revision))

		compInfos = append(compInfos, compsForRevision...)
	}

	return interfaces.NewSnapAppSet(info, compInfos)
}

func appSetForSnapRevision(st *state.State, info *snap.Info) (*interfaces.SnapAppSet, error) {
	var snapst snapstate.SnapState
	mylog.Check(snapstate.Get(st, info.InstanceName(), &snapst))

	compInfos := mylog.Check2(snapst.ComponentInfosForRevision(info.Revision))

	return interfaces.NewSnapAppSet(info, compInfos)
}
