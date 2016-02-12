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

// Package overlord implements the policies for state transitions for the operation of a snappy system.
package overlord

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	// managers
	snapMgr   *SnapManager
	assertMgr *AssertManager
	skillMgr  *SkillManager
}

// New creates a new Overlord with all its state managers.
func New() (*Overlord, error) {
	stateEng := NewStateEngine()

	o := &Overlord{}
	snapMgr, err := NewSnapManager(o)
	if err != nil {
		return nil, err
	}
	o.snapMgr = snapMgr
	stateEng.AddManager(o.snapMgr)

	assertMgr, err := NewAssertManager(o)
	if err != nil {
		return nil, err
	}
	o.assertMgr = assertMgr
	stateEng.AddManager(o.assertMgr)

	skillMgr, err := NewSkillManager(o)
	if err != nil {
		return nil, err
	}
	o.skillMgr = skillMgr
	stateEng.AddManager(o.skillMgr)

	// XXX: setup the StateJournal

	return o, nil
}

// StateJournal returns the StateJournal used by the overlord.
func (o *Overlord) StateJournal() *StateJournal {
	return nil
}

// SnapManager returns the snap manager responsible for snaps under
// the overlord.
func (o *Overlord) SnapManager() *SnapManager {
	return o.snapMgr
}

// AssertManager returns the assertion manager enforcing assertions
// under the overlord.
func (o *Overlord) AssertManager() *AssertManager {
	return o.assertMgr
}

// SkillManager returns the skill manager mantaining skill assignments
// under the overlord.
func (o *Overlord) SkillManager() *SkillManager {
	return o.skillMgr
}
