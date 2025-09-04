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
