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

	snaps, err := snapsWithSecurityProfiles(m.state)
	if err != nil {
		return err
	}
	// Before deciding about adding implicit slots to any snap we need to scan
	// the set of snaps we know about. If any of those is "snapd" then for the
	// duration of this process always add implicit slots to snapd and not to
	// any other type: os snap and use a mapper to use names core-snapd-system
	// on state, in memory and in API responses, respectively.
	m.selectInterfaceMapper(snaps)

	if err := m.addInterfaces(extraInterfaces); err != nil {
		return err
	}
	if err := m.addBackends(extraBackends); err != nil {
		return err
	}
	m.addSnaps(snaps)
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

func (m *InterfaceManager) selectInterfaceMapper(snaps []*snap.Info) {
	for _, snapInfo := range snaps {
		if snapInfo.SnapName() == "snapd" {
			mapper = &CoreSnapdSystemMapper{}
			break
		}
	}
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

func (m *InterfaceManager) addSnaps(snaps []*snap.Info) {
	for _, snapInfo := range snaps {
		addImplicitSlots(snapInfo)
		if err := m.repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("cannot add snap %q to interface repository: %s", snapInfo.InstanceName(), err)
		}
	}
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
		cref.PlugRef.Snap = RemapSnapFromState(cref.PlugRef.Snap)
		cref.SlotRef.Snap = RemapSnapFromState(cref.SlotRef.Snap)
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
		cref.PlugRef.Snap = RemapSnapToState(cref.PlugRef.Snap)
		cref.SlotRef.Snap = RemapSnapToState(cref.SlotRef.Snap)
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
		snapName := snapsup.InstanceName()
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
				if snapsup1.InstanceName() == snapName {
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
		// Resolve the system nickname to a concrete snap.
		return mapper.RemapSnapFromRequest(snapID), nil
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

// SnapMapper offers APIs for re-mapping snap names in interfaces and the
// configuration system. The mapper is designed to apply transformations around
// the edges of snapd (state interactions and API interactions) to offer one
// view on the inside of snapd and another view on the outside.
type SnapMapper interface {
	// re-map functions for loading and saving objects in the state.
	RemapSnapFromState(snapName string) string
	RemapSnapToState(snapName string) string
	// re-map functions for API requests/responses.
	RemapSnapFromRequest(snapName string) string
	RemapSnapToResponse(snapName string) string
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

// RemapSnapToResponse doesn't change the snap name in any way.
func (m *IdentityMapper) RemapSnapToResponse(snapName string) string {
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
		return "core"
	}
	return snapName
}

// RemapSnapToResponse renames the "core" snap to the "system" snap.
//
// This allows us to make all the implicitly defined slots, that are really
// associated with the "core" snap to seemingly occupy the "system" snap
// instead.
func (m *CoreCoreSystemMapper) RemapSnapToResponse(snapName string) string {
	if snapName == "core" {
		return "system"
	}
	return snapName
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
		return "snapd"
	}
	return snapName
}

// RemapSnapToState renames the "snapd" snap to the "core" snap.
//
// This allows the state to stay backwards compatible as all the connections
// seem to refer to the "core" snap, as in pre core{16,18} days where there was
// only one core snap.
func (m *CoreSnapdSystemMapper) RemapSnapToState(snapName string) string {
	if snapName == "snapd" {
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
		return "snapd"
	}
	return snapName
}

// RemapSnapToResponse renames the "snapd" snap to the "system" snap.
//
// This allows us to make all the implicitly defined slots, that are really
// associated with the "snapd" snap to seemingly occupy the "system" snap
// instead. This ties into the concept of using "system" as a nickname (e.g. in
// gadget snap connections).
func (m *CoreSnapdSystemMapper) RemapSnapToResponse(snapName string) string {
	if snapName == "snapd" {
		return "system"
	}
	return snapName
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

// RemapSnapFromRequest  renames a snap as received from an API request according to the current mapper.
func RemapSnapFromRequest(snapName string) string {
	return mapper.RemapSnapFromRequest(snapName)
}

// RemapSnapToResponse renames a snap as about to be sent from an API response according to the current mapper.
func RemapSnapToResponse(snapName string) string {
	return mapper.RemapSnapToResponse(snapName)
}

func connectDisconnectAffectedSnaps(t *state.Task) ([]string, error) {
	plugRef, slotRef, err := getPlugAndSlotRefs(t)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot obtain plug/slot data from task: %s", t.Summary())
	}
	return []string{plugRef.Snap, slotRef.Snap}, nil
}
