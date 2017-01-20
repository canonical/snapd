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

func (m *InterfaceManager) initialize(extra []interfaces.Interface) error {
	m.state.Lock()
	defer m.state.Unlock()

	if err := m.addInterfaces(extra); err != nil {
		return err
	}
	if err := m.addSnaps(); err != nil {
		return err
	}
	if err := m.reloadConnections(""); err != nil {
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

func (m *InterfaceManager) addSnaps() error {
	snaps, err := snapstate.ActiveInfos(m.state)
	if err != nil {
		return err
	}
	for _, snapInfo := range snaps {
		snap.AddImplicitSlots(snapInfo)
		if err := m.repo.AddSnap(snapInfo); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

// reloadConnections reloads connections stored in the state in the repository.
// Using non-empty snapName the operation can be scoped to connections
// affecting a given snap.
func (m *InterfaceManager) reloadConnections(snapName string) error {
	conns, err := getConns(m.state)
	if err != nil {
		return err
	}
	for id := range conns {
		var connRef interfaces.ConnRef
		if err := connRef.ParseID(id); err != nil {
			return err
		}
		if snapName != "" && connRef.PlugRef.Snap != snapName && connRef.SlotRef.Snap != snapName {
			continue
		}
		if err := m.repo.Connect(connRef); err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

func setupSnapSecurity(task *state.Task, snapInfo *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
	st := task.State()
	snapName := snapInfo.Name()

	for _, backend := range backends.All {
		st.Unlock()
		err := backend.Setup(snapInfo, opts, repo)
		st.Lock()
		if err != nil {
			task.Errorf("cannot setup %s for snap %q: %s", backend.Name(), snapName, err)
			return err
		}
	}
	return nil
}

func removeSnapSecurity(task *state.Task, snapName string) error {
	st := task.State()
	for _, backend := range backends.All {
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
	Interface string `json:"interface,omitempty"`
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

func (c *autoConnectChecker) check(plug *interfaces.Plug, slot *interfaces.Slot) bool {
	var plugDecl *asserts.SnapDeclaration
	if plug.Snap.SnapID != "" {
		var err error
		plugDecl, err = c.snapDeclaration(plug.Snap.SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", plug.Snap.Name(), err)
			return false
		}
	}

	var slotDecl *asserts.SnapDeclaration
	if slot.Snap.SnapID != "" {
		var err error
		slotDecl, err = c.snapDeclaration(slot.Snap.SnapID)
		if err != nil {
			logger.Noticef("error: cannot find snap declaration for %q: %v", slot.Snap.Name(), err)
			return false
		}
	}

	// check the connection against the declarations' rules
	ic := policy.ConnectCandidate{
		Plug:                plug.PlugInfo,
		PlugSnapDeclaration: plugDecl,
		Slot:                slot.SlotInfo,
		SlotSnapDeclaration: slotDecl,
		BaseDeclaration:     c.baseDecl,
	}

	return ic.CheckAutoConnect() == nil
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
		if len(candidates) != 1 {
			continue
		}
		slot := candidates[0]
		connRef := interfaces.ConnRef{PlugRef: plug.Ref(), SlotRef: slot.Ref()}
		key := connRef.ID()
		if _, ok := conns[key]; ok {
			// Suggested connection already exist so don't clobber it.
			continue
		}
		if err := m.repo.Connect(connRef); err != nil {
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
		if len(candidates) != 1 {
			continue
		}
		plug := candidates[0]
		connRef := interfaces.ConnRef{PlugRef: plug.Ref(), SlotRef: slot.Ref()}
		key := connRef.ID()
		if _, ok := conns[key]; ok {
			// Suggested connection already exist so don't clobber it.
			continue
		}
		if err := m.repo.Connect(connRef); err != nil {
			task.Logf("cannot auto connect %s to %s: %s (slot auto-connection)", connRef.PlugRef, connRef.SlotRef, err)
			continue
		}
		affectedSnapNames = append(affectedSnapNames, connRef.PlugRef.Snap)
		affectedSnapNames = append(affectedSnapNames, connRef.SlotRef.Snap)
		conns[key] = connState{Interface: plug.Interface, Auto: true}
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

// CheckInterfaces checks whether plugs and slots of snap are allowed for installation.
func CheckInterfaces(st *state.State, snapInfo *snap.Info) error {
	// XXX: AddImplicitSlots is really a brittle interface
	snap.AddImplicitSlots(snapInfo)

	if snapInfo.SnapID == "" {
		// no SnapID means --dangerous was given, so skip interface checks
		return nil
	}

	baseDecl, err := assertstate.BaseDeclaration(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot find base declaration: %v", err)
	}

	snapDecl, err := assertstate.SnapDeclaration(st, snapInfo.SnapID)
	if err != nil {
		return fmt.Errorf("cannot find snap declaration for %q: %v", snapInfo.Name(), err)
	}

	ic := policy.InstallCandidate{
		Snap:            snapInfo,
		SnapDeclaration: snapDecl,
		BaseDeclaration: baseDecl,
	}

	return ic.Check()
}

func init() {
	// hook interface checks into snapstate installation logic
	snapstate.AddCheckSnapCallback(func(st *state.State, snapInfo, _ *snap.Info, _ snapstate.Flags) error {
		return CheckInterfaces(st, snapInfo)
	})
}
