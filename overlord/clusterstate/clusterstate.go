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
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	installWithGoal  = snapstate.InstallWithGoal
	removeMany       = snapstate.RemoveMany
	updateWithGoal   = snapstate.UpdateWithGoal
	storeUpdateGoal  = snapstate.StoreUpdateGoal
	storeInstallGoal = snapstate.StoreInstallGoal
)

// ErrNoClusterAssertion indicates there is no current cluster assertion available.
var ErrNoClusterAssertion = errors.New("clusterstate: no cluster assertion")

// clusterState contains the state of clustering on this device.
//
// TODO: This is pretty verbose right now, but we might want to put everything
// under one namespace in the state?
type clusterState struct {
	// Current contains the information needed to find the current cluster
	// assertion. Maybe we should consider some sort of sequence container, like
	// we use in snapstate?
	Current clusterAssertionState `json:"current"`
}

// clusterAssertionState contains the information needed to find a specific
// cluster assertion.
type clusterAssertionState struct {
	// ClusterID is the globally unique identifier for this cluster assertion.
	ClusterID string `json:"cluster-id"`
	// Sequence is the sequence point at which the cluster assertion with the
	// ClusterID.
	Sequence int `json:"sequence"`
	// AuthorityID is the ID of the account that is associated with this
	// cluster. When updating the cluster to a new sequence number, the new
	// cluster assertion must be signed by an account-key associated with the
	// same account.
	AuthorityID string `json:"authority-id"`
}

// InitializeNewCluster installs a new cluster assertion bundle, replacing any
// existing state. Callers must hold the state lock.
func InitializeNewCluster(st *state.State, bundle io.Reader) error {
	batch, cluster, err := decodeClusterBundle(bundle)
	if err != nil {
		return err
	}

	var existing clusterState
	if err := st.Get("cluster", &existing); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// TODO: we will need some way to handle updating a cluster to an assertion
	// with a new ID, but it probably won't use this function here
	if existing.Current.ClusterID != "" {
		return fmt.Errorf(
			"cannot initialize cluster %q while tracking an existing cluster assertion %q",
			cluster.ClusterID(), existing.Current.ClusterID,
		)
	}

	if err := assertstate.AddBatch(st, batch, nil); err != nil {
		return fmt.Errorf("cannot add cluster assertion bundle: %w", err)
	}

	st.Set("cluster", clusterState{
		Current: clusterAssertionState{
			ClusterID:   cluster.ClusterID(),
			Sequence:    cluster.Sequence(),
			AuthorityID: cluster.AuthorityID(),
		},
	})

	// trigger an ensure pass so that the new assertion is picked up and applied
	st.EnsureBefore(0)

	return nil
}

// UpdateCluster installs an incremental cluster assertion update. Callers must
// hold the state lock.
func UpdateCluster(st *state.State, bundle io.Reader) error {
	batch, cluster, err := decodeClusterBundle(bundle)
	if err != nil {
		return err
	}

	var cs clusterState
	if err := st.Get("cluster", &cs); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return ErrNoClusterAssertion
		}
		return err
	}

	if cluster.AuthorityID() != cs.Current.AuthorityID {
		return fmt.Errorf(
			"cluster assertion authority %q does not match expected authority %q",
			cluster.AuthorityID(),
			cs.Current.AuthorityID,
		)
	}

	if cluster.ClusterID() != cs.Current.ClusterID {
		return fmt.Errorf(
			"cluster assertion id %q does not match expected id %q",
			cluster.ClusterID(),
			cs.Current.ClusterID,
		)
	}

	if cluster.Sequence() <= cs.Current.Sequence {
		return fmt.Errorf(
			"cluster assertion sequence %d must be greater than current sequence %d",
			cluster.Sequence(),
			cs.Current.Sequence,
		)
	}

	if err := assertstate.AddBatch(st, batch, nil); err != nil {
		return fmt.Errorf("cannot add cluster assertion bundle: %w", err)
	}

	st.Set("cluster", clusterState{
		Current: clusterAssertionState{
			ClusterID:   cluster.ClusterID(),
			Sequence:    cluster.Sequence(),
			AuthorityID: cluster.AuthorityID(),
		},
	})

	// trigger an ensure pass so that the new assertion is picked up and applied
	st.EnsureBefore(0)

	return nil
}

// CurrentCluster returns the currently tracked cluster assertion. Callers must
// hold the state lock.
func CurrentCluster(st *state.State) (*asserts.Cluster, error) {
	var cs clusterState
	if err := st.Get("cluster", &cs); err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, ErrNoClusterAssertion
		}
		return nil, err
	}

	headers := map[string]string{
		"cluster-id": cs.Current.ClusterID,
		"sequence":   strconv.Itoa(cs.Current.Sequence),
	}
	a, err := assertstate.DB(st).Find(asserts.ClusterType, headers)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve cluster assertion: %w", err)
	}

	cluster, ok := a.(*asserts.Cluster)
	if !ok {
		return nil, errors.New("internal error: stored assertion is not a cluster assertion")
	}
	return cluster, nil
}

