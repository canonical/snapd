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

// Package overlord implements the overall control of a snappy system.
package overlord

import (
	"fmt"
	"os"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"

	"github.com/ubuntu-core/snappy/overlord/assertstate"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/snapstate"
	"github.com/ubuntu-core/snappy/overlord/state"
)

// Overlord is the central manager of a snappy system, keeping
// track of all available state managers and related helpers.
type Overlord struct {
	stateEng *StateEngine
	// managers
	snapMgr   *snapstate.SnapManager
	assertMgr *assertstate.AssertManager
	ifaceMgr  *ifacestate.InterfaceManager
}

// New creates a new Overlord with all its state managers.
func New() (*Overlord, error) {
	o := &Overlord{}

	backend := state.NewFileBackend(dirs.SnapStateFile)
	s, err := loadState(backend)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)

	snapMgr, err := snapstate.Manager()
	if err != nil {
		return nil, err
	}
	o.snapMgr = snapMgr
	o.stateEng.AddManager(o.snapMgr)

	assertMgr, err := assertstate.Manager()
	if err != nil {
		return nil, err
	}
	o.assertMgr = assertMgr
	o.stateEng.AddManager(o.assertMgr)

	ifaceMgr, err := ifacestate.Manager()
	if err != nil {
		return nil, err
	}
	o.ifaceMgr = ifaceMgr
	o.stateEng.AddManager(o.ifaceMgr)

	return o, nil
}

func loadState(backend state.Backend) (*state.State, error) {
	if !osutil.FileExists(dirs.SnapStateFile) {
		return state.New(backend), nil
	}

	r, err := os.Open(dirs.SnapStateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the state file: %s", err)
	}
	defer r.Close()

	return state.ReadState(backend, r)
}

// StateEngine returns the state engine used by the overlord.
func (o *Overlord) StateEngine() *StateEngine {
	return o.stateEng
}

// SnapManager returns the snap manager responsible for snaps under
// the overlord.
func (o *Overlord) SnapManager() *snapstate.SnapManager {
	return o.snapMgr
}

// AssertManager returns the assertion manager enforcing assertions
// under the overlord.
func (o *Overlord) AssertManager() *assertstate.AssertManager {
	return o.assertMgr
}

// InterfaceManager returns the interface manager maintaining
// interface connections under the overlord.
func (o *Overlord) InterfaceManager() *ifacestate.InterfaceManager {
	return o.ifaceMgr
}
