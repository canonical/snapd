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
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/clusterstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/check.v1"
)

func Test(t *testing.T) { check.TestingT(t) }

type clusterStateSuite struct{}

var _ = check.Suite(&clusterStateSuite{})

func (s *clusterStateSuite) TestUpdateWithoutInitializing(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	bundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, nil, nil)

	st.Lock()
	defer st.Unlock()
	err := clusterstate.UpdateCluster(st, bytes.NewReader(bundle))
	c.Assert(err, testutil.ErrorIs, clusterstate.ErrNoClusterAssertion)
}

func (s *clusterStateSuite) TestGetWithoutInitializing(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()
	_, err := clusterstate.CurrentCluster(st)
	c.Assert(err, testutil.ErrorIs, clusterstate.ErrNoClusterAssertion)
}

func (s *clusterStateSuite) TestUpdateSequenceNotGreater(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	initialBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 2, devices, subclusters)

	st.Lock()
	defer st.Unlock()

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initialBundle))
	c.Assert(err, check.IsNil)

	updateBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 2, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.ErrorMatches, "cluster assertion sequence 2 must be greater than current sequence 2")

	updateBundle, _ = makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.ErrorMatches, "cluster assertion sequence 1 must be greater than current sequence 2")
}

func (s *clusterStateSuite) TestUpdateClusterIDMismatch(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	st.Lock()
	defer st.Unlock()

	initialBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initialBundle))
	c.Assert(err, check.IsNil)

	updateBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "other-cluster-id", 2, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.ErrorMatches, `cluster assertion id "other-cluster-id" does not match expected id "cluster-id"`)
}

func (s *clusterStateSuite) TestUpdateAuthorityMismatch(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	st.Lock()
	defer st.Unlock()

	initialBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initialBundle))
	c.Assert(err, check.IsNil)

	otherAccount := "other-brand"
	otherKey, _ := assertstest.GenerateKey(752)
	sa.Register(otherAccount, otherKey, map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
	})

	updateBundle, _ := makeClusterBundleWithSigning(c, sa, otherAccount, "cluster-id", 2, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.ErrorMatches, `cluster assertion authority "other-brand" does not match expected authority "cluster-brand"`)
}

func (s *clusterStateSuite) TestUpdateAllowsSequenceSkip(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	st.Lock()
	defer st.Unlock()

	initialBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initialBundle))
	c.Assert(err, check.IsNil)

	updateBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 5, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.IsNil)

	cluster, err := clusterstate.CurrentCluster(st)
	c.Assert(err, check.IsNil)
	c.Assert(cluster.Sequence(), check.Equals, 5)
}

func (s *clusterStateSuite) TestUpdateSuccess(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	st.Lock()
	defer st.Unlock()

	initialBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initialBundle))
	c.Assert(err, check.IsNil)

	updateBundle, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 2, devices, subclusters)
	err = clusterstate.UpdateCluster(st, bytes.NewReader(updateBundle))
	c.Assert(err, check.IsNil)

	cluster, err := clusterstate.CurrentCluster(st)
	c.Assert(err, check.IsNil)
	c.Assert(cluster.Sequence(), check.Equals, 2)
}

func (s *clusterStateSuite) TestInitializeNewClusterMissingClusterAssertion(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	bundle := makeBundleWithoutCluster(c, stack)

	st.Lock()
	defer st.Unlock()

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.ErrorMatches, "assertion bundle missing cluster assertion")
}

func (s *clusterStateSuite) TestInitializeNewClusterUntrustedBrand(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	bundle := makeBundleWithUntrustedBrand(c)

	st.Lock()
	defer st.Unlock()

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, `(?s)cannot add cluster assertion bundle:.*no matching public key.*`)
}

func (s *clusterStateSuite) TestInitializeNewClusterSameIDAsExisting(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	devices := []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}
	subclusters := []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	}

	st.Lock()
	defer st.Unlock()

	initial, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 1, devices, subclusters)
	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(initial))
	c.Assert(err, check.IsNil)

	duplicate, _ := makeClusterBundleWithSigning(c, sa, accountID, "cluster-id", 2, devices, subclusters)
	err = clusterstate.InitializeNewCluster(st, bytes.NewReader(duplicate))
	c.Assert(err, check.ErrorMatches, `cluster assertion id "cluster-id" matches existing cluster id`)

	cluster, err := clusterstate.CurrentCluster(st)
	c.Assert(err, check.IsNil)
	c.Assert(cluster.ClusterID(), check.Equals, "cluster-id")
	c.Assert(cluster.Sequence(), check.Equals, 1)
}