func decodeClusterBundle(bundle io.Reader) (*asserts.Batch, *asserts.Cluster, error) {
	var cluster *asserts.Cluster
	batch := asserts.NewBatch(nil)

	dec := asserts.NewDecoder(bundle)
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("cannot decode cluster assertion bundle: %w", err)
		}

		if err := batch.Add(a); err != nil {
			return nil, nil, err
		}

		c, ok := a.(*asserts.Cluster)
		if !ok {
			continue
		}
		if cluster != nil {
			return nil, nil, errors.New("cluster assertion bundle contains multiple cluster assertions")
		}
		cluster = c
	}

	if cluster == nil {
		return nil, nil, errors.New("assertion bundle missing cluster assertion")
	}

	return batch, cluster, nil
}

// applyClusterState creates the tasks needed to apply the state described by
// the cluster assertion on this device.
func applyClusterState(st *state.State, cluster *asserts.Cluster) (map[string]*state.TaskSet, error) {
	serial, err := devicestate.Serial(st)
	if err != nil {
		return nil, err
	}

	deviceID, ok := clusterDeviceIDBySerial(cluster, serial.Serial())
	if !ok {
		return nil, fmt.Errorf("device with serial %q not found in cluster assertion", serial.Serial())
	}

	// mapping of subcluster name to tasks to match desired subcluster state
	tasksets := make(map[string]*state.TaskSet)
	for _, subcluster := range cluster.Subclusters() {
		if !deviceInSubcluster(subcluster, deviceID) {
			continue
		}

		ts, err := applySubcluster(st, subcluster)
		if err != nil {
			return nil, err
		}

		if len(ts.Tasks()) == 0 {
			continue
		}

		tasksets[subcluster.Name] = ts
	}

	return tasksets, nil
}

func applySubcluster(st *state.State, subcluster asserts.Subcluster) (*state.TaskSet, error) {
	installs, removals, updates, err := snapsForSubcluster(st, subcluster)
	if err != nil {
		return nil, err
	}

	combined := state.NewTaskSet()
	if len(installs) == 0 && len(removals) == 0 && len(updates) == 0 {
		return combined, nil
	}

	// TaskSet edges (BeginEdge, EndEdge, etc.) are snap-specific and conflict
	// when flattening multiple snap task sets, so we only aggregate the tasks.
	appendTaskSets := func(src []*state.TaskSet) {
		for _, ts := range src {
			combined.AddAll(ts)
		}
	}

	if len(removals) > 0 {
		// TODO: handle conflict errors from remove
		_, removeTS, err := removeMany(st, removals, &snapstate.RemoveFlags{})
		if err != nil {
			return nil, fmt.Errorf("cannot create snap removal tasks: %w", err)
		}

		appendTaskSets(removeTS)
	}

	if len(updates) > 0 {
		// TODO: handle busy snap errors here (potentially just do a switch in
		// that case?)
		// TODO: handle conflict errors from refresh
		goal := storeUpdateGoal(updates...)
		_, updateTS, err := updateWithGoal(context.Background(), st, goal, nil, snapstate.Options{})
		if err != nil {
			return nil, fmt.Errorf("cannot create snap update tasks: %w", err)
		}

		appendTaskSets(updateTS.Refresh)
	}

	if len(installs) > 0 {
		goal := storeInstallGoal(installs...)
		_, installTS, err := installWithGoal(context.Background(), st, goal, snapstate.Options{})
		if err != nil {
			return nil, fmt.Errorf("cannot create snap installation tasks: %w", err)
		}

		appendTaskSets(installTS)
	}

	return combined, nil
}

func snapsForSubcluster(
	st *state.State, subcluster asserts.Subcluster,
) (
	installs []snapstate.StoreSnap,
	removals []string,
	updates []snapstate.StoreUpdate,
	err error,
) {
	for _, sn := range subcluster.Snaps {
		var snapst snapstate.SnapState
		if err := snapstate.Get(st, sn.Instance, &snapst); err != nil && !errors.Is(err, state.ErrNoState) {
			return nil, nil, nil, err
		}

		// TODO: handle [asserts.ClusterSnapStateEvacuated]
		switch sn.State {
		case asserts.ClusterSnapStateClustered:
			if snapst.IsInstalled() {
				if sn.Channel != "" && snapst.TrackingChannel != sn.Channel {
					updates = append(updates, snapstate.StoreUpdate{
						InstanceName: sn.Instance,
						RevOpts: snapstate.RevisionOptions{
							Channel: sn.Channel,
						},
					})
				}
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
		case asserts.ClusterSnapStateRemoved:
			if !snapst.IsInstalled() {
				continue
			}
			removals = append(removals, sn.Instance)
		}
	}

	return installs, removals, updates, nil
}

func deviceInSubcluster(subcluster asserts.Subcluster, deviceID int) bool {
	for _, id := range subcluster.Devices {
		if id == deviceID {
			return true
		}
	}
	return false
}

func clusterDeviceIDBySerial(cluster *asserts.Cluster, serial string) (int, bool) {
	for _, dev := range cluster.Devices() {
		if dev.Serial == serial {
			return dev.ID, true
		}
	}
	return 0, false
}
