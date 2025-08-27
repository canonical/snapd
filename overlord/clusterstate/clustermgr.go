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

	// check if there's already a cluster configuration change in progress
	for _, chg := range m.state.Changes() {
		if (chg.Kind() == "install-cluster-snaps" || chg.Kind() == "apply-cluster-config") && !chg.IsReady() {
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

	installs, removals, err := findSnapsForDevice(m.state, cluster, serial.Serial())
	if err != nil {
		return err
	}

	if len(installs) == 0 && len(removals) == 0 {
		return nil
	}

	// get removal tasksets first
	removeTasksets, err := m.remove(removals)
	if err != nil {
		return err
	}

	// get installation tasksets
	installTasksets, err := m.install(installs)
	if err != nil {
		return err
	}

	// create a single change for all operations
	chg := m.state.NewChange("apply-cluster-config", "Apply cluster configuration (install and remove snaps)")

	// add removal tasks first, then installation tasks
	for _, ts := range removeTasksets {
		chg.AddAll(ts)
	}
	for _, ts := range installTasksets {
		chg.AddAll(ts)
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

func findSnapsForDevice(st *state.State, cluster *asserts.Cluster, serial string) (installs []snapstate.StoreSnap, removals []string, err error) {
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
		return nil, nil, nil
	}

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
			var snapst snapstate.SnapState
			err := snapstate.Get(st, sn.Instance, &snapst)
			if err != nil && !errors.Is(err, state.ErrNoState) {
				return nil, nil, err
			}

			switch sn.State {
			case "clustered":
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

				installs = append(installs, ss)
			case "removed":
				if !snapst.IsInstalled() {
					continue
				}
				removals = append(removals, sn.Instance)
			}
		}
	}

	return installs, removals, nil
}

func (m *ClusterManager) install(snaps []snapstate.StoreSnap) ([]*state.TaskSet, error) {
	if len(snaps) == 0 {
		return nil, nil
	}

	goal := snapstate.StoreInstallGoal(snaps...)
	_, tasksets, err := snapstate.InstallWithGoal(context.Background(), m.state, goal, snapstate.Options{})
	if err != nil {
		return nil, fmt.Errorf("cannot create snap installation tasks: %w", err)
	}

	return tasksets, nil
}

func (m *ClusterManager) remove(names []string) ([]*state.TaskSet, error) {
	if len(names) == 0 {
		return nil, nil
	}

	_, tasksets, err := snapstate.RemoveMany(m.state, names, &snapstate.RemoveFlags{})
	if err != nil {
		return nil, fmt.Errorf("cannot create snap removal tasks: %w", err)
	}

	return tasksets, nil
}