func (s *clusterStateSuite) TestRejectMultipleClusterAssertions(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	const accountID = "cluster-brand"
	sa := registerAccount(stack, accountID)

	_, one := makeClusterBundleWithSigning(c, sa, accountID, "cluster-one", 1, nil, nil)
	_, two := makeClusterBundleWithSigning(c, sa, accountID, "cluster-two", 2, nil, nil)

	var bundle bytes.Buffer
	enc := asserts.NewEncoder(&bundle)
	for _, as := range []asserts.Assertion{
		sa.Account(accountID),
		sa.AccountKey(accountID),
		one,
		two,
	} {
		err := enc.Encode(as)
		c.Assert(err, check.IsNil)
	}

	st.Lock()
	defer st.Unlock()

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle.Bytes()))
	c.Assert(err, check.ErrorMatches, "cluster assertion bundle contains multiple cluster assertions")

	err = clusterstate.UpdateCluster(st, bytes.NewReader(bundle.Bytes()))
	c.Assert(err, check.ErrorMatches, "cluster assertion bundle contains multiple cluster assertions")
}

type managerSuite struct{}

var _ = check.Suite(&managerSuite{})

func (s *managerSuite) TestEnsureIdempotent(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{
		{
			"name":    "default",
			"devices": []any{"1"},
			"snaps":   []any{},
		},
	})

	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()

	err = mgr.Ensure()
	c.Assert(err, check.IsNil)

	// make sure that calling Ensure a second time with the same assertion
	// doesn't result in an error
	err = mgr.Ensure()
	c.Assert(err, check.IsNil)
}

func (s *managerSuite) TestEnsureClusteringDisabled(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	err := tr.Set("core", "experimental.clustering", false)
	c.Assert(err, check.IsNil)
	tr.Commit()

	st.Unlock()
	defer st.Lock()

	mgr := clusterstate.Manager(st)

	err = mgr.Ensure()
	c.Assert(err, check.IsNil)
}

func (s *managerSuite) TestEnsureLoopHasLogging(c *check.C) {
	testutil.CheckEnsureLoopLogging("clustermgr.go", c, false)
}

