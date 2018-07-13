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
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/backends"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func (m *InterfaceManager) initialize(extraInterfaces []interfaces.Interface, extraBackends []interfaces.SecurityBackend) error {
	m.state.Lock()
	defer m.state.Unlock()

	if err := m.addInterfaces(extraInterfaces); err != nil {
		return err
	}
	if err := m.addBackends(extraBackends); err != nil {
		return err
	}
	if err := m.addSnaps(); err != nil {
		return err
	}
	if err := m.renameCorePlugConnection(); err != nil {
		return err
	}
	if err := removeStaleConnections(m.state); err != nil {
		return err
	}
	if _, err := m.reloadConnections(""); err != nil {
		return err
	}
	if m.profilesNeedRegeneration() {
		if err := m.regenerateAllSecurityProfiles(); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) addInterfaces(extra []interfaces.Interface) error {
	for _, iface := range builtin.Interfaces() {
		if err := m.repo.AddInterface(iface); err != nil {
			return err
		}
	}
	for _, iface := range extra {
		if err := m.repo.AddInterface(iface); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) addBackends(extra []interfaces.SecurityBackend) error {
	for _, backend := range backends.All {
		if err := backend.Initialize(); err != nil {
			return err
		}
		if err := m.repo.AddBackend(backend); err != nil {
			return err
		}
	}
	for _, backend := range extra {
		if err := backend.Initialize(); err != nil {
			return err
		}
		if err := m.repo.AddBackend(backend); err != nil {
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) addSnaps() error {
	snaps, err := snapsWithSecurityProfiles(m.state)
	if err != nil {
		return err
	}
	for _, snapInfo := range snaps {
		addImplicitSlots(snapInfo)
		if err := m.repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("cannot add snap %q to interface repository: %s", snapInfo.InstanceName(), err)
		}
	}
	return nil
}

func (m *InterfaceManager) profilesNeedRegeneration() bool {
	mismatch, err := interfaces.SystemKeyMismatch()
	if err != nil {
		logger.Noticef("error trying to compare the snap system key: %v", err)
		return true
	}
	return mismatch
}

// regenerateAllSecurityProfiles will regenerate all security profiles.
func (m *InterfaceManager) regenerateAllSecurityProfiles() error {
	// Get all the security backends
	securityBackends := m.repo.Backends()

	// Get all the snap infos
	snaps, err := snapsWithSecurityProfiles(m.state)
	if err != nil {
		return err
	}
	// Add implicit slots to all snaps
	for _, snapInfo := range snaps {
		addImplicitSlots(snapInfo)
	}

	// For each snap:
	for _, snapInfo := range snaps {
		snapName := snapInfo.InstanceName()
		// Get the state of the snap so we can compute the confinement option
		var snapst snapstate.SnapState
		if err := snapstate.Get(m.state, snapName, &snapst); err != nil {
			logger.Noticef("cannot get state of snap %q: %s", snapName, err)
		}

		// Compute confinement options
		opts := confinementOptions(snapst.Flags)

		// For each backend:
		for _, backend := range securityBackends {
			if backend.Name() == "" {
				continue // Test backends have no name, skip them to simplify testing.
			}
			// Refresh security of this snap and backend
			if err := backend.Setup(snapInfo, opts, m.repo); err != nil {
				// Let's log this but carry on
				logger.Noticef("cannot regenerate %s profile for snap %q: %s",
					backend.Name(), snapName, err)
			}
		}
	}

	if err := interfaces.WriteSystemKey(); err != nil {
		logger.Noticef("cannot write system key: %v", err)
	}
	return nil
}

// renameCorePlugConnection renames one connection from "core-support" plug to
// slot so that the plug name is "core-support-plug" while the slot is
// unchanged. This matches a change introduced in 2.24, where the core snap no
// longer has the "core-support" plug as that was clashing with the slot with
// the same name.
func (m *InterfaceManager) renameCorePlugConnection() error {
	conns, err := getConns(m.state)
	if err != nil {
		return err
	}
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
	conns, err := getConns(st)
	if err != nil {
		return err
	}
	var staleConns []string
	for id := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return err
		}
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, connRef.PlugRef.Snap, &snapst); err != nil {
			if err != state.ErrNoState {
				return err
			}
			staleConns = append(staleConns, id)
			continue
		}
		if err := snapstate.Get(st, connRef.SlotRef.Snap, &snapst); err != nil {
			if err != state.ErrNoState {
				return err
			}
			staleConns = append(staleConns, id)
			continue
		}
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

// reloadConnections reloads connections stored in the state in the repository.
// Using non-empty snapName the operation can be scoped to connections
// affecting a given snap.
//
// The return value is the list of affected snap names.
func (m *InterfaceManager) reloadConnections(snapName string) ([]string, error) {
	conns, err := getConns(m.state)
	if err != nil {
		return nil, err
	}
	affected := make(map[string]bool)
	for id, conn := range conns {
		if conn.Undesired {
			continue
		}
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return nil, err
		}
		if snapName != "" && connRef.PlugRef.Snap != snapName && connRef.SlotRef.Snap != snapName {
			continue
		}

		// Note: reloaded connections are not checked against policy again, and also we don't call BeforeConnect* methods on them.
		if _, err := m.repo.Connect(connRef, conn.DynamicPlugAttrs, conn.DynamicSlotAttrs, nil); err != nil {
			if _, ok := err.(*interfaces.UnknownPlugSlotError); ok {
				// Some versions of snapd may have left stray connections that
				// don't have the corresponding plug or slot anymore. Before we
				// choose how to deal with this data we want to silently ignore
				// that error not to worry the users.
				continue
			}
			logger.Noticef("%s", err)
		} else {
			affected[connRef.PlugRef.Snap] = true
			affected[connRef.SlotRef.Snap] = true
		}
	}
	result := make([]string, 0, len(affected))
	for name := range affected {
		result = append(result, name)
	}
	return result, nil
}

