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

// AssertManager is responsible for the enforcement of assertions
// in system states. It manipulates observed states to ensure nothing
// in them violates existing assertions, or misses required ones.
type AssertManager struct {
	o *Overlord
}

// NewAssertManager returns a new assertion manager.
func NewAssertManager(o *Overlord) (*AssertManager, error) {
	return &AssertManager{o: o}, nil
}

// Apply implements StateManager.Apply.
func (m *AssertManager) Apply(s *State) error {
	return nil
}

// Learn implements StateManager.Learn.
func (m *AssertManager) Learn(s *State) error {
	return nil
}

// Sanitize implements StateManager.Sanitize.
func (m *AssertManager) Sanitize(s *State) error {
	return nil
}

// Delta implements StateManager.Delta.
func (m *AssertManager) Delta(a, b *State) (Delta, error) {
	return nil, nil
}
