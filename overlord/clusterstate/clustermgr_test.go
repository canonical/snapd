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
	"time"

	check "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

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

	serial := newSerialAssertion(c, "serial-1")
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
				"state":    "clustered",
				"instance": "to-update",
				"channel":  "latest/stable",
			},
		},
	}})

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "to-remove", &snapstate.SnapState{
		Current: snap.R(1),
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(1)}, nil),
			},
		},
	})

	snapstate.Set(st, "to-update", &snapstate.SnapState{
		Current:         snap.R(2),
		TrackingChannel: "latest/edge",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(2)}, nil),
			},
		},
	})

	serial := newSerialAssertion(c, "serial-1")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(ctx context.Context, st *state.State, goal snapstate.UpdateGoal, filter func(*snap.Info, *snapstate.SnapState) bool, opts snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		task := st.NewTask("update", "update channel")
		return []string{"to-update"}, &snapstate.UpdateTaskSets{
			Refresh: []*state.TaskSet{state.NewTaskSet(task)},
		}, nil
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
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
	c.Assert(tss, check.HasLen, 3)
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

	serial := newSerialAssertion(c, "serial-9")
	restore := clusterstate.MockDevicestateSerial(func(*state.State) (*asserts.Serial, error) {
		return serial, nil
	})
	defer restore()

	tss, err := clusterstate.ApplyClusterState(st, cluster)
	c.Assert(err, check.IsNil)
	c.Check(tss, check.HasLen, 0)
}

func makeClusterAssertion(c *check.C, devices []map[string]any, subclusters []map[string]any) *asserts.Cluster {
	devs := make([]any, 0, len(devices))
	for _, dev := range devices {
		devs = append(devs, dev)
	}

	clusters := make([]any, len(subclusters))
	for i, sc := range subclusters {
		clusters[i] = sc
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

	asn, err := signing.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	return asn.(*asserts.Cluster)
}

func newSerialAssertion(c *check.C, serial string) *asserts.Serial {
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

	assertion, err := signing.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	return assertion.(*asserts.Serial)
}
