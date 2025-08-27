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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
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
	runner.AddHandler("assemble-cluster", m.doAssembleCluster, nil)

	return m, nil
}

// Ensure implements StateManager.Ensure.
func (m *ClusterManager) Ensure() error {
	m.state.Lock()
	defer m.state.Unlock()

	for _, chg := range m.state.Changes() {
		if chg.Kind() == "install-cluster-snaps" && !chg.IsReady() {
			return nil
		}
	}

	cluster, err := readClusterAssertion("/tmp/snapd-clusterdb/cluster.assert")
	if err != nil {
		// nothing to do if the file isn't there
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	serial, err := devicestate.DeviceMgr(m.state).Serial()
	if err != nil {
		return nil
	}

	installs, err := findSnapsForDevice(m.state, cluster, serial.Serial())
	if err != nil {
		return err
	}

	if len(installs) == 0 {
		return nil
	}

	if err := m.install(installs); err != nil {
		return err
	}

	return nil
}

func readClusterAssertion(path string) (*asserts.Cluster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read cluster assertion: %w", err)
	}

	var cluster *asserts.Cluster
	dec := asserts.NewDecoder(bytes.NewReader(data))
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("cannot decode assertion: %w", err)
		}

		if c, ok := a.(*asserts.Cluster); ok {
			cluster = c
		}
	}

	if cluster == nil {
		return nil, errors.New("no cluster assertion found in file")
	}

	return cluster, nil
}

func findSnapsForDevice(st *state.State, cluster *asserts.Cluster, serial string) ([]snapstate.StoreSnap, error) {
	var deviceID int
	found := false
	for _, dev := range cluster.Devices() {
		if dev.Serial == serial {
			deviceID = dev.ID
			found = true
			break
		}
	}

	if !found {
		return nil, nil
	}

	var installs []snapstate.StoreSnap
	for _, subcluster := range cluster.Subclusters() {
		// check if this device is in the subcluster
		present := false
		for _, id := range subcluster.Devices {
			if id == deviceID {
				present = true
				break
			}
		}

		if !present {
			continue
		}

		for _, sn := range subcluster.Snaps {
			// only install snaps in "clustered" state
			if sn.State != "clustered" {
				continue
			}

			var snapst snapstate.SnapState
			if err := snapstate.Get(st, sn.Instance, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
				return nil, err
			}

			// TODO: more intelligent check
			if snapst.IsInstalled() {
				continue
			}

			ss := snapstate.StoreSnap{
				InstanceName:  sn.Instance,
				SkipIfPresent: true,
				RevOpts: snapstate.RevisionOptions{
					Channel: sn.Channel,
				},
			}

			// set channel if specified
			if sn.Channel != "" {
				ss.RevOpts.Channel = sn.Channel
			}

			installs = append(installs, ss)
		}
	}

	return installs, nil
}

func (m *ClusterManager) install(snaps []snapstate.StoreSnap) error {
	goal := snapstate.StoreInstallGoal(snaps...)
	_, tasksets, err := snapstate.InstallWithGoal(context.Background(), m.state, goal, snapstate.Options{})
	if err != nil {
		return fmt.Errorf("cannot create snap installation tasks: %w", err)
	}

	chg := m.state.NewChange("install-cluster-snaps", "Install snaps required by cluster configuration")
	for _, ts := range tasksets {
		chg.AddAll(ts)
	}

	return nil
}
