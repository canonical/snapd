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
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
)

var applyClusterSubclusterChangeKind = swfeats.RegisterChangeKind("apply-cluster-subcluster-%s")

// ErrNoClusterAssertion indicates there is no current cluster assertion
// available.
var ErrNoClusterAssertion = errors.New("clusterstate: no cluster assertion")

// ClusterAssertionSource serves as the source of the current cluster assertion.
type ClusterAssertionSource interface {
	// CurrentCluster returns the serialized assertion bundle that describes the
	// current cluster. If there is no current cluster assertion, the
	// implementation must return [ErrNoClusterAssertion].
	CurrentCluster() ([]byte, error)
}

type fileClusterAssertionSource struct {
	path string
}

// NewFileClusterAssertionSource returns a [ClusterAssertionSource] backed by
// the assertion file at the provided path.
func NewFileClusterAssertionSource(path string) ClusterAssertionSource {
	return &fileClusterAssertionSource{path: path}
}

func (s *fileClusterAssertionSource) CurrentCluster() ([]byte, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoClusterAssertion
		}
		return nil, err
	}
	return data, nil
}

type nullClusterAssertionSource struct{}

// NewNullClusterAssertionSource returns a [ClusterAssertionSource] that always
// reports that no cluster assertion is available.
//
// TODO: remove this once it isn't needed
func NewNullClusterAssertionSource() ClusterAssertionSource {
	return nullClusterAssertionSource{}
}

func (nullClusterAssertionSource) CurrentCluster() ([]byte, error) {
	return nil, ErrNoClusterAssertion
}

type ClusterManager struct {
	state  *state.State
	source ClusterAssertionSource
}

// Manager returns a new ClusterManager.
func Manager(st *state.State, source ClusterAssertionSource) *ClusterManager {
	return &ClusterManager{
		state:  st,
		source: source,
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

	bundle, err := m.source.CurrentCluster()
	if err != nil {
		if errors.Is(err, ErrNoClusterAssertion) {
			return nil
		}
		return fmt.Errorf("cannot read cluster assertion bundle: %w", err)
	}

	batch := asserts.NewBatch(nil)
	refs, err := batch.AddStream(bytes.NewReader(bundle))
	if err != nil {
		return fmt.Errorf("cannot decode cluster assertion bundle: %w", err)
	}

	var cref *asserts.Ref
	for _, ref := range refs {
		if ref.Type == asserts.ClusterType {
			cref = ref
			break
		}
	}

	if cref == nil {
		return errors.New("assertion bundle missing cluster assertion")
	}

	m.state.Lock()
	defer m.state.Unlock()

	if err := assertstate.AddBatch(m.state, batch, nil); err != nil {
		return fmt.Errorf("cannot add cluster assertion bundle: %w", err)
	}

	a, err := cref.Resolve(func(assertType *asserts.AssertionType, headers map[string]string) (asserts.Assertion, error) {
		return assertstate.DB(m.state).Find(assertType, headers)
	})
	if err != nil {
		return fmt.Errorf("cannot resolve cluster assertion: %w", err)
	}

	cluster, ok := a.(*asserts.Cluster)
	if !ok {
		return fmt.Errorf("internal error: invalid cluster assertion in bundle")
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
