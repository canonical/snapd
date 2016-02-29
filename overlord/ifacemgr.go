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

package overlord

// InterfaceManager is responsible for the maintenance of interfaces in
// the system state.  It maintains interface connections, and also observes
// installed snaps to track the current set of available plugs and slots.
type InterfaceManager struct {
	o *Overlord
}

// NewInterfaceManager returns a new InterfaceManager.
func NewInterfaceManager(o *Overlord) (*InterfaceManager, error) {
	return &InterfaceManager{o: o}, nil
}

// Connect initiates a change connecting an interface.
func (m *InterfaceManager) Connect(plugSnap, plugName, slotSnap, slotName string) error {
	return nil
}

// Disconnect initiates a change disconnecting an interface.
func (m *InterfaceManager) Disconnect(plugSnap, plugName, slotSnap, slotName string) error {
	return nil
}

// Init implements StateManager.Init.
func (m *InterfaceManager) Init(s *State) error {
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *InterfaceManager) Ensure() error {
	return nil
}

// Stop implements StateManager.Stop.
func (m *InterfaceManager) Stop() error {
	return nil
}