func (m *InterfaceManager) setupSnapSecurity(task *state.Task, snapInfo *snap.Info, opts interfaces.ConfinementOptions) error {
	st := task.State()
	snapName := snapInfo.InstanceName()

	for _, backend := range m.repo.Backends() {
		st.Unlock()
		err := backend.Setup(snapInfo, opts, m.repo)
		st.Lock()
		if err != nil {
			task.Errorf("cannot setup %s for snap %q: %s", backend.Name(), snapName, err)
			return err
		}
	}
	return nil
}

func (m *InterfaceManager) removeSnapSecurity(task *state.Task, snapName string) error {
	st := task.State()
	for _, backend := range m.repo.Backends() {
		st.Unlock()
		err := backend.Remove(snapName)
		st.Lock()
		if err != nil {
			task.Errorf("cannot setup %s for snap %q: %s", backend.Name(), snapName, err)
			return err
		}
	}
	return nil
}

type connState struct {
	Auto      bool   `json:"auto,omitempty"`
	ByGadget  bool   `json:"by-gadget,omitempty"`
	Interface string `json:"interface,omitempty"`
	// Undesired tracks connections that were manually disconnected after being auto-connected,
	// so that they are not automatically reconnected again in the future.
	Undesired        bool                   `json:"undesired,omitempty"`
	StaticPlugAttrs  map[string]interface{} `json:"plug-static,omitempty"`
	DynamicPlugAttrs map[string]interface{} `json:"plug-dynamic,omitempty"`
	StaticSlotAttrs  map[string]interface{} `json:"slot-static,omitempty"`
	DynamicSlotAttrs map[string]interface{} `json:"slot-dynamic,omitempty"`
}

type autoConnectChecker struct {
	st       *state.State
	cache    map[string]*asserts.SnapDeclaration
	baseDecl *asserts.BaseDeclaration
}

func newAutoConnectChecker(s *state.State) (*autoConnectChecker, error) {
	baseDecl, err := assertstate.BaseDeclaration(s)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find base declaration: %v", err)
	}
	return &autoConnectChecker{
		st:       s,
		cache:    make(map[string]*asserts.SnapDeclaration),
		baseDecl: baseDecl,
	}, nil
}

func (c *autoConnectChecker) snapDeclaration(snapID string) (*asserts.SnapDeclaration, error) {
	snapDecl := c.cache[snapID]
	if snapDecl != nil {
		return snapDecl, nil
	}
	snapDecl, err := assertstate.SnapDeclaration(c.st, snapID)
	if err != nil {
		return nil, err
	}
	c.cache[snapID] = snapDecl
	return snapDecl, nil
}

