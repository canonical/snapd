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

// SnapManager is responsible for the installation and removal of snaps.
type SnapManager struct {
	o *Overlord
}

// NewSnapManager returns a new snap manager.
func NewSnapManager(o *Overlord) (*SnapManager, error) {
	return &SnapManager{o: o}, nil
}

// Install records the intent of installing snap in state s.
func (m *SnapManager) Install(s *State, snap string) error {
	return nil
}

// Remove records the intent of removing snap in state s.
func (m *SnapManager) Remove(s *State, snap string) error {
	return nil
}

// Apply implements StateManager.Apply.
func (m *SnapManager) Apply(s *State) error {
	return nil
}

// Learn implements StateManager.Learn.
func (m *SnapManager) Learn(s *State) error {
	return nil
}

// Sanitize implements StateManager.Sanitize.
func (m *SnapManager) Sanitize(s *State) error {
	return nil
}

// Delta implements StateManager.Delta.
func (m *SnapManager) Delta(a, b *State) (Delta, error) {
	return nil, nil
}
