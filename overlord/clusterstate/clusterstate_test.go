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

package clusterstate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type managerSuite struct{}

var _ = check.Suite(&managerSuite{})

func (s *managerSuite) TestApplyClusterStateNoActions(c *check.C) {
	cluster := makeClusterAssertion(c, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{{
		"name":    "default",
		"devices": []any{"1"},
		"snaps":   []any{},
	}})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, "serial-1")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	restore = clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		c.Fatal("unexpected install")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
		c.Fatal("unexpected remove")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(context.Context, *state.State, snapstate.UpdateGoal, func(*snap.Info, *snapstate.SnapState) bool, snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Fatal("unexpected update")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	tss, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.IsNil)
	c.Assert(tss, check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateDeviceNotInAnySubcluster(c *check.C) {
	cluster := makeClusterAssertion(c, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
		{
			"id":        "2",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-2",
			"addresses": []any{"192.168.0.11"},
		},
	}, []map[string]any{{
		"name": "default",
		"devices": []any{
			"2",
		},
		"snaps": []any{
			map[string]any{
				"state":    "clustered",
				"instance": "ignored-snap",
				"channel":  "latest/stable",
			},
		},
	}})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, "serial-1")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	restore = clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		c.Fatal("unexpected install")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
		c.Fatal("unexpected remove")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(context.Context, *state.State, snapstate.UpdateGoal, func(*snap.Info, *snapstate.SnapState) bool, snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Fatal("unexpected update")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	tss, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.IsNil)
	c.Assert(tss, check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateInstallRemoveAndUpdate(c *check.C) {
	cluster := makeClusterAssertion(c, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{{
		"name":    "default",
		"devices": []any{"1"},
		"snaps": []any{
			map[string]any{
				"state":    "clustered",
				"instance": "to-install",
				"channel":  "latest/stable",
			},
			map[string]any{
				"state":    "removed",
				"instance": "to-remove",
				"channel":  "latest/stable",
			},
			map[string]any{
				"state":    "removed",
				"instance": "not-installed-removed",
				"channel":  "latest/stable",
			},
			map[string]any{
				"state":    "clustered",
				"instance": "to-refresh",
				"channel":  "latest/stable",
			},
			map[string]any{
				"state":    "clustered",
				"instance": "already-installed",
				"channel":  "latest/stable",
			},
		},
	}})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "to-remove", &snapstate.SnapState{
		Current:         snap.R(1),
		TrackingChannel: "latest/stable",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(1)}, nil),
			},
		},
	})

	snapstate.Set(st, "to-refresh", &snapstate.SnapState{
		Current:         snap.R(2),
		TrackingChannel: "latest/edge",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(2)}, nil),
			},
		},
	})

	snapstate.Set(st, "already-installed", &snapstate.SnapState{
		Current:         snap.R(5),
		TrackingChannel: "latest/stable",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(5)}, nil),
			},
		},
	})

	serial := makeSerialAssertion(c, "serial-1")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	var updates []snapstate.StoreUpdate
	var installs []snapstate.StoreSnap
	var removals []string

	restore = clusterstate.MockStoreUpdateGoal(func(upds ...snapstate.StoreUpdate) snapstate.UpdateGoal {
		updates = append(updates, upds...)
		return snapstate.StoreUpdateGoal(upds...)
	})
	defer restore()

	restore = clusterstate.MockStoreInstallGoal(func(snaps ...snapstate.StoreSnap) snapstate.InstallGoal {
		installs = append(installs, snaps...)
		return snapstate.StoreInstallGoal(snaps...)
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(ctx context.Context, st *state.State, goal snapstate.UpdateGoal, filter func(*snap.Info, *snapstate.SnapState) bool, opts snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		task := st.NewTask("update", "update channel")
		return []string{"to-refresh"}, &snapstate.UpdateTaskSets{
			Refresh: []*state.TaskSet{state.NewTaskSet(task)},
		}, nil
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
		removals = append(removals, names...)
		task := st.NewTask("remove", "remove snaps")
		return names, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	restore = clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		task := st.NewTask("install", "install snaps")
		return nil, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	tss, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.IsNil)
	c.Assert(tss, check.HasLen, 1)
	c.Assert(removals, check.DeepEquals, []string{"to-remove"})
	c.Assert(installs, check.DeepEquals, []snapstate.StoreSnap{
		{
			InstanceName:  "to-install",
			SkipIfPresent: true,
			RevOpts: snapstate.RevisionOptions{
				Channel: "latest/stable",
			},
		},
	})

	// make sure only the snap that needs an update got it
	c.Assert(updates, check.DeepEquals, []snapstate.StoreUpdate{
		{
			InstanceName: "to-refresh",
			RevOpts: snapstate.RevisionOptions{
				Channel: "latest/stable",
			},
		},
	})
	tasks := tss[0].Tasks()
	c.Assert(tasks, check.HasLen, 3)
	c.Assert([]string{tasks[0].Kind(), tasks[1].Kind(), tasks[2].Kind()}, check.DeepEquals, []string{"remove", "update", "install"})
}

func (s *managerSuite) TestApplyClusterStateMultipleSubclusters(c *check.C) {
	cluster := makeClusterAssertion(c, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{
		{
			"name":    "one",
			"devices": []any{"1"},
			"snaps": []any{
				map[string]any{
					"state":    "clustered",
					"instance": "snap-one-install",
					"channel":  "latest/stable",
				},
			},
		},
		{
			"name":    "two",
			"devices": []any{"1"},
			"snaps": []any{
				map[string]any{
					"state":    "removed",
					"instance": "snap-two-remove",
					"channel":  "latest/stable",
				},
			},
		},
	})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "snap-two-remove", &snapstate.SnapState{
		Current:         snap.R(1),
		TrackingChannel: "latest/stable",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(1)}, nil),
			},
		},
	})

	serial := makeSerialAssertion(c, "serial-1")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	restore = clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		task := st.NewTask("install", "install snap one")
		return nil, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
		task := st.NewTask("remove", "remove snap two")
		return names, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(context.Context, *state.State, snapstate.UpdateGoal, func(*snap.Info, *snapstate.SnapState) bool, snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Fatal("unexpected update")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	tss, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.IsNil)
	c.Assert(tss, check.HasLen, 2)

	first := tss[0].Tasks()
	c.Assert(first, check.HasLen, 1)
	c.Check(first[0].Kind(), check.Equals, "install")

	second := tss[1].Tasks()
	c.Assert(second, check.HasLen, 1)
	c.Check(second[0].Kind(), check.Equals, "remove")
}

