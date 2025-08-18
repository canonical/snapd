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

package clusterstate

import (
	"github.com/snapcore/snapd/overlord/state"
)

// ClusterManager is responsible for managing cluster assembly operations.
type ClusterManager struct {
	state  *state.State
	runner *state.TaskRunner
}

// Manager returns a new cluster manager.
func Manager(s *state.State, runner *state.TaskRunner) (*ClusterManager, error) {
	m := &ClusterManager{
		state:  s,
		runner: runner,
	}

	// register task handlers
	runner.AddHandler("create-cluster", m.doCreateCluster, nil)

	return m, nil
}

// Ensure implements StateManager.Ensure.
func (m *ClusterManager) Ensure() error {
	return nil
}
