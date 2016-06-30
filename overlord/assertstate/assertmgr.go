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

// Package assertstate implements the manager and state aspects responsible
// for the enforcement of assertions in the system and manages the system-wide
// assertion database.
package assertstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/overlord/state"
)

// AssertManager is responsible for the enforcement of assertions in
// system states. It manipulates the observed system state to ensure
// nothing in it violates existing assertions, or misses required
// ones.
type AssertManager struct {
	db *asserts.Database
}

// Manager returns a new assertion manager.
func Manager(s *state.State) (*AssertManager, error) {
	db, err := sysdb.Open()
	if err != nil {
		return nil, err
	}
	return &AssertManager{db: db}, nil
}

// Ensure implements StateManager.Ensure.
func (m *AssertManager) Ensure() error {
	return nil
}

// Stop implements StateManager.Stop.
func (m *AssertManager) Stop() {
}

// Wait implements StateManager.Wait.
func (m *AssertManager) Wait() {
}

// DB returns the assertion database under the manager.
func (m *AssertManager) DB() *asserts.Database {
	return m.db
}
