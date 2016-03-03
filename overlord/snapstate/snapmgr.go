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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"github.com/ubuntu-core/snappy/overlord/state"
)

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct{}

// Manager returns a new snap manager.
func Manager() (*SnapManager, error) {
	return &SnapManager{}, nil
}

// Install initiates a change installing snap.
func (m *SnapManager) Install(snap string) error {
	return nil
}

// Remove initiates a change removing snap.
func (m *SnapManager) Remove(snap string) error {
	return nil
}

// Init implements StateManager.Init.
func (m *SnapManager) Init(s *state.State) error {
	return nil
}

// Ensure implements StateManager.Ensure.
func (m *SnapManager) Ensure() error {
	return nil
}

// Stop implements StateManager.Stop.
func (m *SnapManager) Stop() error {
	return nil
}
