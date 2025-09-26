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
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// ClusterManager is responsible for managing cluster assembly operations.
type ClusterManager struct {
	state  *state.State
	runner *state.TaskRunner

	receiver AssertionReceiver
}

type AssertionReceiver struct {
	server  http.Server
	started int32

	lock   sync.Mutex
	cancel func()
}

func (ar *AssertionReceiver) set(cancel func()) {
	ar.lock.Lock()
	defer ar.lock.Unlock()
	ar.cancel = cancel
}

func (ar *AssertionReceiver) start(st *state.State, host string) error {
	if !atomic.CompareAndSwapInt32(&ar.started, 0, 1) {
		return nil
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, 7070))
	if err != nil {
		return err
	}

	recv := ar.receiver(st)
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recv.ServeHTTP(w, r)
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}

	go server.Serve(ln)

	return nil
}

func (ar *AssertionReceiver) receiver(st *state.State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		batch := asserts.NewBatch(nil)
		refs, err := batch.AddStream(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// validate that we only have cluster and account-key assertions
		var cluster *asserts.Ref
		for _, r := range refs {
			if r.Type.Name == asserts.ClusterType.Name {
				cluster = r
			}
		}

		if cluster == nil {
			http.Error(w, "missing cluster assertion in bundle!", 400)
			return
		}

		st.Lock()
		defer st.Unlock()

		db := assertstate.DB(st)
		if err := assertstate.AddBatch(st, batch, &asserts.CommitOptions{
			Precheck: true,
		}); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// save our assertion to our "distributed database"
		if err := os.MkdirAll("/tmp/snapd-clusterdb", 0755); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		f, err := os.Create("/tmp/snapd-clusterdb/cluster.assert")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer f.Close()

		buf := bytes.NewBuffer(nil)
		enc := asserts.NewEncoder(buf)
		for _, r := range refs {
			a, err := r.Resolve(db.Find)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}

			if err := enc.Encode(a); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
		}

		if err := osutil.AtomicWriteFile("/tmp/snapd-clusterdb/cluster.assert", buf.Bytes(), 0644, 0); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		st.EnsureBefore(0)

		ar.lock.Lock()
		defer ar.lock.Unlock()
		ar.cancel()
	})
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
		if chg.Kind() == "apply-cluster-state" && !chg.IsReady() {
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

	tasksets, err := ApplyClusterState(m.state, cluster)
	if err != nil {
		return err
	}

	if len(tasksets) == 0 {
		return nil
	}

	// create a single change for all operations
	chg := m.state.NewChange("apply-cluster-state", "Apply cluster configuration (install and remove snaps)")

	for _, ts := range tasksets {
		chg.AddAll(ts)
	}

	return nil
}

var (
	installWithGoal   = snapstate.InstallWithGoal
	removeMany        = snapstate.RemoveMany
	devicestateSerial = devicestate.Serial
)

// ApplyClusterState calculates the snap install/remove task sets needed to align
// the given state with the cluster definition for the local device.
func ApplyClusterState(st *state.State, cluster *asserts.Cluster) ([]*state.TaskSet, error) {
	serial, err := devicestateSerial(st)
	if err != nil {
		return nil, nil
	}

	deviceID, ok := clusterDeviceIDBySerial(cluster, serial.Serial())
	if !ok {
		return nil, nil
	}

	installs, removals, err := snapsForClusterDevice(st, cluster, deviceID)
	if err != nil {
		return nil, err
	}

	if len(installs) == 0 && len(removals) == 0 {
		return nil, nil
	}

	var tasksets []*state.TaskSet

	if len(removals) > 0 {
		_, removeTS, err := removeMany(st, removals, &snapstate.RemoveFlags{})
		if err != nil {
			return nil, fmt.Errorf("cannot create snap removal tasks: %w", err)
		}
		tasksets = append(tasksets, removeTS...)
	}

	if len(installs) > 0 {
		goal := snapstate.StoreInstallGoal(installs...)
		_, installTS, err := installWithGoal(context.Background(), st, goal, snapstate.Options{})
		if err != nil {
			return nil, fmt.Errorf("cannot create snap installation tasks: %w", err)
		}
		tasksets = append(tasksets, installTS...)
	}

	return tasksets, nil
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

func snapsForClusterDevice(st *state.State, cluster *asserts.Cluster, deviceID int) (installs []snapstate.StoreSnap, removals []string, err error) {
	if !devicePresent(cluster, deviceID) {
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

func clusterDeviceIDBySerial(cluster *asserts.Cluster, serial string) (int, bool) {
	for _, dev := range cluster.Devices() {
		if dev.Serial == serial {
			return dev.ID, true
		}
	}
	return 0, false
}

func devicePresent(cluster *asserts.Cluster, deviceID int) bool {
	for _, dev := range cluster.Devices() {
		if dev.ID == deviceID {
			return true
		}
	}
	return false
}
