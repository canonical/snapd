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

// SkillManager is responsible for the maintenance of skills in system states.
// It maintains skills assignments, and also observes installed snaps to track
// the current set of available skills and skill slots.
type SkillManager struct {
	o *Overlord
}

// NewSkillManager returns a new SkillManager.
func NewSkillManager(o *Overlord) (*SkillManager, error) {
	return &SkillManager{o: o}, nil
}

// Grant records the intent of granting the skill in state s.
func (m *SkillManager) Grant(s *State, skillSnap, skillName, slotSnap, slotName string) error {
	return nil
}

// Revoke records the intent of revoking the skill in state s.
func (m *SkillManager) Revoke(s *State, skillSnap, skillName, slotSnap, slotName string) error {
	return nil
}

// Apply implements StateManager.Apply.
func (m *SkillManager) Apply(s *State) error {
	return nil
}

// Learn implements StateManager.Learn.
func (m *SkillManager) Learn(s *State) error {
	return nil
}

// Sanitize implements StateManager.Sanitize.
func (m *SkillManager) Sanitize(s *State) error {
	return nil
}

// Delta implements StateManager.Delta.
func (m *SkillManager) Delta(a, b *State) (Delta, error) {
	return nil, nil
}
