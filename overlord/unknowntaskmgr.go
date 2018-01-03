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

import (
	"github.com/snapcore/snapd/overlord/state"
)

type UnknownTaskManager struct {
	state  *state.State
	runner *state.TaskRunner
}

func NewUnknownTaskManager(s *state.State, knownTaskKinds []string) *UnknownTaskManager {
	runner := state.NewUnknownTaskRunner(s, knownTaskKinds)
	mgr := &UnknownTaskManager{
		state:  s,
		runner: runner,
	}
	return mgr
}

// Ensure implements StateManager.Ensure.
func (m *UnknownTaskManager) Ensure() error {
	m.runner.Ensure()
	return nil
}

// Wait implements StateManager.Wait.
func (m *UnknownTaskManager) Wait() {
	m.runner.Wait()
}

// Stop implements StateManager.Stop.
func (m *UnknownTaskManager) Stop() {
	m.runner.Stop()
}

func (mgr *UnknownTaskManager) KnownTaskKinds() []string {
	return []string{}
}