func (s *managerSuite) TestApplyClusterStateNoActions(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
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

	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	restore := clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
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

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	c.Assert(mgr.Ensure(), check.IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Changes(), check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateDeviceNotInAnySubcluster(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
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

	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	restore := clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
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

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)

	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	c.Assert(mgr.Ensure(), check.IsNil)

	st.Lock()
	defer st.Unlock()
	c.Assert(st.Changes(), check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateInstallRemoveAndUpdate(c *check.C) {
	st, stack := newStateWithStoreStack(c)

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

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
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

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	var updates []snapstate.StoreUpdate
	var installs []snapstate.StoreSnap
	var removals []string

	restore := clusterstate.MockStoreUpdateGoal(func(upds ...snapstate.StoreUpdate) snapstate.UpdateGoal {
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

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	c.Assert(mgr.Ensure(), check.IsNil)

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

	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	c.Assert(changes, check.HasLen, 1)
	chg := changes[0]
	c.Assert(chg.Kind(), check.Equals, "apply-cluster-subcluster-default")
	tasks := chg.Tasks()
	c.Assert(tasks, check.HasLen, 3)
	c.Assert([]string{tasks[0].Kind(), tasks[1].Kind(), tasks[2].Kind()}, check.DeepEquals, []string{"remove", "update", "install"})
}

func (s *managerSuite) TestApplyClusterStateMultipleSubclusters(c *check.C) {
	st, stack := newStateWithStoreStack(c)

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

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
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

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	restore := clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
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

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	c.Assert(mgr.Ensure(), check.IsNil)

	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	c.Assert(changes, check.HasLen, 2)

	var (
		seenOne bool
		seenTwo bool
	)

	for _, chg := range changes {
		switch chg.Kind() {
		case "apply-cluster-subcluster-one":
			seenOne = true
			tasks := chg.Tasks()
			c.Assert(tasks, check.HasLen, 1)
			c.Assert(tasks[0].Kind(), check.Equals, "install")
		case "apply-cluster-subcluster-two":
			seenTwo = true
			tasks := chg.Tasks()
			c.Assert(tasks, check.HasLen, 1)
			c.Assert(tasks[0].Kind(), check.Equals, "remove")
		default:
			c.Fatalf("unexpected change kind %q", chg.Kind())
		}
	}

	c.Assert(seenOne, check.Equals, true)
	c.Assert(seenTwo, check.Equals, true)
}

func (s *managerSuite) TestApplyClusterStateDeviceMissing(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	bundle, _ := makeClusterBundle(c, stack, []map[string]any{
		{
			"id":        "1",
			"brand-id":  "canonical",
			"model":     "ubuntu-core-24-amd64",
			"serial":    "serial-1",
			"addresses": []any{"192.168.0.10"},
		},
	}, []map[string]any{})

	st.Lock()
	defer st.Unlock()

	serial := makeSerialAssertion(c, stack, "serial-9")
	addSerialToState(c, st, serial)

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	err = mgr.Ensure()
	c.Assert(err, check.ErrorMatches, `device with serial "serial-9" not found in cluster assertion`)

	st.Lock()
	defer st.Unlock()
	c.Assert(st.Changes(), check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateNoClusterData(c *check.C) {
	st, _ := newStateWithStoreStack(c)

	mgr := clusterstate.Manager(st)

	c.Assert(mgr.Ensure(), check.IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Changes(), check.HasLen, 0)
}

func (s *managerSuite) TestApplyClusterStateSkipsExistingChange(c *check.C) {
	st, stack := newStateWithStoreStack(c)

	st.Lock()
	defer st.Unlock()

	snapstate.Set(st, "snap-two", &snapstate.SnapState{
		Current:         snap.R(1),
		TrackingChannel: "latest/stable",
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{
				sequence.NewRevisionSideState(&snap.SideInfo{Revision: snap.R(1)}, nil),
			},
		},
	})

	bundle, cluster := makeClusterBundle(c, stack, []map[string]any{
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
					"instance": "snap-one",
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
					"instance": "snap-two",
					"channel":  "latest/stable",
				},
			},
		},
	})

	subclusters := cluster.Subclusters()

	existing := st.NewChange(fmt.Sprintf("apply-cluster-subcluster-%s", subclusters[0].Name), "existing subcluster change")
	existingTask := st.NewTask("existing", "existing task")
	existing.AddAll(state.NewTaskSet(existingTask))
	existing.SetStatus(state.DoStatus)

	serial := makeSerialAssertion(c, stack, "serial-1")
	addSerialToState(c, st, serial)

	restore := clusterstate.MockInstallWithGoal(func(ctx context.Context, st *state.State, goal snapstate.InstallGoal, opts snapstate.Options) ([]*snap.Info, []*state.TaskSet, error) {
		task := st.NewTask("one-task", "apply subcluster one")
		return nil, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	restore = clusterstate.MockRemoveMany(func(st *state.State, names []string, flags *snapstate.RemoveFlags) ([]string, []*state.TaskSet, error) {
		task := st.NewTask("two-task", "apply subcluster two")
		return names, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})
	defer restore()

	restore = clusterstate.MockSnapstateUpdateWithGoal(func(context.Context, *state.State, snapstate.UpdateGoal, func(*snap.Info, *snapstate.SnapState) bool, snapstate.Options) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Fatal("unexpected update")
		return nil, nil, errors.New("unexpected")
	})
	defer restore()

	err := clusterstate.InitializeNewCluster(st, bytes.NewReader(bundle))
	c.Assert(err, check.IsNil)
	mgr := clusterstate.Manager(st)

	st.Unlock()
	defer st.Lock()
	c.Assert(mgr.Ensure(), check.IsNil)

	st.Lock()
	defer st.Unlock()

	var (
		oneChanges int
		twoChanges int
	)

	for _, chg := range st.Changes() {
		switch chg.Kind() {
		case fmt.Sprintf("apply-cluster-subcluster-%s", subclusters[0].Name):
			oneChanges++
		case fmt.Sprintf("apply-cluster-subcluster-%s", subclusters[1].Name):
			twoChanges++
		}
	}

	c.Assert(oneChanges, check.Equals, 1)
	c.Assert(twoChanges, check.Equals, 1)
}

func registerAccount(stack *assertstest.StoreStack, accountID string) *assertstest.SigningAccounts {
	sa := assertstest.NewSigningAccounts(stack)

	key, _ := assertstest.GenerateKey(752)
	sa.Register(accountID, key, map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
	})

	return sa
}

func makeClusterBundleWithSigning(
	c *check.C,
	sa *assertstest.SigningAccounts,
	accountID string,
	clusterID string,
	sequence int,
	devices []map[string]any,
	subclusters []map[string]any,
) ([]byte, *asserts.Cluster) {
	devs := make([]any, 0, len(devices))
	for _, dev := range devices {
		devs = append(devs, dev)
	}

	scs := make([]any, 0, len(subclusters))
	for _, sc := range subclusters {
		scs = append(scs, sc)
	}

	headers := map[string]any{
		"type":        "cluster",
		"cluster-id":  clusterID,
		"sequence":    strconv.Itoa(sequence),
		"devices":     devs,
		"subclusters": scs,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	clusterAssertion, err := sa.Signing(accountID).Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)
	cluster := clusterAssertion.(*asserts.Cluster)

	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	for _, as := range []asserts.Assertion{sa.Account(accountID), sa.AccountKey(accountID), cluster} {
		err := enc.Encode(as)
		c.Assert(err, check.IsNil)
	}

	return buf.Bytes(), cluster
}

func makeSerialAssertion(c *check.C, stack *assertstest.StoreStack, serial string) *asserts.Serial {
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

	a, err := stack.Sign(asserts.SerialType, headers, nil, "")
	c.Assert(err, check.IsNil)

	return a.(*asserts.Serial)
}

func addSerialToState(c *check.C, st *state.State, serial *asserts.Serial) {
	err := assertstate.Add(st, serial)
	c.Assert(err, check.IsNil)

	err = devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  serial.BrandID(),
		Model:  serial.Model(),
		Serial: serial.Serial(),
	})
	c.Assert(err, check.IsNil)
}

func makeClusterBundle(c *check.C, stack *assertstest.StoreStack, devices []map[string]any, subclusters []map[string]any) ([]byte, *asserts.Cluster) {
	sa := assertstest.NewSigningAccounts(stack)

	clusterKey, _ := assertstest.GenerateKey(752)
	const accountID = "cluster-brand"
	brandSigning := sa.Register(accountID, clusterKey, map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
	})

	devs := make([]any, 0, len(devices))
	for _, dev := range devices {
		devs = append(devs, dev)
	}

	clusters := make([]any, 0, len(subclusters))
	for _, sc := range subclusters {
		clusters = append(clusters, sc)
	}

	headers := map[string]any{
		"type":        "cluster",
		"cluster-id":  "cluster-id",
		"sequence":    "1",
		"devices":     devs,
		"subclusters": clusters,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	a, err := brandSigning.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)
	cluster := a.(*asserts.Cluster)

	account := sa.Account(accountID)
	accountKey := sa.AccountKey(accountID)

	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	for _, as := range []asserts.Assertion{account, accountKey, cluster} {
		err := enc.Encode(as)
		c.Assert(err, check.IsNil)
	}

	return buf.Bytes(), cluster
}