func (s *managerSuite) TestApplyClusterStateDeviceMissing(c *check.C) {
	cluster := makeClusterAssertion(c, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, "serial-9")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	_, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.ErrorMatches, `device with serial "serial-9" not found in cluster assertion`)
}

func makeClusterAssertion(c *check.C, devices []map[string]any, subclusters []map[string]any) *asserts.Cluster {
	devs := make([]any, 0, len(devices))
	for _, dev := range devices {
		devs = append(devs, dev)
	}

	clusters := make([]any, 0, len(subclusters))
	for _, sc := range subclusters {
		clusters = append(clusters, sc)
	}

	key, _ := assertstest.GenerateKey(752)
	signing := assertstest.NewSigningDB("authority-id", key)

	headers := map[string]any{
		"type":        "cluster",
		"cluster-id":  "cluster-id",
		"sequence":    "1",
		"devices":     devs,
		"subclusters": clusters,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	a, err := signing.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	return a.(*asserts.Cluster)
}

func makeSerialAssertion(c *check.C, serial string) *asserts.Serial {
	pk, _ := assertstest.GenerateKey(752)
	signing := assertstest.NewSigningDB("canonical", pk)

	deviceKey, _ := assertstest.GenerateKey(752)
	encodedKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)

	headers := map[string]any{
		"authority-id":        "canonical",
		"brand-id":            "canonical",
		"model":               "ubuntu-core-24-amd64",
		"serial":              serial,
		"device-key":          string(encodedKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}

	a, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	return a.(*asserts.Serial)
}