func (c *autoConnectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, error) {
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		var err error
		plugDecl, err = c.snapDeclaration(plug.Snap().SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", plug.Snap().InstanceName(), err)
			return false, nil
		}
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		var err error
		slotDecl, err = c.snapDeclaration(slot.Snap().SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", slot.Snap().InstanceName(), err)
			return false, nil
		}
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     c.baseDecl,
	}

	return ic.CheckAutoConnect() == nil, nil
}

type connectChecker struct {
	st       *state.State
	baseDecl *asserts.BaseDeclaration
}

func newConnectChecker(s *state.State) (*connectChecker, error) {
	baseDecl, err := assertstate.BaseDeclaration(s)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find base declaration: %v", err)
	}
	return &connectChecker{
		st:       s,
		baseDecl: baseDecl,
	}, nil
}

func (c *connectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) (bool, error) {
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		var err error
		plugDecl, err = assertstate.SnapDeclaration(c.st, plug.Snap().SnapID)
		if err != nil {
			return false, fmt.Errorf("cannot find snap declaration for %q: %v", plug.Snap().InstanceName(), err)
		}
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		var err error
		slotDecl, err = assertstate.SnapDeclaration(c.st, slot.Snap().SnapID)
		if err != nil {
			return false, fmt.Errorf("cannot find snap declaration for %q: %v", slot.Snap().InstanceName(), err)
		}
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     c.baseDecl,
	}

	// if either of plug or slot snaps don't have a declaration it
	// means they were installed with "dangerous", so the security
	// check should be skipped at this point.
	if plugDecl != nil && slotDecl != nil {
		if err := ic.Check(); err != nil {
			return false, err
		}
	}
	return true, nil
}

func getPlugAndSlotRefs(task *state.Task) (interfaces.PlugRef, interfaces.SlotRef, error) {
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	if err := task.Get("plug", &plugRef); err != nil {
		return plugRef, slotRef, err
	}
	if err := task.Get("slot", &slotRef); err != nil {
		return plugRef, slotRef, err
	}
	return plugRef, slotRef, nil
}

// getConns returns information about connections from the state.
//
// Connections are transparently re-mapped according to remapIncomingConnRef
func getConns(st *state.State) (conns map[string]connState, err error) {
	err = st.Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("cannot obtain data about existing connections: %s", err)
	}
	if conns == nil {
		conns = make(map[string]connState)
	}
	remapped := make(map[string]connState, len(conns))
	for id, cstate := range conns {
		cref, err := interfaces.ParseConnRef(id)
		if err != nil {
			return nil, err
		}
		cref.PlugRef = RemapPlugRefFromState(cref.PlugRef)
		cref.SlotRef = RemapSlotRefFromState(cref.SlotRef)
		remapped[cref.ID()] = cstate
	}
	return remapped, nil
}

// setConns sets information about connections in the state.
//
// Connections are transparently re-mapped according to remapOutgoingConnRef
func setConns(st *state.State, conns map[string]connState) {
	remapped := make(map[string]connState, len(conns))
	for id, cstate := range conns {
		cref, err := interfaces.ParseConnRef(id)
		if err != nil {
			// We cannot fail here
			panic(err)
		}
		cref.PlugRef = RemapPlugRefToState(cref.PlugRef)
		cref.SlotRef = RemapSlotRefToState(cref.SlotRef)
		remapped[cref.ID()] = cstate
	}
	st.Set("conns", remapped)
}

