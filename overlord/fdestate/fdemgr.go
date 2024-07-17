// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

// Package fdestate implements the manager and state responsible for
// managing full disk encryption keys.
package fdestate

import (
	"github.com/snapcore/snapd/overlord/state"
)

// FDEManager is responsible for managing managing full disk
// encryption keys.
type FDEManager struct {
	state *state.State
}

type fdeMgrKey struct{}

func Manager(st *state.State, runner *state.TaskRunner) *FDEManager {
	m := &FDEManager{
		state: st,
	}

	st.Lock()
	defer st.Unlock()
	st.Cache(fdeMgrKey{}, m)

	return m
}

func (m *FDEManager) Ensure() error {
	return nil
}

func fdeMgr(st *state.State) *FDEManager {
	c := st.Cached(fdeMgrKey{})
	if c == nil {
		panic("internal error: FDE manager is not yet associated with state")
	}
	return c.(*FDEManager)
}
