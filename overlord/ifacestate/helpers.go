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

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/apparmor"
	"github.com/ubuntu-core/snappy/interfaces/builtin"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/interfaces/seccomp"
	"github.com/ubuntu-core/snappy/interfaces/udev"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
	"github.com/ubuntu-core/snappy/snap"
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
		plugRef, slotRef, err := parseConnID(id)
		if err != nil {
			return err
		}
		if snapName != "" && plugRef.Snap != snapName && slotRef.Snap != snapName {
			continue
		}
		err = m.repo.Connect(plugRef.Snap, plugRef.Name, slotRef.Snap, slotRef.Name)
		if err != nil {
			logger.Noticef("%s", err)
		}
	}
	return nil
}

func setupSnapSecurity(task *state.Task, snapInfo *snap.Info, repo *interfaces.Repository) error {
	st := task.State()
	var snapState snapstate.SnapState
	snapName := snapInfo.Name()
	if err := snapstate.Get(st, snapName, &snapState); err != nil {
		task.Errorf("cannot get state of snap %q: %s", snapName, err)
		return err
	}
	for _, backend := range securityBackends {
		st.Unlock()
		err := backend.Setup(snapInfo, snapState.DevMode(), repo)
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
	for _, backend := range securityBackends {
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

func connID(plug *interfaces.PlugRef, slot *interfaces.SlotRef) string {
	return fmt.Sprintf("%s:%s %s:%s", plug.Snap, plug.Name, slot.Snap, slot.Name)
}

func parseConnID(conn string) (*interfaces.PlugRef, *interfaces.SlotRef, error) {
	parts := strings.SplitN(conn, " ", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("malformed connection identifier: %q", conn)
	}
	plugParts := strings.SplitN(parts[0], ":", 2)
	slotParts := strings.SplitN(parts[1], ":", 2)
	if len(plugParts) != 2 || len(slotParts) != 2 {
		return nil, nil, fmt.Errorf("malformed connection identifier: %q", conn)
	}
	plugRef := &interfaces.PlugRef{Snap: plugParts[0], Name: plugParts[1]}
	slotRef := &interfaces.SlotRef{Snap: slotParts[0], Name: slotParts[1]}
	return plugRef, slotRef, nil
}

func (m *InterfaceManager) autoConnect(task *state.Task, snapName string, blacklist map[string]bool) error {
	var conns map[string]connState
	err := task.State().Get("conns", &conns)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if conns == nil {
		conns = make(map[string]connState)
	}
	// XXX: quick hack, auto-connect everything
	for _, plug := range m.repo.Plugs(snapName) {
		if blacklist[plug.Name] {
			continue
		}
		candidates := m.repo.AutoConnectCandidates(snapName, plug.Name)
		if len(candidates) != 1 {
			continue
		}
		slot := candidates[0]
		if err := m.repo.Connect(snapName, plug.Name, slot.Snap.Name(), slot.Name); err != nil {
			task.Logf("cannot auto connect %s:%s to %s:%s: %s",
				snapName, plug.Name, slot.Snap.Name(), slot.Name, err)
		}
		key := fmt.Sprintf("%s:%s %s:%s", snapName, plug.Name, slot.Snap.Name(), slot.Name)
		conns[key] = connState{Interface: plug.Interface, Auto: true}
	}
	task.State().Set("conns", conns)
	return nil
}

func getPlugAndSlotRefs(task *state.Task) (*interfaces.PlugRef, *interfaces.SlotRef, error) {
	var plugRef interfaces.PlugRef
	var slotRef interfaces.SlotRef
	if err := task.Get("plug", &plugRef); err != nil {
		return nil, nil, err
	}
	if err := task.Get("slot", &slotRef); err != nil {
		return nil, nil, err
	}
	return &plugRef, &slotRef, nil
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

var securityBackends = []interfaces.SecurityBackend{
	&apparmor.Backend{}, &seccomp.Backend{}, &dbus.Backend{}, &udev.Backend{},
}
