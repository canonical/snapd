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
	if _, err := m.reloadConnections(""); err != nil {
		return err
	}
	if err := m.regenerateAllSecurityProfiles(); err != nil {
		return err
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
	snaps, err := snapstate.ActiveInfos(m.state)
	if err != nil {
		return err
	}
	for _, snapInfo := range snaps {
		addImplicitSlots(snapInfo)
		if err := m.repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

// regenerateAllSecurityProfiles will regenerate the security profiles
// for apparmor and seccomp. This is needed because:
// - for seccomp we may have "terms" on disk that the current snap-confine
//   does not understand (e.g. in a rollback scenario). a refresh ensures
//   we have a profile that matches what snap-confine understand
// - for apparmor the kernel 4.4.0-65.86 has an incompatible apparmor
//   change that breaks existing profiles for installed snaps. With a
//   refresh those get fixed.
func (m *InterfaceManager) regenerateAllSecurityProfiles() error {
	// Get all the security backends
	securityBackends := m.repo.Backends()

	// Get all the snap infos
	snaps, err := snapstate.ActiveInfos(m.state)
	if err != nil {
		return err
	}
	// Add implicit slots to all snaps
	for _, snapInfo := range snaps {
		addImplicitSlots(snapInfo)
	}

	// For each snap:
	for _, snapInfo := range snaps {
		snapName := snapInfo.Name()
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
	for id, cn := range conns {
		connRef, err := interfaces.ParseConnRef(id)
		if err != nil {
			return nil, err
		}
		if snapName != "" && connRef.PlugRef.Snap != snapName && connRef.SlotRef.Snap != snapName {
			continue
		}
		// Note: reloaded connections are not checked against policy again.
		if _, err := m.repo.Connect(connRef, cn.DynamicPlugAttrs, cn.DynamicSlotAttrs, nil); err != nil {
			logger.Noticef("%s", err)
		}
		affected[connRef.PlugRef.Snap] = true
		affected[connRef.SlotRef.Snap] = true
	}
	result := make([]string, 0, len(affected))
	for name := range affected {
		result = append(result, name)
	}
	return result, nil
}

func (m *InterfaceManager) setupSnapSecurity(task *state.Task, snapInfo *snap.Info, opts interfaces.ConfinementOptions) error {
	st := task.State()
	snapName := snapInfo.Name()

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
	Auto             bool                   `json:"auto,omitempty"`
	Interface        string                 `json:"interface,omitempty"`
	DynamicPlugAttrs map[string]interface{} `json:"dynamic-plug-attrs,omitempty"`
	DynamicSlotAttrs map[string]interface{} `json:"dynamic-slot-attrs,omitempty"`
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

func (c *autoConnectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) bool {
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		var err error
		plugDecl, err = c.snapDeclaration(plug.Snap().SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", plug.Snap().Name(), err)
			return false
		}
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		var err error
		slotDecl, err = c.snapDeclaration(slot.Snap().SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", slot.Snap().Name(), err)
			return false
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

	return ic.CheckAutoConnect() == nil
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

func (c *connectChecker) check(plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap().SnapID != "" {
		var err error
		plugDecl, err = assertstate.SnapDeclaration(c.st, plug.Snap().SnapID)
		if err != nil {
			return fmt.Errorf("cannot find snap declaration for %q: %v", plug.Snap().Name(), err)
		}
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap().SnapID != "" {
		var err error
		slotDecl, err = assertstate.SnapDeclaration(c.st, slot.Snap().SnapID)
		if err != nil {
			return fmt.Errorf("cannot find snap declaration for %q: %v", slot.Snap().Name(), err)
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
			return err
		}
	}
	return nil
}

// autoConnect connects the given snap to viable candidates returning the list
// of connected snap names.  The blacklist can prevent auto-connection to
// specific interfaces (blacklist entries are plug or slot names).
func (m *InterfaceManager) autoConnect(task *state.Task, snapName string, blacklist map[string]bool) ([]string, error) {
	var conns map[string]connState
	var affectedSnapNames []string
	err := task.State().Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if conns == nil {
		conns = make(map[string]connState)
	}

	autochecker, err := newAutoConnectChecker(task.State())
	if err != nil {
		return nil, err
	}

	// Auto-connect all the plugs
	for _, plug := range m.repo.Plugs(snapName) {
		if blacklist[plug.Name] {
			continue
		}
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
			crefs := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				crefs = append(crefs, candidate.String())
			}
			task.Logf("cannot auto connect %s (plug auto-connection), candidates found: %q", plug, strings.Join(crefs, ", "))
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
		// FIXME: need to pass connect policy check once autoconnect supports interface hooks
		if _, err := m.repo.Connect(*connRef, nil, nil, nil); err != nil {
			task.Logf("cannot auto connect %s to %s: %s (plug auto-connection)", connRef.PlugRef, connRef.SlotRef, err)
			continue
		}
		affectedSnapNames = append(affectedSnapNames, connRef.PlugRef.Snap)
		affectedSnapNames = append(affectedSnapNames, connRef.SlotRef.Snap)
		conns[key] = connState{Interface: plug.Interface, Auto: true}
	}
	// Auto-connect all the slots
	for _, slot := range m.repo.Slots(snapName) {
		if blacklist[slot.Name] {
			continue
		}
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
				crefs := make([]string, 0, len(candSlots))
				for _, candidate := range candSlots {
					crefs = append(crefs, candidate.String())
				}
				task.Logf("cannot auto connect %s to %s (slot auto-connection), alternatives found: %q", slot, plug, strings.Join(crefs, ", "))
				continue
			}

			connRef := interfaces.NewConnRef(plug, slot)
			key := connRef.ID()
			if _, ok := conns[key]; ok {
				// Suggested connection already exist so don't clobber it.
				// NOTE: we don't log anything here as this is a normal and common condition.
				continue
			}
			// FIXME: need to pass connect policy check once autoconnect supports interface hooks
			if _, err := m.repo.Connect(*connRef, nil, nil, nil); err != nil {
				task.Logf("cannot auto connect %s to %s: %s (slot auto-connection)", connRef.PlugRef, connRef.SlotRef, err)
				continue
			}
			affectedSnapNames = append(affectedSnapNames, connRef.PlugRef.Snap)
			affectedSnapNames = append(affectedSnapNames, connRef.SlotRef.Snap)
			conns[key] = connState{Interface: plug.Interface, Auto: true}
		}
	}

	task.State().Set("conns", conns)
	return affectedSnapNames, nil
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

func getConns(st *state.State) (map[string]connState, error) {
	// Get information about connections from the state
	var conns map[string]connState
	err := st.Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return nil, fmt.Errorf("cannot obtain data about existing connections: %s", err)
	}
	if conns == nil {
		conns = make(map[string]connState)
	}
	return conns, nil
}

func setConns(st *state.State, conns map[string]connState) {
	st.Set("conns", conns)
}
