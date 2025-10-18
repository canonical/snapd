// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"errors"
	"fmt"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
)

var applyClusterSubclusterChangeKind = swfeats.RegisterChangeKind("apply-cluster-subcluster-%s")

type ClusterManager struct {
	state *state.State
}

// Manager returns a new ClusterManager.
func Manager(st *state.State) *ClusterManager {
	return &ClusterManager{
		state: st,
	}
}

// Ensure ensures that the device state matches the expectations defined by the
// cluster assertion.
func (m *ClusterManager) Ensure() error {
	enabled, err := clusteringEnabled(m.state)
	if err != nil {
		return err
	}

	if !enabled {
		return nil
	}

	m.state.Lock()
	defer m.state.Unlock()

	cluster, err := CurrentCluster(m.state)
	if err != nil {
		if errors.Is(err, ErrNoClusterAssertion) {
			return nil
		}
		return fmt.Errorf("cannot get cluster assertion: %w", err)
	}

	tasksets, err := applyClusterState(m.state, cluster)
	if err != nil {
		return err
	}

	if len(tasksets) == 0 {
		return nil
	}

	changesInProgress := make(map[string]bool)
	for _, chg := range m.state.Changes() {
		if !chg.Status().Ready() {
			changesInProgress[chg.Kind()] = true
		}
	}

	for name, tasks := range tasksets {
		kind := fmt.Sprintf(applyClusterSubclusterChangeKind, name)
		if changesInProgress[kind] {
			continue
		}

		chg := m.state.NewChange(kind, fmt.Sprintf("Apply subcluster %q state", name))
		chg.AddAll(tasks)
	}

	return nil
}

func clusteringEnabled(st *state.State) (bool, error) {
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)
	return features.Flag(tr, features.Clustering)
}
