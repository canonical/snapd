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

	"github.com/snapcore/snapd/asserts"
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