// snapsWithSecurityProfiles returns all snaps that have active
// security profiles: these are either snaps that are active, or about
// to be active (pending link-snap) with a done setup-profiles
func snapsWithSecurityProfiles(st *state.State) ([]*snap.Info, error) {
	infos, err := snapstate.ActiveInfos(st)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(infos))
	for _, info := range infos {
		seen[info.InstanceName()] = true
	}
	for _, t := range st.Tasks() {
		if t.Kind() != "link-snap" || t.Status().Ready() {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(t)
		if err != nil {
			return nil, err
		}
		snapName := snapsup.Name()
		if seen[snapName] {
			continue
		}

		doneProfiles := false
		for _, t1 := range t.WaitTasks() {
			if t1.Kind() == "setup-profiles" && t1.Status() == state.DoneStatus {
				snapsup1, err := snapstate.TaskSnapSetup(t)
				if err != nil {
					return nil, err
				}
				if snapsup1.Name() == snapName {
					doneProfiles = true
					break
				}
			}
		}
		if !doneProfiles {
			continue
		}

		seen[snapName] = true
		snapInfo, err := snap.ReadInfo(snapName, snapsup.SideInfo)
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", snapName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}

	return infos, nil
}

func resolveSnapIDToName(st *state.State, snapID string) (name string, err error) {
	if snapID == "system" {
		return snap.DropNick(snapID), nil
	}
	decl, err := assertstate.SnapDeclaration(st, snapID)
	if asserts.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return decl.SnapName(), nil
}

// InterfaceMapper offers APIs for re-mapping plugs and slots in interface
// connections. The mapper is designed to apply transformations around the
// edges of snapd (state interactions and API interactions) to offer one view
// on the inside of snapd and another view on the outside.
type InterfaceMapper interface {
	// re-map functions for loading and saving objects in the state.
	RemapPlugRefFromState(plugRef *interfaces.PlugRef)
	RemapSlotRefFromState(slotRef *interfaces.SlotRef)
	RemapPlugRefToState(plugRef *interfaces.PlugRef)
	RemapSlotRefToState(slotRef *interfaces.SlotRef)

	// re-map functions for API requests/responses.
	RemapPlugRefFromRequest(plugRef *interfaces.PlugRef)
	RemapSlotRefFromRequest(slotRef *interfaces.SlotRef)
	RemapPlugRefToResponse(plugRef *interfaces.PlugRef)
	RemapSlotRefToResponse(slotRef *interfaces.SlotRef)
}

// NilMapper implements InterfaceMapper and performs no transformations at all.
type NilMapper struct{}

// RemapPlugRefFromState doesn't change the plug in any way.
func (m *NilMapper) RemapPlugRefFromState(plugRef *interfaces.PlugRef) {}

// RemapSlotRefFromState doesn't change the plug in any way.
func (m *NilMapper) RemapSlotRefFromState(slotRef *interfaces.SlotRef) {}

// RemapPlugRefToState doesn't change the plug in any way.
func (m *NilMapper) RemapPlugRefToState(plugRef *interfaces.PlugRef) {}

// RemapSlotRefToState doesn't change the plug in any way.
func (m *NilMapper) RemapSlotRefToState(slotRef *interfaces.SlotRef) {}

// RemapPlugRefFromRequest doesn't change the plug in any way.
func (m *NilMapper) RemapPlugRefFromRequest(plugRef *interfaces.PlugRef) {}

// RemapSlotRefFromRequest doesn't change the plug in any way.
func (m *NilMapper) RemapSlotRefFromRequest(slotRef *interfaces.SlotRef) {}

// RemapPlugRefToResponse doesn't change the plug in any way.
func (m *NilMapper) RemapPlugRefToResponse(plugRef *interfaces.PlugRef) {}

// RemapSlotRefToResponse doesn't change the plug in any way.
func (m *NilMapper) RemapSlotRefToResponse(slotRef *interfaces.SlotRef) {}

// mapper contains the currently active interface mapper.
var mapper InterfaceMapper = &NilMapper{}

// MockInterfaceMapper mocks the currently used interface mapper.
func MockInterfaceMapper(new InterfaceMapper) (restore func()) {
	old := mapper
	mapper = new
	return func() { mapper = old }
}

// CoreCoreSystemMapper implements InterfaceMapper and makes implicit slots
// appear to be on "core" in the state and in memory but as "system" in the API.
//
// NOTE: This mapper can be used to prepare, as an intermediate step, for the
// transition to "snapd" mapper. Using it the state and API layer will look
// exactly the same as with the "snapd" mapper. This can be used to make any
// necessary adjustments the test suite.
type CoreCoreSystemMapper struct {
	NilMapper // Embedding the nil mapper allows us to cut on boilerplate.
}

// RemapSlotRefFromRequest moves slots from "system" snaps to the "core" snap.
//
// This allows us to accept connection and disconnection requests that
// explicitly refer to "core" or using the "system" nickname.
func (m *CoreCoreSystemMapper) RemapSlotRefFromRequest(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "system" {
		slotRef.Snap = "core"
	}
}

// RemapSlotRefToResponse makes slots from "core" snap to the "system" snap.
//
// This allows us to make all the implicitly defined slots, that are really
// associated with the "core" snap to seemingly occupy the "system" snap
// instead.
func (m *CoreCoreSystemMapper) RemapSlotRefToResponse(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "core" {
		slotRef.Snap = "system"
	}
}

// CoreSnapdSystemMapper implements InterfaceMapper and makes implicit slots
// appear to be on "core" in the state and on "system" in the API while they
// are on "snapd" in memory.
type CoreSnapdSystemMapper struct {
	NilMapper // Embedding the nil mapper allows us to cut on boilerplate.
}

// RemapSlotRefFromState moves slots from the "core" snap to the "snapd" snap.
//
// This allows modern snapd to load an old state that remembers connections
// between slots on the "core" snap and other snaps. In memory we are actually
// using "snapd" snap for hosting those slots and this lets us stay compatible.
func (m *CoreSnapdSystemMapper) RemapSlotRefFromState(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "core" {
		slotRef.Snap = "snapd"
	}
}

// RemapSlotRefToState moves slots from the "snapd" snap to the "core" snap.
//
// This allows the state to stay backwards compatible as all the connections
// seem to refer to the "core" snap, as in pre core{16,18} days where there was
// only one core snap.
func (m *CoreSnapdSystemMapper) RemapSlotRefToState(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "snapd" {
		slotRef.Snap = "core"
	}
}

// RemapSlotRefFromRequest moves slots from "core" or "system" snaps to the "snapd" snap.
//
// This allows us to accept connection and disconnection requests that
// explicitly refer to "core" or "system" even though we really want them to
// refer to "snapd". Note that this is not fully symmetric with
// RemapSlotRefToResponse as we explicitly always talk about "system" snap,
// even if the request used "core".
func (m *CoreSnapdSystemMapper) RemapSlotRefFromRequest(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "core" || slotRef.Snap == "system" {
		slotRef.Snap = "snapd"
	}
}

// RemapSlotRefToResponse makes slots from "snapd" snap to the "system" snap.
//
// This allows us to make all the implicitly defined slots, that are really
// associated with the "snapd" snap to seemingly occupy the "system" snap
// instead. This ties into the concept of using "system" as a nickname (e.g. in
// gadget snap connections).
func (m *CoreSnapdSystemMapper) RemapSlotRefToResponse(slotRef *interfaces.SlotRef) {
	if slotRef.Snap == "snapd" {
		slotRef.Snap = "system"
	}
}

// Remapping functions for state => memory transitions.

func RemapPlugRefFromState(plugRef interfaces.PlugRef) interfaces.PlugRef {
	mapper.RemapPlugRefFromState(&plugRef)
	return plugRef
}

func RemapSlotRefFromState(slotRef interfaces.SlotRef) interfaces.SlotRef {
	mapper.RemapSlotRefFromState(&slotRef)
	return slotRef
}

// Remapping functions for memory => state transitions.

func RemapPlugRefToState(plugRef interfaces.PlugRef) interfaces.PlugRef {
	mapper.RemapPlugRefToState(&plugRef)
	return plugRef
}

func RemapSlotRefToState(slotRef interfaces.SlotRef) interfaces.SlotRef {
	mapper.RemapSlotRefToState(&slotRef)
	return slotRef
}

// Remapping functions for wire => memory (API requests)

func RemapPlugRefFromRequest(plugRef interfaces.PlugRef) interfaces.PlugRef {
	mapper.RemapPlugRefFromRequest(&plugRef)
	return plugRef
}

func RemapSlotRefFromRequest(slotRef interfaces.SlotRef) interfaces.SlotRef {
	mapper.RemapSlotRefFromRequest(&slotRef)
	return slotRef
}

// Remapping functions for memory => wire (API responses)

func RemapPlugRefToResponse(plugRef interfaces.PlugRef) interfaces.PlugRef {
	mapper.RemapPlugRefToResponse(&plugRef)
	return plugRef
}

func RemapSlotRefToResponse(slotRef interfaces.SlotRef) interfaces.SlotRef {
	mapper.RemapSlotRefToResponse(&slotRef)
	return slotRef
}