func makeBundleWithoutCluster(c *check.C, stack *assertstest.StoreStack) []byte {
	sa := assertstest.NewSigningAccounts(stack)

	clusterKey, _ := assertstest.GenerateKey(752)
	const accountID = "cluster-brand"
	sa.Register(accountID, clusterKey, map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
	})

	account := sa.Account(accountID)
	accountKey := sa.AccountKey(accountID)

	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	for _, as := range []asserts.Assertion{account, accountKey} {
		err := enc.Encode(as)
		c.Assert(err, check.IsNil)
	}

	return buf.Bytes()
}

func makeBundleWithUntrustedBrand(c *check.C) []byte {
	untrustedStack := assertstest.NewStoreStack("untrusted", nil)
	sa := assertstest.NewSigningAccounts(untrustedStack)

	clusterKey, _ := assertstest.GenerateKey(752)
	const accountID = "untrusted-brand"
	brandSigning := sa.Register(accountID, clusterKey, map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
	})

	headers := map[string]any{
		"type":       "cluster",
		"cluster-id": "cluster-id",
		"sequence":   "1",
		"devices": []any{
			map[string]any{
				"id":        "1",
				"brand-id":  "canonical",
				"model":     "ubuntu-core-24-amd64",
				"serial":    "serial-1",
				"addresses": []any{"192.168.0.10"},
			},
		},
		"subclusters": []any{
			map[string]any{
				"name": "default",
				"devices": []any{
					"1",
				},
				"snaps": []any{},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	clusterAssertion, err := brandSigning.Sign(asserts.ClusterType, headers, nil, "")
	c.Assert(err, check.IsNil)

	account := sa.Account(accountID)
	accountKey := sa.AccountKey(accountID)

	var buf bytes.Buffer
	enc := asserts.NewEncoder(&buf)
	for _, as := range []asserts.Assertion{account, accountKey, clusterAssertion} {
		err := enc.Encode(as)
		c.Assert(err, check.IsNil)
	}

	return buf.Bytes()
}

func newStateWithStoreStack(c *check.C) (*state.State, *assertstest.StoreStack) {
	signing := assertstest.NewStoreStack("canonical", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   signing.Trusted,
	})
	c.Assert(err, check.IsNil)

	err = db.Add(signing.StoreAccountKey(""))
	c.Assert(err, check.IsNil)

	st := state.New(nil)

	st.Lock()
	defer st.Unlock()

	assertstate.ReplaceDB(st, db)

	tr := config.NewTransaction(st)
	err = tr.Set("core", "experimental.clustering", true)
	c.Assert(err, check.IsNil)
	tr.Commit()

	return st, signing
}
