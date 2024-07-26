// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

package snapstate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store/storetest"
	userclient "github.com/snapcore/snapd/usersession/client"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

func verifyUpdateTasks(c *C, typ snap.Type, opts, discards int, ts *state.TaskSet) {
	verifyUpdateTasksWithComponents(c, typ, opts, discards, nil, ts)
}

func verifyUpdateTasksWithComponents(c *C, typ snap.Type, opts, discards int, components []string, ts *state.TaskSet) {
	kinds := taskKinds(ts.Tasks())

	expected := expectedDoInstallTasks(typ, unlinkBefore|cleanupAfter|opts, discards, nil, components, nil)
	if opts&doesReRefresh != 0 {
		expected = append(expected, "check-rerefresh")
	}

	c.Assert(kinds, DeepEquals, expected)

	te := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	if opts&localSnap != 0 || opts&localRevision != 0 {
		c.Assert(te.Kind(), Equals, "prepare-snap")
	} else {
		c.Assert(te.Kind(), Equals, "validate-snap")
	}
}

// mockRestartAndSettle expects the state to be locked
func (s *snapmgrTestSuite) mockRestartAndSettle(c *C, chg *state.Change) {
	restart.MockPending(s.state, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)
	s.settle(c)
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := release.MockOnClassic(false)
	defer restore()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// ensure garbage collection runs as the last tasks
	expectedTail := fakeOps{
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/1"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/1"),
			stype: "app",
		},
		{
			op:   "remove-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/2"),
		},
		{
			op:    "remove-snap-files",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/2"),
			stype: "app",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(11),
		},
	}

	opsTail := s.fakeBackend.ops[len(s.fakeBackend.ops)-len(expectedTail):]
	c.Assert(opsTail.Ops(), DeepEquals, expectedTail.Ops())
	c.Check(opsTail, DeepEquals, expectedTail)
}

func (s *snapmgrTestSuite) TestRepeatedUpdatesDoGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := release.MockOnClassic(false)
	defer restore()

	// start with a single revision
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Channel: "some-channel", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	// allow 2 revisions
	c.Assert(tr.Set("core", "refresh.retain", 2), IsNil)
	tr.Commit()

	s.fakeStore.refreshRevnos = make(map[string]snap.Revision)

	for refreshRev := 2; refreshRev < 10; refreshRev++ {
		s.fakeStore.refreshRevnos["some-snap-id"] = snap.R(refreshRev)

		chg := s.state.NewChange("update", "update a snap")
		ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		chg.AddAll(ts)

		s.settle(c)

		var snapst snapstate.SnapState
		c.Assert(snapstate.Get(s.state, "some-snap", &snapst), IsNil)
		// and we expect 2 revisions at all times
		c.Check(snapst.Sequence.Revisions, HasLen, 2)
		c.Check(snapst.Sequence, DeepEquals, snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Channel: "some-channel", Revision: snap.R(refreshRev - 1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Channel: "some-channel", Revision: snap.R(refreshRev)},
		}))
	}
}

func (s *snapmgrTestSuite) TestUpdateScenarios(c *C) {
	// TODO: also use channel-for-7 or equiv to check updates that are switches
	for k, t := range switchScenarios {
		s.testUpdateScenario(c, k, t)
	}
}

func (s *snapmgrTestSuite) testUpdateScenario(c *C, desc string, t switchScenario) {
	// reset
	s.fakeBackend.ops = nil

	comment := Commentf("%q (%+v)", desc, t)
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		Channel:  t.chanFrom,
		SnapID:   "some-snap-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Active:          true,
		Current:         si.Revision,
		TrackingChannel: t.chanFrom,
		CohortKey:       t.cohFrom,
	})

	chg := s.state.NewChange("update-snap", t.summary)
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{
		Channel:     t.chanTo,
		CohortKey:   t.cohTo,
		LeaveCohort: t.cohFrom != "" && t.cohTo == "",
	}, 0, snapstate.Flags{})
	c.Assert(err, IsNil, comment)
	chg.AddAll(ts)

	s.settle(c)

	// switch is not really really doing anything backend related
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, []string{
		"storesvc-snap-action",
		"storesvc-snap-action:action",
		"storesvc-download",
		"validate-snap:Doing",
		"current",
		"open-snap-file",
		"setup-snap",
		"remove-snap-aliases",
		"run-inhibit-snap-for-unlink",
		"unlink-snap",
		"copy-data",
		"setup-snap-save-data",
		"setup-profiles:Doing",
		"candidate",
		"link-snap",
		"auto-connect:Doing",
		"update-aliases",
		"cleanup-trash",
	}, comment)

	expectedChanTo := t.chanTo
	if t.chanTo == "" {
		expectedChanTo = t.chanFrom
	}
	expectedCohTo := t.cohTo

	// ensure the desired channel/cohort has changed
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil, comment)
	c.Assert(snapst.TrackingChannel, Equals, expectedChanTo, comment)
	c.Assert(snapst.CohortKey, Equals, expectedCohTo, comment)

	// ensure the current info *has* changed
	info, err := snapst.CurrentInfo()
	c.Assert(err, IsNil, comment)
	c.Assert(info.Channel, Equals, expectedChanTo, comment)
}

func (s *snapmgrTestSuite) TestUpdateTasksWithOldCurrent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	restore := release.MockOnClassic(false)
	defer restore()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1, si2, si3, si4}),
		Current:         snap.R(2),
		SnapType:        "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 2, ts)

	// and ensure that it will remove the revisions after "current"
	// (si3, si4)
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()

	i := len(tasks) - 8
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si3.Revision)

	i = len(tasks) - 6
	c.Check(tasks[i].Kind(), Equals, "clear-snap")
	err = tasks[i].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, si4.Revision)
}

func (s *snapmgrTestSuite) enableRefreshAppAwarenessUX() {
	s.state.Lock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness-ux", true)
	tr.Commit()
	s.state.Unlock()
}

func (s *snapmgrTestSuite) testUpdateCanDoBackwards(c *C, refreshAppAwarenessUX bool) {
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
		Current:  si11.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(7)}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "",
				Revision: snap.R(7),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  "some-snap",
			revno: snap.R(7),
		},
	}
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if refreshAppAwarenessUX {
		// remove "remove-snap-aliases" operation
		expected = expected[1:]
	}
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestUpdateCanDoBackwards(c *C) {
	s.testUpdateCanDoBackwards(c, false)
}

func (s *snapmgrTestSuite) TestUpdateCanDoBackwardsSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testUpdateCanDoBackwards(c, true)
}

func revs(seq []*sequence.RevisionSideState) []int {
	revs := make([]int, len(seq))
	for i, si := range seq {
		revs[i] = si.Snap.Revision.N
	}

	return revs
}

type opSeqOpts struct {
	revert  bool
	fail    bool
	before  []int
	current int
	via     int
	after   []int
}

// build a SnapState with a revision sequence given by `before` and a
// current revision of `current`. Then refresh --revision via. Then
// check the revision sequence is as in `after`.
func (s *snapmgrTestSuite) testOpSequence(c *C, opts *opSeqOpts) (*snapstate.SnapState, *state.TaskSet) {
	s.state.Lock()
	defer s.state.Unlock()

	seq := make([]*snap.SideInfo, len(opts.before))
	for i, n := range opts.before {
		seq[i] = &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(n)}
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos(seq),
		Current:         snap.R(opts.current),
		SnapType:        "app",
	})

	var chg *state.Change
	var ts *state.TaskSet
	var err error
	if opts.revert {
		chg = s.state.NewChange("revert", "revert a snap")
		ts, err = snapstate.RevertToRevision(s.state, "some-snap", snap.R(opts.via), snapstate.Flags{}, "")
	} else {
		chg = s.state.NewChange("refresh", "refresh a snap")
		ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(opts.via)}, s.user.ID, snapstate.Flags{})
	}
	c.Assert(err, IsNil)
	if opts.fail {
		tasks := ts.Tasks()
		var last *state.Task
		// don't make a task wait on rerefresh, that's bad
		for i := len(tasks) - 1; i > 0; i-- {
			last = tasks[i]
			if last.Kind() != "check-rerefresh" {
				break
			}
		}
		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		if len(last.Lanes()) > 0 {
			lanes := last.Lanes()
			// validity
			c.Assert(lanes, HasLen, 1)
			terr.JoinLane(lanes[0])
		}
		chg.AddTask(terr)
	}
	chg.AddAll(ts)

	s.settle(c)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(revs(snapst.Sequence.Revisions), DeepEquals, opts.after)

	return &snapst, ts
}

func (s *snapmgrTestSuite) testUpdateSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	restore := release.MockOnClassic(false)
	defer restore()

	opts.revert = false
	snapst, ts := s.testOpSequence(c, opts)
	// update always ends with current==seq[-1]==via:
	c.Check(snapst.Current.N, Equals, opts.after[len(opts.after)-1])
	c.Check(snapst.Current.N, Equals, opts.via)

	c.Check(s.fakeBackend.ops.Count("copy-data"), Equals, 1)
	c.Check(s.fakeBackend.ops.First("copy-data"), DeepEquals, &fakeOp{
		op:   "copy-data",
		path: fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via),
		old:  fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.current),
	})

	return ts
}

func (s *snapmgrTestSuite) testUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	restore := release.MockOnClassic(false)
	defer restore()

	opts.revert = false
	opts.after = opts.before
	s.fakeBackend.linkSnapFailTrigger = fmt.Sprintf(filepath.Join(dirs.SnapMountDir, "some-snap/%d"), opts.via)
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

// testTotal*Failure fails *after* link-snap
func (s *snapmgrTestSuite) testTotalUpdateFailureSequence(c *C, opts *opSeqOpts) *state.TaskSet {
	restore := release.MockOnClassic(false)
	defer restore()

	opts.revert = false
	opts.fail = true
	snapst, ts := s.testOpSequence(c, opts)
	// a failed update will always end with current unchanged
	c.Check(snapst.Current.N, Equals, opts.current)

	ops := s.fakeBackend.ops
	c.Check(ops.Count("copy-data"), Equals, 1)
	do := ops.First("copy-data")

	c.Check(ops.Count("undo-copy-snap-data"), Equals, 1)
	undo := ops.First("undo-copy-snap-data")

	do.op = undo.op
	c.Check(do, DeepEquals, undo) // i.e. they only differed in the op

	return ts
}

func (s *snapmgrTestSuite) TestUpdateLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh to a snap depending on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-layout/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can refresh to a snap depending on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-layout/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyExplicitLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh multiple snaps if one of them depends on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-layout/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, _, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyLayoutsChecksFeatureFlag(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// When layouts are disabled we cannot refresh multiple snaps if one of them depends on the feature.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", false)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-layout/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	refreshes, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, HasLen, 0)

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	refreshes, _, err = snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestUpdateFailsEarlyOnEpochMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-epoch-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-epoch-snap", SnapID: "some-epoch-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-epoch-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot refresh "some-epoch-snap" to new revision 11 with epoch 42, because it can't read the current epoch of 13`)
}

func (s *snapmgrTestSuite) TestUpdateTasksPropagatesErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "fakestore-please-error-on-refresh", Revision: snap.R(7)}}),
		Current:         snap.R(7),
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `failing as requested`)
}

func (s *snapmgrTestSuite) TestUpdateTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 0, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "some-channel")
}

func (s *snapmgrTestSuite) TestUpdateAmendRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: some-snap`, &si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, []string{
		"storesvc-snap-action",
		"storesvc-snap-action:action",
		"storesvc-download",
		"validate-snap:Doing",
		"current",
		"open-snap-file",
		"setup-snap",
		"remove-snap-aliases",
		"run-inhibit-snap-for-unlink",
		"unlink-snap",
		"copy-data",
		"setup-snap-save-data",
		"setup-profiles:Doing",
		"candidate",
		"link-snap",
		"auto-connect:Doing",
		"update-aliases",
		"cleanup-trash",
	})
	// just check the interesting op
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "install", // we asked for an Update, but an amend is actually an Install
			InstanceName: "some-snap",
			Channel:      "some-channel",
			Epoch:        snap.E("1*"), // in amend, epoch in the action is not nil!
		},
		revno:  snap.R(11),
		userID: 1,
	})

	task := ts.Tasks()[1]
	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
			Size:        5,
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		Version:   "some-snapVer",
		PlugsOnly: true,
		Flags: snapstate.Flags{
			Amend:       true,
			Transaction: client.TransactionPerSnap,
		},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
	})

	// verify services stop reason
	verifyStopReason(c, ts, "refresh")

	// check post-refresh hook
	task = ts.Tasks()[14]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "some-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(-42),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}, nil))
}

func (s *snapmgrTestSuite) testUpdateRunThrough(c *C, refreshAppAwarenessUX bool) {
	// we start without the auxiliary store info (or with an older one)
	c.Check(snapstate.AuxStoreInfoFilename("services-snap-id"), testutil.FileAbsent)

	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(7),
		SnapID:   "services-snap-id",
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)
	fi, err := os.Stat(snap.MountFile("services-snap", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	now, err := time.Parse(time.RFC3339, "2021-06-10T10:00:00Z")
	c.Assert(err, IsNil)
	restoreTimeNow := snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restoreTimeNow()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		CohortKey:       "embattled",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{
		Channel:   "some-channel",
		CohortKey: "some-cohort",
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// check unlink-reason
	unlinkTask := findLastTask(chg, "unlink-current-snap")
	c.Assert(unlinkTask, NotNil)
	var unlinkReason string
	unlinkTask.Get("unlink-reason", &unlinkReason)
	c.Check(unlinkReason, Equals, "refresh")

	// local modifications, edge must be set
	te := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	c.Assert(te.Kind(), Equals, "validate-snap")

	s.settle(c)

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "services-snap",
				SnapID:          "services-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("0"),
				CohortKey:       "embattled",
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "services-snap",
				SnapID:       "services-snap-id",
				Channel:      "some-channel",
				CohortKey:    "some-cohort",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "services-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "services-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services:refresh",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op: "current-snap-service-states",
		},
	}
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: "services-snap",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "services-snap",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "services-snap/7"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "services-snap"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "services-snap/11"),
			services: []string{"svc1", "svc3", "svc2"},
		},
		{
			op:    "cleanup-trash",
			name:  "services-snap",
			revno: snap.R(11),
		},
	}...)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "services-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:   "some-channel",
		CohortKey: "some-cohort",
		UserID:    s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "services-snap_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		Version:   "services-snapVer",
		PlugsOnly: true,
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
	})

	// verify services stop reason
	verifyStopReason(c, ts, "refresh")

	// check post-refresh hook
	task = ts.Tasks()[14]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.LastRefreshTime, NotNil)
	c.Check(snapst.LastRefreshTime.Equal(now), Equals, true)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	}, nil))
	c.Check(snapst.CohortKey, Equals, "some-cohort")

	// we end up with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("services-snap-id"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
	s.testUpdateRunThrough(c, false)
}

func (s *snapmgrTestSuite) TestUpdateRunThroughSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testUpdateRunThrough(c, true)
}

func (s *snapmgrTestSuite) TestUpdateDropsRevertStatus(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(7),
		SnapID:   "services-snap-id",
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	s.state.Lock()
	defer s.state.Unlock()

	si2 := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(11),
		SnapID:   "services-snap-id",
	}
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si, &si2}),
		Current:  si.Revision,
		RevertStatus: map[int]snapstate.RevertStatus{
			11: snapstate.NotBlocked,
		},
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		CohortKey:       "embattled",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{
		Channel:   "some-channel",
		CohortKey: "some-cohort",
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "services-snap", &snapst), IsNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Current, Equals, snap.R(11))
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	}, nil))
	c.Check(snapst.RevertStatus, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateResetsHoldState(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
		SnapID:   "some-snap-id",
	}
	snaptest.MockSnap(c, `name: some-snap`, &si)

	si2 := snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(7),
		SnapID:   "other-snap-id",
	}
	snaptest.MockSnap(c, `name: other-snap`, &si2)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si2}),
		Current:  si.Revision,
		SnapType: "app",
	})

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// pretend that the snap was held during last auto-refresh
	_, err := snapstate.HoldRefresh(s.state, snapstate.HoldAutoRefresh, "gating-snap", 0, "some-snap", "other-snap")
	c.Assert(err, IsNil)
	// validity check
	held, err := snapstate.HeldSnaps(s.state, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{
		"some-snap":  {"gating-snap"},
		"other-snap": {"gating-snap"},
	})

	_, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// and it is not held anymore (but other-snap still is)
	held, err = snapstate.HeldSnaps(s.state, snapstate.HoldAutoRefresh)
	c.Assert(err, IsNil)
	c.Check(held, DeepEquals, map[string][]string{
		"other-snap": {"gating-snap"},
	})
}

func (s *snapmgrTestSuite) testParallelInstanceUpdateRunThrough(c *C, refreshAppAwarenessUX bool) {
	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(7),
		SnapID:   "services-snap-id",
	}
	snaptest.MockSnapInstance(c, "services-snap_instance", `name: services-snap`, &si)
	fi, err := os.Stat(snap.MountFile("services-snap_instance", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "services-snap_instance", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		InstanceKey:     "instance",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap_instance", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "services-snap_instance",
				SnapID:          "services-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("0"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				SnapID:       "services-snap-id",
				InstanceName: "services-snap_instance",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "services-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "services-snap_instance",
			path:  filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
			revno: snap.R(11),
		},
		{
			op:   "stop-snap-services:refresh",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op: "current-snap-service-states",
		},
	}
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: "services-snap_instance",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "services-snap_instance",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "services-snap_instance"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "services-snap",
				SnapID:   "services-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
		{
			op:       "start-snap-services",
			path:     filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
			services: []string{"svc1", "svc3", "svc2"},
		},
		{
			op:    "cleanup-trash",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
	}...)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "services-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
	}})
	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "some-channel",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "services-snap_instance_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:    snapsup.SideInfo,
		Type:        snap.TypeApp,
		Version:     "services-snapVer",
		PlugsOnly:   true,
		InstanceKey: "instance",
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(11),
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
	})

	// verify services stop reason
	verifyStopReason(c, ts, "refresh")

	// check post-refresh hook
	task = ts.Tasks()[14]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap_instance" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap_instance", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.InstanceKey, Equals, "instance")
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	}, nil))
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateRunThrough(c *C) {
	s.testParallelInstanceUpdateRunThrough(c, false)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateRunThroughSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testParallelInstanceUpdateRunThrough(c, true)
}

func (s *snapmgrTestSuite) TestUpdateWithNewBase(c *C) {
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-base", target: filepath.Join(dirs.SnapBlobDir, "some-base_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "some-snap", target: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithAlreadyInstalledBase(c *C) {
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "some-base",
			SnapID:   "some-base-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "some-snap", target: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithNewDefaultProvider(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	si := &snap.SideInfo{
		RealName: "snap-content-plug",
		SnapID:   "snap-content-plug-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: snap-content-plug`, si)
	snapstate.Set(s.state, "snap-content-plug", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug", target: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "snap-content-slot", target: filepath.Join(dirs.SnapBlobDir, "snap-content-slot_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateWithInstalledDefaultProvider(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	si := &snap.SideInfo{
		RealName: "snap-content-plug",
		SnapID:   "snap-content-plug-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: snap-content-plug`, si)
	snapstate.Set(s.state, "snap-content-plug", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "snap-content-slot", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "snap-content-slot",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "content"})
	c.Assert(err, IsNil)

	sn := &snap.Info{SuggestedName: "snap-content-slot", Slots: make(map[string]*snap.SlotInfo), Version: "1"}
	slot := &snap.SlotInfo{
		Snap:      sn,
		Name:      "snap-content-slot",
		Interface: "content",
		Attrs: map[string]interface{}{
			"content": "shared-content",
		},
	}
	sn.Slots["snap-content-slot"] = slot

	appSet, err := interfaces.NewSnapAppSet(sn, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug", target: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			c.Check(op.userID, Equals, 1)
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[0], DeepEquals, fakeDownload{
				macaroon: "macaroon",
				name:     "some-snap",
				target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			}, Commentf(snapName))
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateModelKernelSwitchTrackRunThrough(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// use services-snap here to make sure services would be stopped/started appropriately
	si := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	snaptest.MockSnap(c, `name: kernel`, &si)
	fi, err := os.Stat(snap.MountFile("kernel", si.Revision))
	c.Assert(err, IsNil)
	refreshedDate := fi.ModTime()
	// look at disk
	r := snapstate.MockRevisionDate(nil)
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	r1 := snapstatetest.MockDeviceModel(ModelWithKernelTrack("18"))
	defer r1()
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "edge"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Assert(len(s.fakeBackend.ops) > 2, Equals, true)
	c.Assert(s.fakeBackend.ops[:2], DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "kernel",
				SnapID:          "kernel-id",
				Revision:        snap.R(7),
				TrackingChannel: "18/stable",
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		}, {
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "kernel",
				SnapID:       "kernel-id",
				Channel:      "18/edge",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
	})

	// check progress
	task := ts.Tasks()[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: "18/edge",
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, "kernel_11.snap"),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeKernel,
		Version:   "kernelVer",
		PlugsOnly: true,
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(11),
		Channel:  "18/edge",
		SnapID:   "kernel-id",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "kernel", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "18/edge")
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "kernel",
		Channel:  "18/edge",
		SnapID:   "kernel-id",
		Revision: snap.R(11),
	}, nil))
}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsNoUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		}),
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "",
		"some-snap":     "macaroon",
		"services-snap": "macaroon2",
	}

	seen := make(map[string]int)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapID] = op.userID
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 2)
	// we check all snaps with each user
	c.Check(seen["some-snap-id"], Equals, 1)
	c.Check(seen["services-snap-id"], Equals, 2)
	// coalesced with one of the others
	c.Check(seen["core-snap-id"] > 0, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		}),
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// do UpdateMany using user 2 as fallback
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 2, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "macaroon2",
		"some-snap":     "macaroon",
		"services-snap": "macaroon2",
	}

	type snapIDuserID struct {
		snapID string
		userID int
	}
	seen := make(map[snapIDuserID]bool)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			seen[snapIDuserID{snapID: snapID, userID: op.userID}] = true
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 2)
	// we check all snaps with each user
	c.Check(seen, DeepEquals, map[snapIDuserID]bool{
		{snapID: "core-snap-id", userID: 2}:     true,
		{snapID: "some-snap-id", userID: 1}:     true,
		{snapID: "services-snap-id", userID: 2}: true,
	})

	var coreState, snapState snapstate.SnapState
	// user in SnapState was preserved
	err = snapstate.Get(s.state, "some-snap", &snapState)
	c.Assert(err, IsNil)
	c.Check(snapState.UserID, Equals, 1)
	c.Check(snapState.Current, DeepEquals, snap.R(11))

	// user in SnapState was set
	err = snapstate.Get(s.state, "core", &coreState)
	c.Assert(err, IsNil)
	c.Check(coreState.UserID, Equals, 2)
	c.Check(coreState.Current, DeepEquals, snap.R(11))

}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsUserWithNoStoreAuthRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		}),
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   3,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	macaroonMap := map[string]string{
		"core":          "",
		"some-snap":     "macaroon",
		"services-snap": "",
	}

	seen := make(map[string]int)
	ir := 0
	di := 0
	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action":
			ir++
			c.Check(op.curSnaps, DeepEquals, []store.CurrentSnap{
				{
					InstanceName:  "core",
					SnapID:        "core-snap-id",
					Revision:      snap.R(1),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
					Epoch:         snap.E("1*"),
				},
				{
					InstanceName:  "services-snap",
					SnapID:        "services-snap-id",
					Revision:      snap.R(2),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 2),
					Epoch:         snap.E("0"),
				},
				{
					InstanceName:  "some-snap",
					SnapID:        "some-snap-id",
					Revision:      snap.R(5),
					RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 5),
					Epoch:         snap.E("1*"),
				},
			})
		case "storesvc-snap-action:action":
			snapID := op.action.SnapID
			if _, ok := seen[snapID]; !ok {
				seen[snapID] = op.userID
			}
		case "storesvc-download":
			snapName := op.name
			fakeDl := s.fakeStore.downloads[di]
			// check target path separately and clear it
			c.Check(fakeDl.target, Matches, filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_[0-9]+.snap", snapName)))
			fakeDl.target = ""
			c.Check(fakeDl, DeepEquals, fakeDownload{
				macaroon: macaroonMap[snapName],
				name:     snapName,
			}, Commentf(snapName))
			di++
		}
	}
	c.Check(ir, Equals, 1)
	// we check all snaps with each user
	c.Check(seen["some-snap-id"], Equals, 1)
	// coalesced with request for 1
	c.Check(seen["services-snap-id"], Equals, 1)
	c.Check(seen["core-snap-id"], Equals, 1)
}

func (s *snapmgrTestSuite) testUpdateUndoRunThrough(c *C, refreshAppAwarenessUX bool) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.settle(c)

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:  "some-snap",
				SnapID:        "some-snap-id",
				Revision:      snap.R(7),
				RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:         snap.E("1*"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
	}
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: "some-snap",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap.failed",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			unlinkSkipBinaries: false,
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "undo-setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
			old:  filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
	}...)
	// aliases removal undo is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op: "update-aliases",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:    "undo-setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}...)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 1)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	s.testUpdateUndoRunThrough(c, false)
}

func (s *snapmgrTestSuite) TestUpdateUndoRunThroughSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testUpdateUndoRunThrough(c, true)
}

func lastWithLane(tasks []*state.Task) *state.Task {
	for i := len(tasks) - 1; i >= 0; i-- {
		if lanes := tasks[i].Lanes(); len(lanes) == 1 && lanes[0] != 0 {
			return tasks[i]
		}
	}
	return nil
}

func (s *snapmgrTestSuite) TestUpdateUndoRestoresRevisionConfig(c *C) {
	var errorTaskExecuted bool

	// overwrite error-trigger task handler with custom one for this test
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		st := task.State()
		st.Lock()
		defer st.Unlock()

		// modify current config of some-snap
		tr := config.NewTransaction(st)
		tr.Set("some-snap", "foo", "canary")
		tr.Commit()

		errorTaskExecuted = true
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si2 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(6),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si2, &si}),
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
		SnapType:        "app",
	})

	// set some configuration
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "revision 7 value")
	tr.Commit()
	config.SaveRevisionConfig(s.state, "some-snap", snap.R(7))

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	last := lastWithLane(ts.Tasks())
	c.Assert(last, NotNil)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.settle(c)

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(errorTaskExecuted, Equals, true)

	// after undoing the update some-snap config should be restored to that of rev.7
	var val string
	tr = config.NewTransaction(s.state)
	c.Assert(tr.Get("some-snap", "foo", &val), IsNil)
	c.Check(val, Equals, "revision 7 value")
}

func (s *snapmgrTestSuite) TestUpdateMakesConfigSnapshot(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var cfgs map[string]interface{}
	// we don't have config snapshots yet
	c.Assert(s.state.Get("revision-config", &cfgs), testutil.ErrorIs, state.ErrNoState)

	chg := s.state.NewChange("update", "update a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(2)}
	ts, err := snapstate.Update(s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	restore := snapstate.MockEnsuredMountsUpdated(s.snapmgr, true)
	defer restore()

	s.settle(c)

	cfgs = nil
	// config copy of rev. 1 has been made
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{
			"foo": "bar",
		},
	})
}

func (s *snapmgrTestSuite) testUpdateTotalUndoRunThrough(c *C, refreshAppAwarenessUX bool) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
		SnapType:        "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// We need to make it not be rerefresh, and we could do just
	// that but instead we do the 'right' thing and attach it to
	// the last task that's on a lane.
	last := lastWithLane(ts.Tasks())
	c.Assert(last, NotNil)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(last)
	terr.JoinLane(last.Lanes()[0])
	chg.AddTask(terr)

	s.settle(c)

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "latest/stable",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "some-channel",
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  snap.R(11),
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: "some-snap",
		},
		{
			op:    "validate-snap:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:    "setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			revno: snap.R(11),
		},
	}
	// aliases removal is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: "some-snap",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        "some-snap",
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: "some-snap",
				SnapID:   "some-snap-id",
				Channel:  "some-channel",
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "auto-connect:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
	}...)
	// undoing everything from here down...
	if refreshAppAwarenessUX {
		// refresh-app-awareness-ux changes setup-aliases undo behavior
		expected = append(expected, fakeOp{
			op: "update-aliases",
		})
	} else {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: "some-snap",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:    "auto-connect:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:   "undo-copy-snap-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "undo-setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
			old:  filepath.Join(dirs.SnapDataSaveDir, "some-snap"),
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
	}...)
	// aliases removal undo is skipped when refresh-app-awareness-ux is enabled
	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op: "update-aliases",
		})
	}
	expected = append(expected, fakeOps{
		{
			op:    "undo-setup-snap",
			name:  "some-snap",
			path:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			stype: "app",
		},
		{
			op:   "remove-snap-dir",
			name: "some-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap"),
		},
	}...)

	// ensure all our tasks ran
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
	}})
	// friendlier failure first
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.TrackingChannel, Equals, "latest/stable")
	c.Assert(snapst.Sequence.Revisions, HasLen, 1)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	}, nil))
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	s.testUpdateTotalUndoRunThrough(c, false)
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThroughSkipBinaries(c *C) {
	s.enableRefreshAppAwarenessUX()
	s.testUpdateTotalUndoRunThrough(c, true)
}

func (s *snapmgrTestSuite) TestUpdateSameRevision(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err.Error(), Equals, "snap has no updates available")
}

func (s *snapmgrTestSuite) TestUpdateToRevisionRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		}),
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	for _, op := range s.fakeBackend.ops {
		switch op.op {
		case "storesvc-snap-action:action":
			c.Check(op.userID, Equals, 1)
		case "storesvc-download":
			snapName := op.name
			c.Check(s.fakeStore.downloads[0], DeepEquals, fakeDownload{
				macaroon: "macaroon",
				name:     "some-snap",
				target:   filepath.Join(dirs.SnapBlobDir, "some-snap_11.snap"),
			}, Commentf(snapName))
		}
	}
}

// A noResultsStore returns no results for install/refresh requests
type noResultsStore struct {
	*fakeStore
}

func (n noResultsStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		panic("no assertion query support")
	}
	return nil, nil, &store.SnapActionError{NoResults: true}
}

func (s *snapmgrTestSuite) TestUpdateNoStoreResults(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, noResultsStore{fakeStore: s.fakeStore})

	// this is an atypical case in which the store didn't return
	// an error nor a result, we are defensive and return
	// a reasonable error
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, snapstate.ErrMissingExpectedResult)
}

func (s *snapmgrTestSuite) TestUpdateNoStoreResultsWithChannelChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, noResultsStore{fakeStore: s.fakeStore})

	// this is an atypical case in which the store didn't return
	// an error nor a result, we are defensive and return
	// a reasonable error
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-9/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, snapstate.ErrMissingExpectedResult)
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchesChannel(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "other-chanenl/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchesChannelConflict(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionSwitchChannelRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "other-channel",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	// no local modifications, hence no edge
	te := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, IsNil)

	s.settle(c)

	expected := fakeOps{
		// we just expect the "storesvc-snap-action" ops, we
		// don't have a fakeOp for switchChannel because it has
		// not a backend method, it just manipulates the state
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    "some-snap",
				SnapID:          "some-snap-id",
				Revision:        snap.R(7),
				TrackingChannel: "other-channel/stable",
				RefreshedDate:   fakeRevDateEpoch.AddDate(0, 0, 7),
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		},

		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: "some-snap",
				SnapID:       "some-snap-id",
				Channel:      "channel-for-7/stable",
				Flags:        store.SnapActionEnforceValidation,
			},
			userID: 1,
		},
	}

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel:   "channel-for-7/stable",
		UserID:    s.user.ID,
		Type:      "app",
		PlugsOnly: true,
		Version:   "some-snapVer",
		SideInfo:  snapsup.SideInfo,
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_7.snap"),
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7/stable",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 1)
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7/stable",
		Revision: snap.R(7),
	}, nil))
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidation(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "toggle-snap-flags")
}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidationConflict(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	// make it visible
	s.state.NewChange("refresh", "refresh a snap").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Check(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)

}

func (s *snapmgrTestSuite) TestUpdateSameRevisionToggleIgnoreValidationRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.settle(c)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup, DeepEquals, snapstate.SnapSetup{
		SideInfo:  snapsup.SideInfo,
		Channel:   "channel-for-7/stable",
		UserID:    s.user.ID,
		Type:      "app",
		PlugsOnly: true,
		Version:   "some-snapVer",
		Flags: snapstate.Flags{
			IgnoreValidation: true,
			Transaction:      client.TransactionPerSnap,
		},
		SnapPath: filepath.Join(dirs.SnapBlobDir, "some-snap_7.snap"),
	})
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7/stable",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence.Revisions, HasLen, 1)
	c.Check(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7",
		Revision: snap.R(7),
	}, nil))
	c.Check(snapst.IgnoreValidation, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNo(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, validateErr)
}

func (s *snapmgrTestSuite) TestUpdateValidateRefreshesSaysNoButIgnoreValidationIsSet(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	flags := snapstate.Flags{JailMode: true, IgnoreValidation: true, Transaction: client.TransactionPerSnap}
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags, DeepEquals, flags.ForSnapSetup())
}

func (s *snapmgrTestSuite) TestUpdateIgnoreValidationSticky(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshesFail := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		if len(ignoreValidation) == 0 {
			return nil, validateErr
		}
		c.Check(ignoreValidation, DeepEquals, map[string]bool{
			"some-snap": true,
		})
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshesFail

	flags := snapstate.Flags{IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Channel:      "stable",
			Flags:        store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.settle(c)

	// verify snap has IgnoreValidation set
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(11))

	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(12),
	}
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(11),
			TrackingChannel:  "latest/stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 11),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(12),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Channel:      "latest/stable",
			Flags:        store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(tts[0])

	s.settle(c)

	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(12))

	// reset ignore validation
	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(11),
	}
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes
	flags = snapstate.Flags{}
	ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(12),
			TrackingChannel:  "latest/stable",
			IgnoreValidation: true,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 12),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Channel:      "latest/stable",
			Flags:        store.SnapActionEnforceValidation,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.settle(c)

	snapst = snapstate.SnapState{}
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, false)
	c.Check(snapst.Current, Equals, snap.R(11))
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateIgnoreValidationSticky(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:     si.Revision,
		SnapType:    "app",
		InstanceKey: "instance",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshesFail := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 2)
		if len(ignoreValidation) == 0 {
			return nil, validateErr
		}
		c.Check(ignoreValidation, DeepEquals, map[string]bool{
			"some-snap_instance": true,
		})
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshesFail

	flags := snapstate.Flags{IgnoreValidation: true}
	ts, err := snapstate.Update(s.state, "some-snap_instance", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, flags)
	c.Assert(err, IsNil)

	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}, {
			InstanceName:     "some-snap_instance",
			SnapID:           "some-snap-id",
			Revision:         snap.R(7),
			IgnoreValidation: false,
			RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:            snap.E("1*"),
		}},
		userID: 1,
	})
	c.Check(s.fakeBackend.ops[1], DeepEquals, fakeOp{
		op:    "storesvc-snap-action:action",
		revno: snap.R(11),
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap_instance",
			SnapID:       "some-snap-id",
			Channel:      "stable",
			Flags:        store.SnapActionIgnoreValidation,
		},
		userID: 1,
	})

	chg := s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(ts)

	s.settle(c)

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap 'instance' has IgnoreValidation set and the snap was
	// updated
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(11))
	// and the other snap does not
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Current, Equals, snap.R(7))
	c.Check(snapst.IgnoreValidation, Equals, false)

	s.fakeBackend.ops = nil
	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"some-snap-id": snap.R(12),
	}
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-snap_instance"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})

	chg = s.state.NewChange("refresh", "refresh snaps")
	for _, ts := range tts[:len(tts)-1] {
		chg.AddAll(ts)
	}

	s.settle(c)

	// ensure all our tasks ran
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, false)
	c.Check(snapst.Current, Equals, snap.R(12))

	err = snapstate.Get(s.state, "some-snap_instance", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.IgnoreValidation, Equals, true)
	c.Check(snapst.Current, Equals, snap.R(12))

	for i := 0; i < 2; i++ {
		op := s.fakeBackend.ops[i]
		switch op.op {
		case "storesvc-snap-action":
			c.Check(op, DeepEquals, fakeOp{
				op: "storesvc-snap-action",
				curSnaps: []store.CurrentSnap{{
					InstanceName:     "some-snap",
					SnapID:           "some-snap-id",
					Revision:         snap.R(7),
					IgnoreValidation: false,
					RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 7),
					Epoch:            snap.E("1*"),
				}, {
					InstanceName:     "some-snap_instance",
					SnapID:           "some-snap-id",
					Revision:         snap.R(11),
					TrackingChannel:  "latest/stable",
					IgnoreValidation: true,
					RefreshedDate:    fakeRevDateEpoch.AddDate(0, 0, 11),
					Epoch:            snap.E("1*"),
				}},
				userID: 1,
			})
		case "storesvc-snap-action:action":
			switch op.action.InstanceName {
			case "some-snap":
				c.Check(op, DeepEquals, fakeOp{
					op:    "storesvc-snap-action:action",
					revno: snap.R(12),
					action: store.SnapAction{
						Action:       "refresh",
						InstanceName: "some-snap",
						SnapID:       "some-snap-id",
						Flags:        0,
					},
					userID: 1,
				})
			case "some-snap_instance":
				c.Check(op, DeepEquals, fakeOp{
					op:    "storesvc-snap-action:action",
					revno: snap.R(12),
					action: store.SnapAction{
						Action:       "refresh",
						InstanceName: "some-snap_instance",
						SnapID:       "some-snap-id",
						Channel:      "latest/stable",
						Flags:        store.SnapActionIgnoreValidation,
					},
					userID: 1,
				})
			default:
				c.Fatalf("unexpected instance name %q", op.action.InstanceName)
			}
		default:
			c.Fatalf("unexpected action %q", op.op)
		}
	}

}

func (s *snapmgrTestSuite) TestUpdateFromLocal(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrLocalSnap)
}

func (s *snapmgrTestSuite) TestUpdateAmend(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 0, ts)

	// ensure we go from local to store revision-7
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()
	c.Check(tasks[1].Kind(), Equals, "download-snap")
	err = tasks[1].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestUpdateAmendToLocalRevWithoutFlag(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x1"),
	}

	otherSI := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R("x2"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			&si,
			&otherSI,
		}),
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{
		Revision: otherSI.Revision,
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh|localRevision, 0, ts)

	// ensure we go from local to local revision x2
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()
	c.Check(tasks[1].Kind(), Equals, "prepare-snap")
	err = tasks[1].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, otherSI.Revision)
}

func (s *snapmgrTestSuite) TestUpdateAmendSnapNotFound(c *C) {
	si := snap.SideInfo{
		RealName: "snap-unknown",
		Revision: snap.R("x1"),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snap-unknown", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "latest/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "snap-unknown", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, Equals, store.ErrSnapNotFound)
}

func (s *snapmgrTestSuite) TestSingleUpdateBlockedRevision(c *C) {
	// single updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
		Current:  si7.Revision,
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:         snap.E("1*"),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestMultiUpdateBlockedRevision(c *C) {
	// multi-updates should *not* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
		Current:  si7.Revision,
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Epoch:         snap.E("1*"),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestAllUpdateBlockedRevision(c *C) {
	//  update-all *should* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
		Current:  si7.Revision,
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
	c.Check(err, IsNil)
	c.Check(updates, HasLen, 0)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Block:         []snap.Revision{snap.R(11)},
			Epoch:         snap.E("1*"),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestAllUpdateRevisionNotBlocked(c *C) {
	//  update-all *should* set the block list
	si7 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	si11 := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si7, &si11}),
		Current:  si7.Revision,
		RevertStatus: map[int]snapstate.RevertStatus{
			si7.Revision.N: snapstate.NotBlocked,
		},
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
	c.Check(err, IsNil)
	c.Check(updates, HasLen, 0)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(7),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 7),
			Block:         []snap.Revision{snap.R(11)},
			Epoch:         snap.E("1*"),
		}},
		userID: 1,
	})
}

func (s *snapmgrTestSuite) TestUpdateManyPartialFailureCheckRerefreshDone(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	makeTestRefreshConfig(s.state)

	var someSnapValidation bool

	// override validate-snap handler set by AddForeignTaskHandlers.
	s.o.TaskRunner().AddHandler("validate-snap", func(t *state.Task, _ *tomb.Tomb) error {
		t.State().Lock()
		defer t.State().Unlock()
		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)
		if snapsup.SnapName() == "some-snap" {
			someSnapValidation = true
			return fmt.Errorf("boom")
		}
		return nil
	}, nil)

	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 2)
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	s.state.Unlock()
	s.snapmgr.Ensure()
	s.state.Lock()

	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Check(chg.Kind(), Equals, "auto-refresh")
	c.Check(chg.IsReady(), Equals, false)
	s.verifyRefreshLast(c)

	checkIsAutoRefresh(c, chg.Tasks(), true)

	s.settle(c)

	// not updated
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "some-snap", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.Revision{N: 1})

	// updated
	c.Assert(snapstate.Get(s.state, "some-other-snap", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.Revision{N: 11})

	c.Assert(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n.*Fetch and check assertions for snap \"some-snap\" \\(11\\) \\(boom\\)")
	c.Assert(chg.IsReady(), Equals, true)

	// check-rerefresh is last
	tasks := chg.Tasks()
	checkRerefresh := tasks[len(tasks)-1]
	c.Check(checkRerefresh.Kind(), Equals, "check-rerefresh")
	c.Check(checkRerefresh.Status(), Equals, state.DoneStatus)

	// validity
	c.Check(someSnapValidation, Equals, true)
}

var orthogonalAutoAliasesScenarios = []struct {
	aliasesBefore map[string][]string
	names         []string
	prune         []string
	update        bool
	new           bool
}{
	{nil, nil, nil, true, true},
	{nil, []string{"some-snap"}, nil, true, false},
	{nil, []string{"other-snap"}, nil, false, true},
	{map[string][]string{"some-snap": {"aliasA", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, nil, []string{"other-snap"}, true, false},
	{map[string][]string{"other-snap": {"aliasB", "aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasC"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasC"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, nil, []string{"other-snap", "some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}, "other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap", "some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, nil, []string{"some-snap"}, true, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"other-snap"}, []string{"some-snap"}, false, true},
	{map[string][]string{"some-snap": {"aliasB"}}, []string{"some-snap"}, nil, true, false},
	{map[string][]string{"other-snap": {"aliasA"}}, nil, []string{"other-snap"}, true, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"other-snap"}, []string{"other-snap"}, false, true},
	{map[string][]string{"other-snap": {"aliasA"}}, []string{"some-snap"}, []string{"other-snap"}, true, false},
}

func (s *snapmgrTestSuite) TestUpdateManyAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.fakeBackend.addSnapApp("some-snap", "cmdA")
	s.fakeBackend.addSnapApp("other-snap", "cmdB")

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.InstanceName() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedSet := func(aliases []string) map[string]bool {
		res := make(map[string]bool, len(aliases))
		for _, alias := range aliases {
			res[alias] = true
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		for _, instanceName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, instanceName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[instanceName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, instanceName, &snapst)
		}

		updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, scenario.names, nil, s.user.ID, nil)
		c.Check(err, IsNil)
		if scenario.update {
			verifyLastTasksetIsReRefresh(c, tts)
		}

		_, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0
		expectedUpdatesSet := make(map[string]bool)
		var expectedPruned map[string]map[string]bool
		var pruneTs *state.TaskSet
		if len(scenario.prune) != 0 {
			pruneTs = tts[0]
			j++
			taskAliases := make(map[string]map[string]bool)
			for _, aliasTask := range pruneTs.Tasks() {
				c.Check(aliasTask.Kind(), Equals, "prune-auto-aliases")
				var aliases []string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.InstanceName()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, instanceName := range scenario.prune {
				expectedPruned[instanceName] = expectedSet(dropped[instanceName])
				if instanceName == "other-snap" && !scenario.new && !scenario.update {
					expectedUpdatesSet["other-snap"] = true
				}
			}
			c.Check(taskAliases, DeepEquals, expectedPruned)
		}
		if scenario.update {
			updateTs := tts[j]
			j++
			expectedUpdatesSet["some-snap"] = true
			first := updateTs.Tasks()[0]
			c.Check(first.Kind(), Equals, "prerequisites")
			wait := false
			if expectedPruned["other-snap"]["aliasA"] {
				wait = true
			} else if expectedPruned["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, pruneTs.Tasks())
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			newTs := tts[j]
			j++
			expectedUpdatesSet["other-snap"] = true
			tasks := newTs.Tasks()
			c.Check(tasks, HasLen, 1)
			aliasTask := tasks[0]
			c.Check(aliasTask.Kind(), Equals, "refresh-aliases")

			wait := false
			if expectedPruned["some-snap"]["aliasB"] {
				wait = true
			} else if expectedPruned["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, pruneTs.Tasks())
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		l := len(tts)
		if scenario.update {
			l--
		}
		c.Assert(j, Equals, l, Commentf("%#v", scenario))

		// check reported updated names
		c.Check(len(updates) > 0, Equals, true)
		sort.Strings(updates)
		expectedUpdates := make([]string, 0, len(expectedUpdatesSet))
		for x := range expectedUpdatesSet {
			expectedUpdates = append(expectedUpdates, x)
		}
		sort.Strings(expectedUpdates)
		c.Check(updates, DeepEquals, expectedUpdates)
	}
}

func (s *snapmgrTestSuite) TestUpdateOneAutoAliasesScenarios(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.fakeBackend.addSnapApp("some-snap", "cmdA")
	s.fakeBackend.addSnapApp("other-snap", "cmdB")
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		switch info.InstanceName() {
		case "some-snap":
			return map[string]string{"aliasA": "cmdA"}, nil
		case "other-snap":
			return map[string]string{"aliasB": "cmdB"}, nil
		}
		return nil, nil
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	expectedSet := func(aliases []string) map[string]bool {
		res := make(map[string]bool, len(aliases))
		for _, alias := range aliases {
			res[alias] = true
		}
		return res
	}

	for _, scenario := range orthogonalAutoAliasesScenarios {
		if len(scenario.names) != 1 {
			continue
		}

		for _, instanceName := range []string{"some-snap", "other-snap"} {
			var snapst snapstate.SnapState
			err := snapstate.Get(s.state, instanceName, &snapst)
			c.Assert(err, IsNil)
			snapst.Aliases = nil
			snapst.AutoAliasesDisabled = false
			if autoAliases := scenario.aliasesBefore[instanceName]; autoAliases != nil {
				targets := make(map[string]*snapstate.AliasTarget)
				for _, alias := range autoAliases {
					targets[alias] = &snapstate.AliasTarget{Auto: "cmd" + alias[len(alias)-1:]}
				}

				snapst.Aliases = targets
			}
			snapstate.Set(s.state, instanceName, &snapst)
		}

		ts, err := snapstate.Update(s.state, scenario.names[0], nil, s.user.ID, snapstate.Flags{})
		c.Assert(err, IsNil)
		_, dropped, err := snapstate.AutoAliasesDelta(s.state, []string{"some-snap", "other-snap"})
		c.Assert(err, IsNil)

		j := 0

		tasks := ts.Tasks()
		// make sure the last task from Update is the rerefresh
		if scenario.update {
			reRefresh := tasks[len(tasks)-1]
			c.Check(reRefresh.Kind(), Equals, "check-rerefresh")
			// nothing should wait on it
			c.Check(reRefresh.NumHaltTasks(), Equals, 0)
			tasks = tasks[:len(tasks)-1] // and now forget about it
		}

		var expectedPruned map[string]map[string]bool
		var pruneTasks []*state.Task
		if len(scenario.prune) != 0 {
			nprune := len(scenario.prune)
			pruneTasks = tasks[:nprune]
			j += nprune
			taskAliases := make(map[string]map[string]bool)
			for _, aliasTask := range pruneTasks {
				c.Check(aliasTask.Kind(), Equals, "prune-auto-aliases")
				var aliases []string
				err := aliasTask.Get("aliases", &aliases)
				c.Assert(err, IsNil)
				snapsup, err := snapstate.TaskSnapSetup(aliasTask)
				c.Assert(err, IsNil)
				taskAliases[snapsup.InstanceName()] = expectedSet(aliases)
			}
			expectedPruned = make(map[string]map[string]bool)
			for _, instanceName := range scenario.prune {
				expectedPruned[instanceName] = expectedSet(dropped[instanceName])
			}
			c.Check(taskAliases, DeepEquals, expectedPruned)
		}
		if scenario.update {
			first := tasks[j]
			j += 19
			c.Check(first.Kind(), Equals, "prerequisites")
			wait := false
			if expectedPruned["other-snap"]["aliasA"] {
				wait = true
			} else if expectedPruned["some-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(first.WaitTasks(), DeepEquals, pruneTasks)
			} else {
				c.Check(first.WaitTasks(), HasLen, 0)
			}
		}
		if scenario.new {
			aliasTask := tasks[j]
			j++
			c.Check(aliasTask.Kind(), Equals, "refresh-aliases")
			wait := false
			if expectedPruned["some-snap"]["aliasB"] {
				wait = true
			} else if expectedPruned["other-snap"] != nil {
				wait = true
			}
			if wait {
				c.Check(aliasTask.WaitTasks(), DeepEquals, pruneTasks)
			} else {
				c.Check(aliasTask.WaitTasks(), HasLen, 0)
			}
		}
		c.Assert(len(tasks), Equals, j, Commentf("%#v", scenario))

		// conflict checks are triggered
		chg := s.state.NewChange("update", "...")
		chg.AddAll(ts)
		err = snapstate.CheckChangeConflict(s.state, scenario.names[0], nil)
		c.Check(err, ErrorMatches, `.* has "update" change in progress`)
		chg.SetStatus(state.DoneStatus)
	}
}

func (s *snapmgrTestSuite) TestUpdateLocalSnapFails(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrLocalSnap)
}

func (s *snapmgrTestSuite) TestUpdateDisabledUnsupported(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:  si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `refreshing disabled snap "some-snap" not supported`)
}

func (s *snapmgrTestSuite) TestUpdateKernelTrackChecksSwitchingTracks(c *C) {
	si := snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithKernelTrack("18"))
	defer r()
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	// switching tracks is not ok
	_, err := snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "new-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot switch from kernel track "18" as specified for the \(device\) model to "new-channel"`)

	// no change to the channel is ok
	_, err = snapstate.Update(s.state, "kernel", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching risk level is ok
	_, err = snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "18/beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching just risk within the pinned track is ok
	_, err = snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateGadgetTrackChecksSwitchingTracks(c *C) {
	si := snap.SideInfo{
		RealName: "brand-gadget",
		SnapID:   "brand-gadget-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithGadgetTrack("18"))
	defer r()
	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	// switching tracks is not ok
	_, err := snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "new-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot switch from gadget track "18" as specified for the \(device\) model to "new-channel"`)

	// no change to the channel is ok
	_, err = snapstate.Update(s.state, "brand-gadget", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching risk level is ok
	_, err = snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "18/beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// switching just risk within the pinned track is ok
	_, err = snapstate.Update(s.state, "brand-gadget", &snapstate.RevisionOptions{Channel: "beta"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextSameRevisionSwitchesChannel(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	prqt := new(testPrereqTracker)

	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{}, prqt, nil, "")
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")

	c.Assert(prqt.infos, HasLen, 1)
	c.Check(prqt.infos[0].SnapName(), Equals, "some-snap")
	c.Check(prqt.missingProviderContentTagsCalls, Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	validateCalled := false
	happyValidateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx1 snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(deviceCtx1, Equals, deviceCtx)
		validateCalled = true
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = happyValidateRefreshes

	prqt := new(testPrereqTracker)

	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{}, prqt, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 0, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)

	c.Assert(prqt.infos, HasLen, 1)
	c.Check(prqt.infos[0].SnapName(), Equals, "some-snap")
	c.Check(prqt.missingProviderContentTagsCalls, Equals, 1)
}

type testPrereqTracker struct {
	infos                           []*snap.Info
	missingProviderContentTagsCalls int
}

func (prqt *testPrereqTracker) Add(info *snap.Info) {
	prqt.infos = append(prqt.infos, info)
}

func (prqt *testPrereqTracker) MissingProviderContentTags(*snap.Info, snap.InterfaceRepo) map[string][]string {
	prqt.missingProviderContentTagsCalls++
	return nil
}

func (s *snapmgrTestSuite) TestUpdatePathWithDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(8)}
	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1*
`)
	prqt := new(testPrereqTracker)

	ts, err := snapstate.UpdatePathWithDeviceContext(s.state, si, mockSnap, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{}, prqt, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh|localSnap, 0, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(prqt.infos, HasLen, 1)
	c.Check(prqt.infos[0].SnapName(), Equals, "some-snap")
	c.Check(prqt.missingProviderContentTagsCalls, Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdatePathWithDeviceContextSwitchChannel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(7)}
	mockSnap := makeTestSnap(c, `name: some-snap
version: 1.0
epoch: 1*
`)

	ts, err := snapstate.UpdatePathWithDeviceContext(s.state, si, mockSnap, "some-snap", &snapstate.RevisionOptions{Channel: "22/edge"}, s.user.ID, snapstate.Flags{}, nil, deviceCtx, "")
	c.Assert(err, IsNil)
	c.Check(ts.Tasks(), HasLen, 1)
	c.Check(ts.Tasks()[0].Kind(), Equals, "switch-snap-channel")
}

func (s *snapmgrTestSuite) TestUpdatePathWithDeviceContextBadFile(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(8)}
	path := filepath.Join(c.MkDir(), "some-snap_8.snap")
	err := os.WriteFile(path, []byte(""), 0644)
	c.Assert(err, IsNil)

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	ts, err := snapstate.UpdatePathWithDeviceContext(s.state, si, path, "some-snap", opts, s.user.ID, snapstate.Flags{}, nil, deviceCtx, "")

	c.Assert(err, ErrorMatches, `cannot open snap file: cannot process snap or snapdir: cannot read ".*/some-snap_8.snap": EOF`)
	c.Assert(ts, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextToRevision(c *C) {
	const channel = ""
	revision := snap.R(11)
	s.testUpdateWithDeviceContext(c, revision, channel)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextToRevisionWithChannel(c *C) {
	const channel = "some-channel"
	revision := snap.R(11)
	s.testUpdateWithDeviceContext(c, revision, channel)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextDefaultsToTracked(c *C) {
	const channel = ""
	revision := snap.R(0)
	s.testUpdateWithDeviceContext(c, revision, channel)
}

func (s *snapmgrTestSuite) testUpdateWithDeviceContext(c *C, revision snap.Revision, channel string) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	const trackedChannel = "tracked-channel/stable"
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: "some-snap",
				Revision: snap.R(5),
				SnapID:   "some-snap-id",
			},
		}),
		TrackingChannel: trackedChannel,
		Current:         snap.R(5),
		SnapType:        "app",
		UserID:          1,
	})

	opts := &snapstate.RevisionOptions{Channel: channel, Revision: revision}
	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", opts, 0, snapstate.Flags{}, nil, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 0, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	enforceValidationSets := store.SnapActionFlags(0)
	if revision.Unset() {
		enforceValidationSets = store.SnapActionEnforceValidation
	}

	expectedChannel := channel
	if revision.Unset() && channel == "" {
		expectedChannel = trackedChannel
	}

	for _, op := range s.fakeBackend.ops {
		if op.op == "storesvc-snap-action:action" {
			c.Check(op, DeepEquals, fakeOp{
				op: "storesvc-snap-action:action",
				action: store.SnapAction{
					Action:       "refresh",
					InstanceName: "some-snap",
					SnapID:       "some-snap-id",
					Revision:     revision,
					Channel:      expectedChannel,
					Flags:        enforceValidationSets,
				},
				userID: 1,
				revno:  snap.R(11),
			})
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateTasksCoreSetsIgnoreOnConfigure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "os",
	})

	oldConfigure := snapstate.Configure
	defer func() { snapstate.Configure = oldConfigure }()

	var configureFlags int
	snapstate.Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
		configureFlags = flags
		return state.NewTaskSet()
	}

	_, err := snapstate.Update(s.state, "core", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure the core snap sets the "ignore-hook-error" flag
	c.Check(configureFlags&snapstate.IgnoreHookError, Equals, 1)
}

func (s *snapmgrTestSuite) TestUpdateDevModeConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-devmode/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// updated snap is devmode, refresh without --devmode, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires devmode or confinement override`)

	// updated snap is devmode, refresh with --devmode
	_, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateClassicConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap-now-classic", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap-now-classic", SnapID: "some-snap-now-classic-id", Revision: snap.R(7)}}),
		Current:  snap.R(7),
		SnapType: "app",
	})

	// updated snap is classic, refresh without --classic, do nothing
	// TODO: better error message here
	_, err := snapstate.Update(s.state, "some-snap-now-classic", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `.* requires classic confinement`)

	// updated snap is classic, refresh with --classic
	ts, err := snapstate.Update(s.state, "some-snap-now-classic", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap-now-classic", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateClassicFromClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update needs classic, refresh with --classic works
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// devmode overrides the snapsetup classic flag
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{DevMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode overrides it too (you need to provide both)
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{JailMode: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, false)

	// jailmode and classic together gets you both
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{JailMode: true, Classic: true})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	// snap installed with --classic, update needs classic, refresh without --classic works
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)
	snapsup, err = snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags.Classic, Equals, true)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.settle(c)

	// verify snap is in classic
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.Classic, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateStrictFromClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap-was-classic", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap-was-classic", SnapID: "some-snap-was-classic-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with --classic, update does not need classic, refresh works without --classic
	_, err := snapstate.Update(s.state, "some-snap-was-classic", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// snap installed with --classic, update does not need classic, refresh works with --classic
	_, err = snapstate.Update(s.state, "some-snap-was-classic", nil, s.user.ID, snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateChannelFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.Channel, Equals, "latest/edge")
}

func (s *snapmgrTestSuite) TestUpdateTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:  snap.R(7),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *snapmgrTestSuite) TestUpdateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:  snap.R(7),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	// need a change to make the tasks visible
	s.state.NewChange("refresh", "...").AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has "refresh" change in progress`)
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.testUpdateCreatesGCTasks(c, 2)
}

func (s *snapmgrTestSuite) TestUpdateCreatesGCTasksOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.testUpdateCreatesGCTasks(c, 3)
}

func (s *snapmgrTestSuite) testUpdateCreatesGCTasks(c *C, expectedDiscards int) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, expectedDiscards, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateCreatesDiscardAfterCurrentTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, snap.TypeApp, doesReRefresh, 3, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateManyTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:  snap.R(7),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *snapmgrTestSuite) TestUpdateMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	ts := tts[0]
	verifyUpdateTasks(c, snap.TypeApp, 0, 3, ts)

	// check that the tasks are in non-default lane
	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{1})
	}
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks())+1) // 1==rerefresh

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	checkIsAutoRefresh(c, ts.Tasks(), false)
}

func (s *snapmgrTestSuite) TestUpdateManyIgnoreRunning(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
	}
	snaptest.MockSnap(c, `name: some-snap`, &si)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"some-snap"}, nil, 0, &snapstate.Flags{IgnoreRunning: true})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Assert(updates, HasLen, 1)

	snapsup, err := snapstate.TaskSnapSetup(tts[0].Tasks()[0])
	c.Assert(err, IsNil)
	c.Assert(snapsup.IgnoreRunning, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyTransactionally(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
	}
	snaptest.MockSnap(c, `name: some-snap`, &si)
	si2 := snap.SideInfo{
		RealName: "some-other-snap",
		Revision: snap.R(1),
		SnapID:   "some-other-snap-id",
	}
	snaptest.MockSnap(c, `name: some-other-snap`, &si2)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si2}),
		Current:         si2.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"some-snap", "some-other-snap"}, nil, 0,
		&snapstate.Flags{Transaction: client.TransactionAllSnaps})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Assert(updates, HasLen, 2)

	// Last task is re-refresh, so it is a different lane
	for _, ts := range tts[:len(tts)-1] {
		checkIsAutoRefresh(c, ts.Tasks(), false)
		// check that tasksets are all in one lane
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{1})
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateManyTransactionallyFails(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// trigger download error on one of the snaps
	s.fakeStore.downloadError["some-other-snap"] = fmt.Errorf("boom")

	snapstate.ReplaceStore(s.state,
		contentStore{fakeStore: s.fakeStore, state: s.state})

	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "some-other-snap",
			SnapID:   "some-other-snap-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh some snaps")
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"some-snap", "some-other-snap"}, nil, 0,
		&snapstate.Flags{Transaction: client.TransactionAllSnaps})
	c.Assert(err, IsNil)
	c.Check(updated, testutil.DeepUnsortedMatches,
		[]string{"some-snap", "some-other-snap"})
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	s.settle(c)

	// content consumer snap fails to download
	c.Assert(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n.*Download snap \"some-other-snap\" \\(11\\) from channel \"latest/stable\" \\(boom\\).*")
	c.Assert(chg.IsReady(), Equals, true)

	var snapSt snapstate.SnapState
	// some-other-snap not updated due to download failure
	c.Assert(snapstate.Get(s.state, "some-other-snap", &snapSt), IsNil)
	c.Check(snapSt.Current, Equals, snap.R(1))

	// some-snap not updated either as this is a transactional refresh
	// (on update revision is 11)
	c.Assert(snapstate.Get(s.state, "some-snap", &snapSt), IsNil)
	c.Check(snapSt.Current, Equals, snap.R(7))
}

func (s *snapmgrTestSuite) TestUpdateManyFailureDoesntUndoSnapdRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-base", "snapd"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 4)
	c.Assert(updates, HasLen, 3)

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// refresh of some-snap fails on link-snap
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.settle(c)

	c.Check(chg.Err(), ErrorMatches, ".*cannot perform the following tasks:\n- Make snap \"some-snap\" \\(11\\) available to the system.*")
	c.Check(chg.IsReady(), Equals, true)

	var snapst snapstate.SnapState

	// failed snap remains at the old revision, snapd and some-base are refreshed.
	c.Assert(snapstate.Get(s.state, "some-snap", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.Revision{N: 1})

	c.Assert(snapstate.Get(s.state, "snapd", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.Revision{N: 11})

	c.Assert(snapstate.Get(s.state, "some-base", &snapst), IsNil)
	c.Check(snapst.Current, Equals, snap.Revision{N: 11})

	var undoneDownloads, doneDownloads int
	for _, ts := range tts {
		for _, t := range ts.Tasks() {
			if t.Kind() == "download-snap" {
				sup, err := snapstate.TaskSnapSetup(t)
				c.Assert(err, IsNil)
				switch sup.SnapName() {
				case "some-snap":
					undoneDownloads++
					c.Check(t.Status(), Equals, state.UndoneStatus)
				case "snapd", "some-base":
					doneDownloads++
					c.Check(t.Status(), Equals, state.DoneStatus)
				default:
					c.Errorf("unexpected snap %s", sup.SnapName())
				}
			}
		}
	}
	c.Assert(undoneDownloads, Equals, 1)
	c.Assert(doneDownloads, Equals, 2)
}

func (s *snapmgrTestSuite) TestUpdateManyDevModeConfinementFiltering(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-devmode/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// updated snap is devmode, updatemany doesn't update it
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassicConfinementFiltering(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// if a snap installed without --classic gets a classic update it isn't installed
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	// FIXME: UpdateMany will not error out in this case (daemon catches this case, with a weird error)
	c.Assert(tts, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyClassic(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-classic/stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
}

func (s *snapmgrTestSuite) TestUpdateManyClassicToStrict(c *C) {
	restore := maybeMockClassicSupport(c)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "stable",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, s.user.ID, &snapstate.Flags{Classic: true})
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	// ensure we clear the classic flag
	snapsup, err := snapstate.TaskSnapSetup(tts[0].Tasks()[0])
	c.Assert(err, IsNil)
	c.Assert(snapsup.Flags.Classic, Equals, false)

	verifyLastTasksetIsReRefresh(c, tts)
}

func (s *snapmgrTestSuite) TestUpdateManyDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateManyOneSwitchesChannel(c *C) {
	sideInfos := []snap.SideInfo{
		{
			RealName: "some-snap",
			Revision: snap.R(7),
			SnapID:   "some-snap-id",
		},
		{
			RealName: "some-other-snap",
			Revision: snap.R(1),
			SnapID:   "some-other-snap-id",
		},
	}

	s.state.Lock()
	defer s.state.Unlock()

	updates := make([]snapstate.StoreUpdate, 0, len(sideInfos))
	for _, si := range sideInfos {
		snaptest.MockSnap(c, fmt.Sprintf("name: %s", si.RealName), &si)
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
			Current:         si.Revision,
			SnapType:        "app",
			TrackingChannel: "latest/stable",
		})

		// for some-snap, we're going to switch to a different channel
		var channel string
		if si.RealName == "some-snap" {
			channel = "channel-for-7/stable"
		}

		updates = append(updates, snapstate.StoreUpdate{
			InstanceName: si.RealName,
			RevOpts: snapstate.RevisionOptions{
				Channel: channel,
			},
		})
	}

	goal := snapstate.StoreUpdateGoal(updates...)
	names, uts, err := snapstate.UpdateWithGoal(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)
	c.Assert(uts.Refresh, HasLen, 3)

	c.Assert(names, testutil.DeepUnsortedMatches, []string{"some-snap", "some-other-snap"})

	verifyLastTasksetIsReRefresh(c, uts.Refresh)
	c.Assert(uts.Refresh[1].Tasks(), HasLen, 1)

	switchTask := uts.Refresh[1].Tasks()[0]
	c.Check(switchTask.Kind(), Equals, "switch-snap-channel")
}

func (s *snapmgrTestSuite) TestUpdateManyOneSwitchesChannelWithAutoAlias(c *C) {
	sideInfos := []snap.SideInfo{
		{
			RealName: "alias-snap",
			Revision: snap.R(11),
			SnapID:   "alias-snap-id",
		},
		{
			RealName: "some-other-snap",
			Revision: snap.R(1),
			SnapID:   "some-other-snap-id",
		},
	}

	s.state.Lock()
	defer s.state.Unlock()

	n := 0
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		if info.InstanceName() == "alias-snap" {
			if n > 0 {
				return map[string]string{
					"alias1": "cmd1",
					"alias2": "cmd2",
				}, nil
			}
			n++
		}
		return nil, nil
	}

	updates := make([]snapstate.StoreUpdate, 0, len(sideInfos))
	for _, si := range sideInfos {
		snaptest.MockSnap(c, fmt.Sprintf("name: %s", si.RealName), &si)
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
			Current:         si.Revision,
			SnapType:        "app",
			TrackingChannel: "latest/stable",
		})

		// for alias-snap, we're going to switch to a different channel
		var channel string
		if si.RealName == "alias-snap" {
			channel = "latest/candidate"
		}

		updates = append(updates, snapstate.StoreUpdate{
			InstanceName: si.RealName,
			RevOpts: snapstate.RevisionOptions{
				Channel: channel,
			},
		})
	}

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "auto",
		},
	})

	s.state.Unlock()
	err := s.snapmgr.Ensure()
	s.state.Lock()
	c.Assert(err, IsNil)

	goal := snapstate.StoreUpdateGoal(updates...)
	names, uts, err := snapstate.UpdateWithGoal(context.Background(), s.state, goal, nil, snapstate.Options{})
	c.Assert(err, IsNil)
	// switch channel task set, refresh task set, auto alias task set, and
	// rerefresh task set
	c.Assert(uts.Refresh, HasLen, 4)
	c.Assert(names, testutil.DeepUnsortedMatches, []string{"alias-snap", "some-other-snap"})

	verifyLastTasksetIsReRefresh(c, uts.Refresh)

	switchTask := uts.Refresh[2].Tasks()[0]
	c.Assert(switchTask.Kind(), Equals, "switch-snap-channel")
	c.Assert(taskKinds(uts.Refresh[1].Tasks()), DeepEquals, []string{"refresh-aliases"})
}

func (s *snapmgrTestSuite) TestUpdateAllDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 0)
}

func taskSetsShareLane(tss ...*state.TaskSet) bool {
	lanes := make(map[int]int)
	for _, ts := range tss {
		// use a known task to read the lanes from, where expect
		// the task to be in a shared task-lane across the provided
		// task-sets
		for _, t := range ts.Tasks() {
			if t.Kind() == "link-snap" {
				for _, l := range t.Lanes() {
					lanes[l]++
				}
				break
			}
		}
	}
	// Now all of the lanes in the map should have the
	// value of the len(tss), as that would indicate that
	// link-snap tasks of each task-set have increased that lane.
	for _, c := range lanes {
		if c != len(tss) {
			return false
		}
	}
	return true
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC16(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "os",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core", "some-base"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 4)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, HasLen, 3)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// Some-snap is expected to wait for both the essential snap, but
	// also the base of some-snap. These dependencies are set up between
	// last tasks of core/some-base, on preprequisites
	lastTaskOfCore, err := tts[0].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := tts[1].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfSnap, err := tts[2].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	c.Check(firstTaskOfSnap.WaitTasks(), HasLen, 2)
	c.Check(firstTaskOfSnap.WaitTasks(), testutil.Contains, lastTaskOfCore)
	c.Check(firstTaskOfSnap.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// core and the other snaps are not expected to share the same lane
	c.Check(taskSetsShareLane(tts[0], tts[1]), Equals, false)
	c.Check(taskSetsShareLane(tts[0], tts[2]), Equals, false)
	c.Check(taskSetsShareLane(tts[1], tts[2]), Equals, false)

	// Manually verify their lanes
	c.Check(tts[0].Tasks()[0].Lanes(), DeepEquals, []int{1})
	c.Check(tts[1].Tasks()[0].Lanes(), DeepEquals, []int{2})
	c.Check(tts[2].Tasks()[0].Lanes(), DeepEquals, []int{3})
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC18(c *C) {
	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core18", "some-base", "snapd"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 5)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, HasLen, 4)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// Some-app will be waiting for the bases, which includes both some-base and
	// core18. The first task of some-snap will be waiting for the last tasks of
	// those two dependencies.
	lastTaskOfCore, err := tts[1].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := tts[2].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfSnap, err := tts[3].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	c.Check(firstTaskOfSnap.WaitTasks(), HasLen, 2)
	c.Check(firstTaskOfSnap.WaitTasks(), testutil.Contains, lastTaskOfCore)
	c.Check(firstTaskOfSnap.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Core18 and snapd are not expected to share the same lane, we only
	// check essential snaps as those are the ones that can end up in same lane.
	// Although snapd should never be a part of the transactional lane.
	c.Check(taskSetsShareLane(tts[0], tts[1]), Equals, false)

	// Manually verify the lanes of the initial task for the 4 task-sets
	c.Check(tts[0].Tasks()[0].Lanes(), DeepEquals, []int{1}) // snapd
	c.Check(tts[1].Tasks()[0].Lanes(), DeepEquals, []int{2}) // core18
	c.Check(tts[2].Tasks()[0].Lanes(), DeepEquals, []int{3}) // base
	c.Check(tts[3].Tasks()[0].Lanes(), DeepEquals, []int{4}) // snap
}

func (s *validationSetsSuite) TestUpdateManyWithRevisionOpts(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		// current validation set forbids "some-snap"
		vs := snapasserts.NewValidationSets()
		snapOne := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"required": "1",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "1", snapOne)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	// updating "some-snap" with revision opts should succeed because current
	// validation sets should be ignored
	revOpts := []*snapstate.RevisionOptions{{Revision: snap.R(2), ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/2"}}}
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, revOpts, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"some-snap"})

	chg := s.state.NewChange("refresh", "")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)
	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	validateCalled := false
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].InstanceName(), Equals, "some-snap")
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})
	verifyUpdateTasks(c, snap.TypeApp, 0, 0, tts[0])

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateMany(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		}),
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "instance",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	// ensure stable ordering of updates list
	if updates[0] != "some-snap" {
		updates[1], updates[0] = updates[0], updates[1]
	}

	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})

	var snapsup, snapsupInstance *snapstate.SnapSetup

	// ensure stable ordering of task sets list
	snapsup, err = snapstate.TaskSnapSetup(tts[0].Tasks()[0])
	c.Assert(err, IsNil)
	if snapsup.InstanceName() != "some-snap" {
		tts[0], tts[1] = tts[1], tts[0]
		snapsup, err = snapstate.TaskSnapSetup(tts[0].Tasks()[0])
		c.Assert(err, IsNil)
	}
	snapsupInstance, err = snapstate.TaskSnapSetup(tts[1].Tasks()[0])
	c.Assert(err, IsNil)

	c.Assert(snapsup.InstanceName(), Equals, "some-snap")
	c.Assert(snapsupInstance.InstanceName(), Equals, "some-snap_instance")

	verifyUpdateTasks(c, snap.TypeApp, 0, 3, tts[0])
	verifyUpdateTasks(c, snap.TypeApp, 0, 1, tts[1])
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:     snap.R(1),
		SnapType:    "app",
		InstanceKey: "instance",
	})

	validateCalled := false
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		validateCalled = true
		c.Check(refreshes, HasLen, 2)
		instanceIdx := 0
		someIdx := 1
		if refreshes[0].InstanceName() != "some-snap_instance" {
			instanceIdx = 1
			someIdx = 0
		}
		c.Check(refreshes[someIdx].InstanceName(), Equals, "some-snap")
		c.Check(refreshes[instanceIdx].InstanceName(), Equals, "some-snap_instance")
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(refreshes[1].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[1].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return refreshes, nil
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})
	verifyUpdateTasks(c, snap.TypeApp, 0, 0, tts[0])
	verifyUpdateTasks(c, snap.TypeApp, 0, 0, tts[1])

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshesUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current: snap.R(1),
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		c.Check(refreshes, HasLen, 1)
		c.Check(refreshes[0].SnapID, Equals, "some-snap-id")
		c.Check(refreshes[0].Revision, Equals, snap.R(11))
		c.Check(ignoreValidation, HasLen, 0)
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	// refresh all => no error
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

	// refresh some-snap => report error
	updates, tts, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, 0, nil)
	c.Assert(err, Equals, validateErr)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

}

func (s *snapmgrTestSuite) testUpdateManyDiskSpaceCheck(c *C, featureFlag, failDiskCheck, failInstallSize bool) error {
	var diskCheckCalled, installSizeCalled bool
	restore := snapstate.MockOsutilCheckFreeSpace(func(path string, sz uint64) error {
		diskCheckCalled = true
		c.Check(path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
		c.Check(sz, Equals, snapstate.SafetyMarginDiskSpace(123))
		if failDiskCheck {
			return &osutil.NotEnoughDiskSpaceError{}
		}
		return nil
	})
	defer restore()

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
		installSizeCalled = true
		if failInstallSize {
			return 0, fmt.Errorf("boom")
		}
		c.Assert(snaps, HasLen, 2)
		c.Check(snaps[0].InstanceName(), Equals, "snapd")
		c.Check(snaps[1].InstanceName(), Equals, "some-snap")
		return 123, nil
	})
	defer restoreInstallSize()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-refresh", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	if featureFlag {
		c.Check(installSizeCalled, Equals, true)
		if failInstallSize {
			c.Check(diskCheckCalled, Equals, false)
		} else {
			c.Check(diskCheckCalled, Equals, true)
			if failDiskCheck {
				c.Check(updates, HasLen, 0)
			} else {
				c.Check(updates, HasLen, 2)
			}
		}
	} else {
		c.Check(installSizeCalled, Equals, false)
		c.Check(diskCheckCalled, Equals, false)
	}

	return err
}

func (s *snapmgrTestSuite) TestUpdateManyDiskSpaceCheckError(c *C) {
	featureFlag := true
	failDiskCheck := true
	failInstallSize := false
	err := s.testUpdateManyDiskSpaceCheck(c, featureFlag, failDiskCheck, failInstallSize)
	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `insufficient space in .* to perform "refresh" change for the following snaps: snapd, some-snap`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"snapd", "some-snap"})
}

func (s *snapmgrTestSuite) TestUpdateManyDiskSpaceSkippedIfFeatureDisabled(c *C) {
	featureFlag := false
	failDiskCheck := true
	failInstallSize := false
	err := s.testUpdateManyDiskSpaceCheck(c, featureFlag, failDiskCheck, failInstallSize)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateManyDiskSpaceFailInstallSize(c *C) {
	featureFlag := true
	failDiskCheck := false
	failInstallSize := true
	err := s.testUpdateManyDiskSpaceCheck(c, featureFlag, failDiskCheck, failInstallSize)
	c.Assert(err, ErrorMatches, "boom")
}

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapMergedLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3", "svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestUnlinkCurrentSnapPassthroughLastActiveDisabledServicesSet(c *C) {
	si := snap.SideInfo{
		RealName: "services-snap",
		Revision: snap.R(-42),
	}
	snaptest.MockSnap(c, `name: services-snap`, &si)

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:                     true,
		Sequence:                   snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		Current:                    si.Revision,
		SnapType:                   "app",
		TrackingChannel:            "stable",
		LastActiveDisabledServices: []string{"missing-svc3"},
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})

	c.Assert(err, IsNil)
	// only add up to unlink-current-snap task
	for _, t := range ts.Tasks() {
		chg.AddTask(t)
		if t.Kind() == "unlink-current-snap" {
			// don't add any more from this point on
			break
		}
	}

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"missing-svc3"})
}

func (s *snapmgrTestSuite) TestStopSnapServicesSavesSnapSetupLastActiveDisabledServices(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "services-snap",
			Revision: snap.R(11),
			SnapID:   "services-snap-id",
		},
	}

	chg := s.state.NewChange("stop-services", "stop the services")
	t1 := s.state.NewTask("prerequisites", "...")
	t1.Set("snap-setup", snapsup)
	t2 := s.state.NewTask("stop-snap-services", "...")
	t2.Set("stop-reason", snap.StopReasonDisable)
	t2.Set("snap-setup-task", t1.ID())
	t2.WaitFor(t1)
	chg.AddTask(t1)
	chg.AddTask(t2)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "services-snap", &snapst), IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestStopSnapServicesFirstSavesSnapSetupLastActiveDisabledServices(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
		// leave this line to keep gofmt 1.10 happy
		LastActiveDisabledServices: []string{"svc2"},
	})

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "services-snap",
			Revision: snap.R(11),
			SnapID:   "services-snap-id",
		},
	}

	chg := s.state.NewChange("stop-services", "stop the services")
	t := s.state.NewTask("stop-snap-services", "...")
	t.Set("stop-reason", snap.StopReasonDisable)
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, "services-snap", &snapst), IsNil)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(snapst.LastActiveDisabledServices)
	c.Assert(snapst.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestRefreshDoesntRestoreRevisionConfig(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// set global configuration (affecting current snap)
	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "100")
	tr.Commit()

	// set per-revision config for the upcoming rev. 2, we don't expect it restored though
	// since only revert restores revision configs.
	s.state.Set("revision-config", map[string]interface{}{
		"some-snap": map[string]interface{}{
			"2": map[string]interface{}{"foo": "200"},
		},
	})

	// simulate a refresh to rev. 2
	chg := s.state.NewChange("update", "update some-snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(2)}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)

	// config of rev. 1 has been stored in per-revision map
	var cfgs map[string]interface{}
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{"foo": "100"},
		"2": map[string]interface{}{"foo": "200"},
	})

	// config of rev. 2 hasn't been restored by refresh, old value returned
	tr = config.NewTransaction(s.state)
	var res string
	c.Assert(tr.Get("some-snap", "foo", &res), IsNil)
	c.Assert(res, Equals, "100")
}

func (s *snapmgrTestSuite) TestUpdateContentProviderDownloadFailure(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// trigger download error on content provider
	s.fakeStore.downloadError["snap-content-slot"] = fmt.Errorf("boom")

	snapstate.ReplaceStore(s.state, contentStore{fakeStore: s.fakeStore, state: s.state})

	si := &snap.SideInfo{
		RealName: "snap-content-plug",
		SnapID:   "snap-content-plug-id",
		Revision: snap.R(7),
	}
	snaptest.MockSnap(c, `name: snap-content-plug`, si)
	snapstate.Set(s.state, "snap-content-plug", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "snap-content-slot", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "snap-content-slot",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(1),
		}}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updated, testutil.DeepUnsortedMatches, []string{"snap-content-plug", "snap-content-slot"})
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	s.settle(c)

	// content consumer snap fails to download
	c.Assert(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n.*Download snap \"snap-content-slot\" \\(11\\) from channel \"latest/stable\" \\(boom\\).*")
	c.Assert(chg.IsReady(), Equals, true)

	var snapSt snapstate.SnapState
	// content provider not updated due to download failure
	c.Assert(snapstate.Get(s.state, "snap-content-slot", &snapSt), IsNil)
	c.Check(snapSt.Current, Equals, snap.R(1))

	c.Assert(snapstate.Get(s.state, "snap-content-plug", &snapSt), IsNil)
	// but content consumer got updated to the new revision
	c.Check(snapSt.Current, Equals, snap.R(11))
}

func (s *snapmgrTestSuite) TestNoReRefreshInUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)

	// ensure we have no re-refresh task
	for _, t := range ts.Tasks() {
		c.Assert(t.Kind(), Not(Equals), "check-rerefresh")
	}

	snapsup, err := snapstate.TaskSnapSetup(ts.Tasks()[0])
	c.Assert(err, IsNil)
	// NoReRefresh is consumed and consulted when creating the taskset
	// but is not copied into SnapSetup
	c.Check(snapsup.Flags.NoReRefresh, Equals, false)
}

func (s *snapmgrTestSuite) TestEmptyUpdateWithChannelChangeAndAutoAlias(c *C) {
	// this reproduces the cause behind lp:1860324,
	// namely an empty refresh with a channel change on a snap
	// with changed aliases

	s.state.Lock()
	defer s.state.Unlock()

	n := 0
	snapstate.AutoAliases = func(st *state.State, info *snap.Info) (map[string]string, error) {
		if info.InstanceName() == "alias-snap" {
			if n > 0 {
				return map[string]string{
					"alias1": "cmd1",
					"alias2": "cmd2",
				}, nil
			}
			n++
		}
		return nil, nil
	}

	snapstate.Set(s.state, "alias-snap", &snapstate.SnapState{
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11), SnapID: "alias-snap-id"},
		}),
		Current: snap.R(11),
		Active:  true,
	})

	s.state.Set("aliases", map[string]map[string]string{
		"alias-snap": {
			"alias1": "auto",
		},
	})

	s.state.Unlock()
	err := s.snapmgr.Ensure()
	s.state.Lock()
	c.Assert(err, IsNil)

	ts, err := snapstate.Update(s.state, "alias-snap", &snapstate.RevisionOptions{Channel: "latest/candidate"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh snap")
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
}

func (s *snapmgrTestSuite) testUpdateDiskSpaceCheck(c *C, featureFlag, failInstallSize, failDiskCheck bool) error {
	restore := snapstate.MockOsutilCheckFreeSpace(func(path string, sz uint64) error {
		c.Check(sz, Equals, snapstate.SafetyMarginDiskSpace(123))
		if failDiskCheck {
			return &osutil.NotEnoughDiskSpaceError{}
		}
		return nil
	})
	defer restore()

	var installSizeCalled bool

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []snapstate.MinimalInstallInfo, userID int, prqt snapstate.PrereqTracker) (uint64, error) {
		installSizeCalled = true
		if failInstallSize {
			return 0, fmt.Errorf("boom")
		}
		c.Assert(snaps, HasLen, 1)
		c.Check(snaps[0].InstanceName(), Equals, "some-snap")
		return 123, nil
	})
	defer restoreInstallSize()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.check-disk-space-refresh", featureFlag)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	opts := &snapstate.RevisionOptions{Channel: "some-channel"}
	_, err := snapstate.Update(s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})

	if featureFlag {
		c.Check(installSizeCalled, Equals, true)
	} else {
		c.Check(installSizeCalled, Equals, false)
	}

	return err
}

func (s *snapmgrTestSuite) TestUpdateDiskSpaceError(c *C) {
	featureFlag := true
	failInstallSize := false
	failDiskCheck := true
	err := s.testUpdateDiskSpaceCheck(c, featureFlag, failInstallSize, failDiskCheck)
	diskSpaceErr := err.(*snapstate.InsufficientSpaceError)
	c.Assert(diskSpaceErr, ErrorMatches, `insufficient space in .* to perform "refresh" change for the following snaps: some-snap`)
	c.Check(diskSpaceErr.Path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd"))
	c.Check(diskSpaceErr.Snaps, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestUpdateDiskCheckSkippedIfDisabled(c *C) {
	featureFlag := false
	failInstallSize := false
	failDiskCheck := true
	err := s.testUpdateDiskSpaceCheck(c, featureFlag, failInstallSize, failDiskCheck)
	c.Check(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateDiskCheckInstallSizeError(c *C) {
	featureFlag := true
	failInstallSize := true
	failDiskCheck := false
	err := s.testUpdateDiskSpaceCheck(c, featureFlag, failInstallSize, failDiskCheck)
	c.Check(err, ErrorMatches, "boom")
}

func (s *snapmgrTestSuite) TestUpdateDiskCheckHappy(c *C) {
	featureFlag := true
	failInstallSize := false
	failDiskCheck := false
	err := s.testUpdateDiskSpaceCheck(c, featureFlag, failInstallSize, failDiskCheck)
	c.Check(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateSnapAndOutdatedPrereq(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	updateSnaps := []string{"outdated-consumer", "outdated-producer"}
	for _, snapName := range updateSnaps {
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
				RealName: snapName,
				SnapID:   fmt.Sprintf("%s-id", snapName),
				Revision: snap.R(1),
			}}),
			Current: snap.R(1),
			Active:  true,
		})
	}

	chg := s.state.NewChange("refresh-snap", "test: update snaps")
	updated, tss, err := snapstate.UpdateMany(context.Background(), s.state, updateSnaps, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tss, Not(HasLen), 0)
	c.Assert(updated, testutil.DeepUnsortedMatches, updateSnaps)

	for _, ts := range tss {
		chg.AddAll(ts)
	}
	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	c.Check(s.fakeStore.downloads, testutil.DeepUnsortedMatches, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "outdated-consumer", target: filepath.Join(dirs.SnapBlobDir, "outdated-consumer_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "outdated-producer", target: filepath.Join(dirs.SnapBlobDir, "outdated-producer_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdatePrereqDetectConflictWithPrereq(c *C) {
	s.state.Lock()

	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-producer",
			SnapID:   "outdated-producer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  false,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	enableTasks, err := snapstate.Enable(s.state, "outdated-producer")
	c.Assert(err, IsNil)
	c.Check(enableTasks, Not(HasLen), 0)
	enableChg := s.state.NewChange("enable-snap", "test: enable snap")
	enableChg.AddAll(enableTasks)

	// this update triggers an update of the producer which conflicts with the
	// 'Enable' op. This should be detected before it tries to update the producer
	updateTasks, err := snapstate.Update(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(updateTasks, Not(HasLen), 0)
	updateChg := s.state.NewChange("refresh-snap", "test: update snap")
	updateChg.AddAll(updateTasks)

	s.state.Unlock()
	_ = s.o.Settle(3 * time.Second)

	s.state.Lock()
	defer s.state.Unlock()

	prereqTask := findStrictlyOnePrereqTask(c, updateChg)

	// check that it's not done and that it was scheduled for a specific time
	// (only done when retrying). This doesn't test that it's scheduled for
	// sometime in the future to avoid race conditions on slower systems
	c.Check(prereqTask.Status(), Equals, state.DoingStatus)
	c.Assert(prereqTask.AtTime().IsZero(), Equals, false)
}

func (s *snapmgrTestSuite) TestUpdatePrereqWithConflictingTask(c *C) {
	s.state.Lock()

	prodInfo := &snap.SideInfo{
		RealName: "outdated-producer",
		SnapID:   "outdated-producer-id",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{prodInfo}),
		Current:  snap.R(1),
		Active:   true,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	// the Update op will conflict with this task and it should be retried
	chg := s.state.NewChange("test", "")
	task := s.state.NewTask("test", "")
	task.SetStatus(state.DoStatus)
	task.Set("snap-setup", &snapstate.SnapSetup{SideInfo: prodInfo})
	chg.AddTask(task)

	// the update of the producer should be scheduled but conflict in the Update call.
	// That should still result in the task being retried
	updateTasks, err := snapstate.Update(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(updateTasks, Not(HasLen), 0)
	updateChg := s.state.NewChange("refresh-snap", "test: update snap")
	updateChg.AddAll(updateTasks)

	s.state.Unlock()
	_ = s.o.Settle(3 * time.Second)

	s.state.Lock()
	defer s.state.Unlock()

	prereqTask := findStrictlyOnePrereqTask(c, updateChg)

	// check that it's not done and that it was scheduled for a specific time
	// (only done when retrying). This doesn't test that it's scheduled for
	// sometime in the future to avoid race conditions on slower systems
	c.Check(prereqTask.Status(), Equals, state.DoingStatus)
	c.Assert(prereqTask.AtTime().IsZero(), Equals, false)
}

func (s *snapmgrTestSuite) TestUpdateNoRetryIfPrereqTaskFails(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-producer",
			SnapID:   "outdated-producer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		// this will cause the update refresh to fail but the (prerequisites) task
		// shouldn't be retried
		Active: false,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	// the update of the producer should be attempted but fail and not be retried
	updateTasks, err := snapstate.Update(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(updateTasks, Not(HasLen), 0)
	updateChg := s.state.NewChange("refresh-snap", "test: update snap")
	updateChg.AddAll(updateTasks)

	s.settle(c)

	prereqTask := findStrictlyOnePrereqTask(c, updateChg)

	// check that the task is done and that it wasn't ever rescheduled for a
	// specific time (only done when retrying)
	c.Check(prereqTask.Status(), Equals, state.DoneStatus)
	c.Assert(prereqTask.AtTime().IsZero(), Equals, true)
}

func (s *snapmgrTestSuite) TestUpdatePrereqIgnoreDuplOpInSameChange(c *C) {
	s.state.Lock()

	prodInfo := &snap.SideInfo{
		RealName: "outdated-producer",
		SnapID:   "outdated-producer-id",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{prodInfo}),
		Current:  snap.R(1),
		Active:   true,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	chg := s.state.NewChange("refresh-snap", "test: update snap")

	// we inject a conflicting task to simulate a concurrent update
	// (same snap and same change) for determinism. Using UpdateMany
	// would create a race between the update operations
	confTask := s.state.NewTask("conflicting-task", "")
	confTask.Set("snap-setup", &snapstate.SnapSetup{SideInfo: prodInfo})
	chg.AddTask(confTask)

	updateTasks, err := snapstate.Update(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(updateTasks.Tasks(), Not(HasLen), 0)
	chg.AddAll(updateTasks)

	s.state.Unlock()
	// the tasks won't converge because the re-refresh waits for all tasks
	// in the change, including our 'conflicting-task'
	_ = s.o.Settle(3 * time.Second)

	s.state.Lock()
	defer s.state.Unlock()

	// check that the prereq task wasn't retried
	prereqTask := findStrictlyOnePrereqTask(c, chg)
	c.Check(prereqTask.Status(), Equals, state.DoneStatus)
	c.Assert(prereqTask.AtTime().IsZero(), Equals, true)
}

// looks for a 'prerequisites' task in the change and fails if more or less
// than one is found
func findStrictlyOnePrereqTask(c *C, chg *state.Change) *state.Task {
	var prereqTask *state.Task

	for _, task := range chg.Tasks() {
		if task.Kind() != "prerequisites" {
			continue
		}

		if prereqTask != nil {
			c.Fatalf("encountered two 'prerequisite' tasks in the change but only expected one: \n%s\n%s\n",
				task.Summary(), prereqTask.Summary())
		}

		prereqTask = task
	}

	c.Assert(prereqTask, NotNil)
	return prereqTask
}

func (s *validationSetsSuite) TestUpdateSnapRequiredByValidationSetAlreadyAtRequiredRevision(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "4",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "1", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	_, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `snap has no updates available`)
}

func (s *validationSetsSuite) TestUpdateSnapRequiredByValidationRefreshToRequiredRevision(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "11",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "1", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	// new snap revision from the store
	c.Check(snapsup.Revision(), Equals, snap.R(11))

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}}}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			Revision:       snap.R(11),
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/1"},
			Flags:          store.SnapActionEnforceValidation,
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateSnapRequiredByValidationSetAnyRevision(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		// no revision specified
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	// new snap revision from the store
	c.Check(snapsup.Revision(), Equals, snap.R(11))

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}}}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/2"},
			Flags:          store.SnapActionEnforceValidation,
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateToRevisionSnapRequiredByValidationSetAnyRevision(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		// no revision specified
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	// new snap revision from the store
	c.Check(snapsup.Revision(), Equals, snap.R(11))

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}}}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			Revision:       snap.R(11),
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/2"},
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateToRevisionSnapRequiredByValidationWithMatchingRevision(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "11",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	// new snap revision from the store
	c.Check(snapsup.Revision(), Equals, snap.R(11))
	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: fakeRevDateEpoch.AddDate(0, 0, 1),
		}},
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			Revision:       snap.R(11),
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/2"},
			// XXX: updateToRevisionInfo doesn't set store.SnapActionEnforceValidation flag?
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateToRevisionSnapRequiredByValidationAlreadyAtRevisionNoop(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "4",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	// revision 4 is already installed
	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(4),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(4)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(4))
	c.Assert(s.fakeBackend.ops, HasLen, 0)
}

func (s *validationSetsSuite) TestUpdateToRevisionSnapRequiredByValidationWrongRevisionError(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "5",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, ErrorMatches, `cannot update snap "some-snap" to revision 11 without --ignore-validation, revision 5 is required by validation sets: 16/foo/bar/2`)
}

// test that updating to a revision that is different than the revision required
// by a validation set is possible if --ignore-validation flag is passed.
func (s *validationSetsSuite) TestUpdateToWrongRevisionIgnoreValidation(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "5",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	// revision 1 is already installed; it doesn't match the required revision 5
	// but that's not relevant for the test (we could have installed it with
	// --ignore-validation before, and that's reflected by IgnoreValidation flag
	// in the snapstate).
	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
		Flags: snapstate.Flags{
			IgnoreValidation: true,
		},
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	// revision 5 is required and requesting revision 11 would fail
	// without --ignore-validation.
	revOpts := &snapstate.RevisionOptions{Revision: snap.R(11)}
	_, err := snapstate.Update(s.state, "some-snap", revOpts, 0, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(1),
			Epoch:            snap.E("1*"),
			RefreshedDate:    refreshedDate,
			IgnoreValidation: true,
		}},
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Revision:     snap.R(11),
			Flags:        store.SnapActionIgnoreValidation,
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateManyRequiredByValidationSetAlreadyAtCorrectRevisionNoop(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "5",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(5)},
		}),
		Current:  snap.R(5),
		SnapType: "app",
	})
	names, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(names, HasLen, 0)
	c.Assert(s.fakeBackend.ops, HasLen, 0)
}

func (s *validationSetsSuite) TestUpdateManyRequiredByValidationSetsCohortIgnored(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVs ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "5",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:    true,
		Sequence:  snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:   snap.R(1),
		SnapType:  "app",
		CohortKey: "cohortkey",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	names, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}},
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			Revision:       snap.R(5),
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/2"},
		},
		revno: snap.R(5),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateManyRequiredByValidationSetIgnoreValidation(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "5",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
		Flags: snapstate.Flags{
			IgnoreValidation: true,
		},
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)
	names, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, 0, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"some-snap"})

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:     "some-snap",
			SnapID:           "some-snap-id",
			Revision:         snap.R(1),
			Epoch:            snap.E("1*"),
			RefreshedDate:    refreshedDate,
			IgnoreValidation: true,
		}},
	}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Flags:        store.SnapActionIgnoreValidation,
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *validationSetsSuite) TestUpdateSnapRequiredByValidationSetAlreadyAtRequiredRevisionIgnoreValidationOK(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		someSnap := map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
			"revision": "4",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "1", someSnap)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		}),
		Current:  snap.R(4),
		SnapType: "app",
	})

	// this would normally fail since the snap is already installed at the required revision 4; will get
	// refreshed to revision 11.
	_, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)
	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOp := fakeOp{
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:       "refresh",
			InstanceName: "some-snap",
			SnapID:       "some-snap-id",
			Flags:        store.SnapActionIgnoreValidation,
		},
		revno: snap.R(11),
	}
	c.Assert(s.fakeBackend.ops[1], DeepEquals, expectedOp)
}

func (s *validationSetsSuite) TestUpdateToRevisionWithValidationSets(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, fmt.Errorf("unexpected")
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si)

	refreshedDate := fakeRevDateEpoch.AddDate(0, 0, 1)

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(11), ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar", "16/foo/baz"}}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	// new snap revision from the store
	c.Check(snapsup.Revision(), Equals, snap.R(11))

	c.Assert(s.fakeBackend.ops, HasLen, 2)
	expectedOps := fakeOps{{
		op: "storesvc-snap-action",
		curSnaps: []store.CurrentSnap{{
			InstanceName:  "some-snap",
			SnapID:        "some-snap-id",
			Revision:      snap.R(1),
			Epoch:         snap.E("1*"),
			RefreshedDate: refreshedDate,
		}}}, {
		op: "storesvc-snap-action:action",
		action: store.SnapAction{
			Action:         "refresh",
			InstanceName:   "some-snap",
			SnapID:         "some-snap-id",
			Revision:       snap.R(11),
			ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar", "16/foo/baz"},
		},
		revno: snap.R(11),
	}}
	c.Assert(s.fakeBackend.ops, DeepEquals, expectedOps)
}

func (s *snapmgrTestSuite) TestUpdatePrerequisiteWithSameDeviceContext(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-producer",
			SnapID:   "outdated-producer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		CtxStore: contentStore{
			fakeStore: s.fakeStore,
			state:     s.state,
		},
		DeviceModel: &asserts.Model{},
	}
	snapstatetest.MockDeviceContext(deviceCtx)

	ts, err := snapstate.UpdateWithDeviceContext(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{NoReRefresh: true}, nil, deviceCtx, "")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), Not(HasLen), 0)

	chg := s.state.NewChange("update", "test: update")
	chg.AddAll(ts)

	s.settle(c)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "outdated-consumer", target: filepath.Join(dirs.SnapBlobDir, "outdated-consumer_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "outdated-producer", target: filepath.Join(dirs.SnapBlobDir, "outdated-producer_11.snap")},
	})
}

func (s *validationSetsSuite) testUpdateManyValidationSetsPartialFailure(c *C) *state.Change {
	logbuf, rest := logger.MockLogger()
	defer rest()

	s.fakeStore.refreshRevnos = map[string]snap.Revision{
		"aaqKhntON3vR7kwEbVPsILm7bUViPDz":  snap.R(11),
		"bgtKhntON3vR7kwEbVPsILm7bUViPDzx": snap.R(11),
	}

	var enforcedValidationSetsCalls int
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vs := snapasserts.NewValidationSets()
		snap1 := map[string]interface{}{
			"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap",
			"presence": "required",
		}
		snap2 := map[string]interface{}{
			"id":       "bgtKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-other-snap",
			"presence": "required",
		}
		var sequence string
		if enforcedValidationSetsCalls == 0 {
			snap1["revision"] = "11"
			snap2["revision"] = "11"
			sequence = "3"
		} else {
			snap1["revision"] = "1"
			snap2["revision"] = "1"
			sequence = "2"
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", sequence, snap1, snap2)
		c.Assert(vs.Add(vsa1.(*asserts.ValidationSet)), IsNil)
		enforcedValidationSetsCalls++
		return vs, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap`, si1)

	si2 := &snap.SideInfo{RealName: "some-other-snap", SnapID: "bgtKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-other-snap`, si2)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-other-snap/11")

	names, tss, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{"some-other-snap", "some-snap"})
	c.Check(logbuf.String(), Equals, "")
	chg := s.state.NewChange("update", "")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	return chg
}

func (s *validationSetsSuite) TestUpdateManyValidationSetsPartialFailureNothingToRestore(c *C) {
	var refreshed []string
	restoreMaybeRestoreValidationSetsAndRevertSnaps := snapstate.MockMaybeRestoreValidationSetsAndRevertSnaps(func(st *state.State, refreshedSnaps []string, fromChange string) ([]*state.TaskSet, error) {
		refreshed = refreshedSnaps
		// nothing to restore
		return nil, nil
	})
	defer restoreMaybeRestoreValidationSetsAndRevertSnaps()

	var addCurrentTrackingToValidationSetsStackCalled int
	restoreAddCurrentTrackingToValidationSetsStack := snapstate.MockAddCurrentTrackingToValidationSetsStack(func(st *state.State) error {
		addCurrentTrackingToValidationSetsStackCalled++
		return nil
	})
	defer restoreAddCurrentTrackingToValidationSetsStack()

	s.testUpdateManyValidationSetsPartialFailure(c)

	// only some-snap was successfully refreshed, this also confirms that
	// mockMaybeRestoreValidationSetsAndRevertSnaps was called.
	c.Check(refreshed, DeepEquals, []string{"some-snap"})

	// validation sets history update was attempted (could be a no-op if
	// maybeRestoreValidationSetsAndRevertSnaps restored last tracking
	// data).
	c.Check(addCurrentTrackingToValidationSetsStackCalled, Equals, 1)
}

func (s *validationSetsSuite) TestUpdateManyValidationSetsPartialFailureRevertTasks(c *C) {
	restore := snapstate.MockRestoreValidationSetsTracking(func(st *state.State) error {
		tr := assertstate.ValidationSetTracking{
			AccountID: "foo",
			Name:      "bar",
			Mode:      assertstate.Enforce,
			Current:   2,
		}
		assertstate.UpdateValidationSet(s.state, &tr)
		return nil
	})
	defer restore()

	chg := s.testUpdateManyValidationSetsPartialFailure(c)

	s.state.Lock()
	defer s.state.Unlock()

	seenLinkSnap := make(map[string]int)
	var checkReRefreshTask *state.Task
	for _, t := range chg.Tasks() {
		if t.Kind() == "check-rerefresh" {
			checkReRefreshTask = t
		}
		if t.Kind() == "link-snap" {
			sup, err := snapstate.TaskSnapSetup(t)
			c.Assert(err, IsNil)
			if sup.SnapName() == "some-snap" && t.Status() == state.DoneStatus {
				c.Assert(t.Status(), Equals, state.DoneStatus)
			}
			// some-other-snap failed to refresh
			if sup.SnapName() == "some-other-snap" {
				c.Assert(t.Status(), Equals, state.ErrorStatus)
			}
			seenLinkSnap[fmt.Sprintf("%s:%s", sup.SnapName(), sup.Revision())]++
		}
	}

	// some-snap was seen twice, first time was successful refresh, second time was for
	// the revert to previous revision
	c.Check(seenLinkSnap, DeepEquals, map[string]int{
		"some-snap:11": 1,
		"some-snap:1":  1,
		// some-other-snap failed
		"some-other-snap:11": 1,
	})

	var snapSt snapstate.SnapState
	// both snap are at the initial revisions
	c.Assert(snapstate.Get(s.state, "some-snap", &snapSt), IsNil)
	c.Check(snapSt.Current, Equals, snap.R("1"))
	c.Assert(snapstate.Get(s.state, "some-other-snap", &snapSt), IsNil)
	c.Check(snapSt.Current, Equals, snap.R("1"))

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(checkReRefreshTask, NotNil)
	c.Check(checkReRefreshTask.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestUpdatePrerequisiteBackwardsCompat(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "outdated-producer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-producer",
			SnapID:   "outdated-producer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})
	snapstate.Set(s.state, "outdated-consumer", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "outdated-consumer",
			SnapID:   "outdated-consumer-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	tasks, err := snapstate.Update(s.state, "outdated-consumer", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(tasks, Not(HasLen), 0)
	chg := s.state.NewChange("update", "test: update snap")
	chg.AddAll(tasks)

	prereqTask := findStrictlyOnePrereqTask(c, chg)

	var snapsup snapstate.SnapSetup
	err = prereqTask.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	// mimic a task serialized by an "old" snapd without PrereqContentAttrs
	// The new code shouldn't update the prereq since it doesn't have the content attrs
	snapsup.PrereqContentAttrs = nil
	prereqTask.Set("snap-setup", &snapsup)

	s.settle(c)

	// the producer wasn't updated since there were no content attributes
	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "outdated-consumer", target: filepath.Join(dirs.SnapBlobDir, "outdated-consumer_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateDeduplicatesSnapNames(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			RealName: "some-base",
			SnapID:   "some-base-id",
			Revision: snap.R(1),
		}}),
		Current: snap.R(1),
		Active:  true,
	})

	updated, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-base", "some-snap", "some-base"}, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(updated, testutil.DeepUnsortedMatches, []string{"some-snap", "some-base"})
}

func (s *snapmgrTestSuite) TestUpdateDoHiddenDirMigration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:  info.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// check backend hid data
	s.fakeBackend.ops.MustFindOp(c, "hide-snap-data")
	c.Assert(s.fakeBackend.ops.First("init-exposed-snap-home"), IsNil)

	// check state and seq file were updated
	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	assertMigrationState(c, s.state, "some-snap", expected)
}

func (s *snapmgrTestSuite) TestUndoMigrationIfUpdateFailsAfterSettingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:  info.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// fail the change after the link-snap task (after state is saved)
	s.o.TaskRunner().AddHandler("fail", func(*state.Task, *tomb.Tomb) error {
		return errors.New("expected")
	}, nil)

	failingTask := s.state.NewTask("fail", "expected failure")
	chg.AddTask(failingTask)
	linkTask := findLastTask(chg, "link-snap")
	failingTask.WaitFor(linkTask)
	for _, lane := range linkTask.Lanes() {
		failingTask.JoinLane(lane)
	}

	s.settle(c)
	c.Assert(chg.Err(), Not(IsNil))

	// check migration is undone
	s.fakeBackend.ops.MustFindOp(c, "hide-snap-data")
	s.fakeBackend.ops.MustFindOp(c, "undo-hide-snap-data")

	// check migration status was reverted in state and seq file
	assertMigrationState(c, s.state, "some-snap", nil)
}

func (s *snapmgrTestSuite) TestUndoMigrationIfUpdateFails(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:  info.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	// fail at the end
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), Not(IsNil))

	// check migration is undone
	s.fakeBackend.ops.MustFindOp(c, "hide-snap-data")
	s.fakeBackend.ops.MustFindOp(c, "undo-hide-snap-data")

	// check migration is off in state and seq file
	assertMigrationState(c, s.state, "some-snap", nil)
}

func (s *snapmgrTestSuite) TestUpdateAfterMigration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapst := &snapstate.SnapState{
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:        info.Revision,
		Active:         true,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// shouldn't do migration since it was already done
	c.Assert(s.fakeBackend.ops.First("hide-snap-data"), IsNil)
	c.Assert(s.fakeBackend.ops.First("undo-hide-snap-data"), IsNil)

	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	c.Assert(s.fakeBackend.ops.MustFindOp(c, "copy-data").dirOpts, DeepEquals, expected)

	assertMigrationState(c, s.state, "some-snap", expected)
}

func (s *snapmgrTestSuite) TestUpdateAfterCore22Migration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Assert(tr.Set("core", "experimental.hidden-snap-folder", true), IsNil)
	tr.Commit()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}
	snapst := &snapstate.SnapState{
		Sequence:              snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:               info.Revision,
		Active:                true,
		MigratedHidden:        true,
		MigratedToExposedHome: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// shouldn't do migration since it was already done
	c.Check(s.fakeBackend.ops.First("hide-snap-data"), IsNil)
	c.Check(s.fakeBackend.ops.First("undo-hide-snap-data"), IsNil)
	c.Check(s.fakeBackend.ops.First("init-exposed-snap-home"), IsNil)
	c.Check(s.fakeBackend.ops.First("undo-init-exposed-snap-home"), IsNil)

	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true}
	c.Check(s.fakeBackend.ops.MustFindOp(c, "copy-data").dirOpts, DeepEquals, expected)

	assertMigrationState(c, s.state, "some-snap", expected)
}

// takes in some test parameters to prepare the failure and return the root
// error that will cause the failure.
type prepFailFunc func(*overlord.Overlord, *state.Change) error

func (s *snapmgrTestSuite) TestUndoRevertMigrationIfRevertFails(c *C) {
	s.testUndoRevertMigrationIfRevertFails(c, func(_ *overlord.Overlord, chg *state.Change) error {
		err := errors.New("boom")
		s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
			if op.op == "undo-hide-snap-data" {
				return err
			}

			return nil
		}

		return err
	})
}

func (s *snapmgrTestSuite) TestUndoRevertMigrationIfRevertFailsAfterWritingState(c *C) {
	// fail the change after the link-snap task (after state is saved)
	s.testUndoRevertMigrationIfRevertFails(c, failAfterLinkSnap)
}

func (s *snapmgrTestSuite) testUndoRevertMigrationIfRevertFails(c *C, prepFail prepFailFunc) {
	s.state.Lock()
	defer s.state.Unlock()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}

	snapst := &snapstate.SnapState{
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:        info.Revision,
		Active:         true,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	expectedErr := prepFail(s.o, chg)

	s.settle(c)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	s.fakeBackend.ops.MustFindOp(c, "undo-hide-snap-data")

	// check migration reversion was undone in state and seq file
	expectedOpts := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	assertMigrationState(c, s.state, "some-snap", expectedOpts)
}

func containsInOrder(c *C, ops fakeOps, expected []string) {
	var i int
	opNames := make([]string, len(ops))
	for i, op := range ops {
		opNames[i] = op.op
	}

	for _, op := range opNames {
		if op == expected[i] {
			i++

			// found all ops
			if i == len(expected) {
				return
			}
		}
	}

	c.Fatalf("cannot find 1st sequence in 2nd:\n%q\n%q", expected, opNames)
}

func (s *snapmgrTestSuite) TestRevertMigration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	info := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "some-snap-id",
		RealName: "some-snap",
	}

	snapst := &snapstate.SnapState{
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{info}),
		Current:        info.Revision,
		Active:         true,
		MigratedHidden: true,
	}
	snapstate.Set(s.state, "some-snap", snapst)
	c.Assert(snapstate.WriteSeqFile("some-snap", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)

	s.fakeBackend.ops.MustFindOp(c, "undo-hide-snap-data")

	// check migration status is 'off' in state and seq file
	assertMigrationState(c, s.state, "some-snap", nil)
}

func (s *snapmgrTestSuite) TestUpdateDoHiddenDirMigrationOnCore22(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "snap-for-core22-id",
		RealName: "snap-core18-to-core22",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "snap-core18-to-core22", &snapstate.RevisionOptions{Channel: "channel-for-core22/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "core22", target: filepath.Join(dirs.SnapBlobDir, "core22_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "snap-core18-to-core22", target: filepath.Join(dirs.SnapBlobDir, "snap-core18-to-core22_2.snap")},
	})

	containsInOrder(c, s.fakeBackend.ops, []string{"hide-snap-data", "init-exposed-snap-home"})

	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true}
	assertMigrationState(c, s.state, "snap-core18-to-core22", expected)
}

func (s *snapmgrTestSuite) TestUndoMigrationIfUpdateToCore22FailsAfterWritingState(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "snap-for-core22-id",
		RealName: "snap-core18-to-core22",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	// adding core22 so it's easier to find the other snap's tasks below
	snapstate.Set(s.state, "core22", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{
			SnapID:   "core22",
			Revision: snap.R("2"),
			RealName: "core22",
		}}),
		Current:  snap.R("2"),
		SnapType: "base",
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "snap-core18-to-core22", &snapstate.RevisionOptions{Channel: "channel-for-core22/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// fails change while after link-snap runs (after state is persisted)
	expectedErr := failAfterLinkSnap(s.o, chg)

	s.settle(c)
	c.Assert(chg.Err(), Not(IsNil))
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	expectedOps := []string{"hide-snap-data", "init-exposed-snap-home", "undo-init-exposed-snap-home", "undo-hide-snap-data"}
	containsInOrder(c, s.fakeBackend.ops, expectedOps)

	// check that the undoInfo returned by InitExposed and stored in the task is
	// the same as the one passed into UndoInitExposed
	t := findLastTask(chg, "copy-snap-data")
	c.Assert(t, Not(IsNil))

	var undoInfo backend.UndoInfo
	c.Assert(t.Get("undo-exposed-home-init", &undoInfo), IsNil)
	op := s.fakeBackend.ops.MustFindOp(c, "undo-init-exposed-snap-home")
	c.Check(op.undoInfo, DeepEquals, &undoInfo)

	assertMigrationState(c, s.state, "snap-core18-to-core22", nil)
}

func (s *snapmgrTestSuite) TestUndoMigrationIfUpdateToCore22Fails(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "snap-for-core22-id",
		RealName: "snap-core18-to-core22",
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	}
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "snap-core18-to-core22", &snapstate.RevisionOptions{Channel: "channel-for-core22/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// fails change while initializing ~/Snap (before state is persisted)
	expectedErr := errors.New("boom")
	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "init-exposed-snap-home" {
			return expectedErr
		}
		return nil
	}

	s.settle(c)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(chg.Err(), ErrorMatches, fmt.Sprintf(`(.|\s)*%s\)?`, expectedErr.Error()))

	expectedOps := []string{"hide-snap-data", "init-exposed-snap-home"}
	containsInOrder(c, s.fakeBackend.ops, expectedOps)

	assertMigrationState(c, s.state, "snap-core18-to-core22", nil)
}

func (s *snapmgrTestSuite) TestUpdateMigrateTurnOffFlagAndRefreshToCore22(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "snap-for-core22-id",
		RealName: "snap-core18-to-core22",
	}
	snapst := &snapstate.SnapState{
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:        si.Revision,
		Active:         true,
		MigratedHidden: true,
	}
	// state is hidden but flag is off; was turned off just before refresh to
	// core22 base
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "snap-core18-to-core22", &snapstate.RevisionOptions{Channel: "channel-for-core22/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.settle(c)
	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "core22", target: filepath.Join(dirs.SnapBlobDir, "core22_11.snap")},
		{macaroon: s.user.StoreMacaroon, name: "snap-core18-to-core22", target: filepath.Join(dirs.SnapBlobDir, "snap-core18-to-core22_2.snap")},
	})

	containsInOrder(c, s.fakeBackend.ops, []string{"init-exposed-snap-home", "init-xdg-dirs"})

	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true}
	assertMigrationState(c, s.state, "snap-core18-to-core22", expected)
}

func (s *snapmgrTestSuite) TestUpdateMigrateTurnOffFlagAndRefreshToCore22ButFail(c *C) {
	c.Skip("TODO:Snap-folder: no automatic migration for core22 snaps to ~/Snap folder for now")

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		Revision: snap.R(1),
		SnapID:   "snap-for-core22-id",
		RealName: "snap-core18-to-core22",
	}
	snapst := &snapstate.SnapState{
		Sequence:       snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:        si.Revision,
		Active:         true,
		MigratedHidden: true,
	}
	// state is hidden but flag is off; was turned off just before refresh to
	// core22 base
	snapstate.Set(s.state, "snap-core18-to-core22", snapst)
	c.Assert(snapstate.WriteSeqFile("snap-core18-to-core22", snapst), IsNil)

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "snap-core18-to-core22", &snapstate.RevisionOptions{Channel: "channel-for-core22/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "init-exposed-snap-home" {
			return errors.New("boom")
		}
		return nil
	}

	s.settle(c)
	c.Assert(chg.Err(), Not(IsNil))
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	// only the ~/Snap was done and undone
	c.Assert(s.fakeBackend.ops.First("hide-snap-data"), IsNil)
	c.Assert(s.fakeBackend.ops.First("undo-hide-snap-data"), IsNil)

	expected := &dirs.SnapDirOptions{HiddenSnapDataDir: true}
	assertMigrationState(c, s.state, "snap-core18-to-core22", expected)
}

// assertMigrationState checks the migration status in the state and sequence
// file. Fails if no state or sequence file exist.
func assertMigrationState(c *C, st *state.State, snap string, expected *dirs.SnapDirOptions) {
	if expected == nil {
		expected = &dirs.SnapDirOptions{}
	}

	// check snap state has expected migration value
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(st, snap, &snapst), IsNil)
	c.Assert(snapst.MigratedHidden, Equals, expected.HiddenSnapDataDir)
	c.Assert(snapst.MigratedToExposedHome, Equals, expected.MigratedToExposedHome)

	assertMigrationInSeqFile(c, snap, expected)
}

func assertMigrationInSeqFile(c *C, snap string, expected *dirs.SnapDirOptions) {
	if expected == nil {
		expected = &dirs.SnapDirOptions{}
	}

	seqFilePath := filepath.Join(dirs.SnapSeqDir, snap+".json")
	file, err := os.Open(seqFilePath)
	c.Assert(err, IsNil)
	defer file.Close()

	data, err := io.ReadAll(file)
	c.Assert(err, IsNil)

	// check sequence file has expected migration value
	type seqData struct {
		MigratedHidden        bool `json:"migrated-hidden"`
		MigratedToExposedHome bool `json:"migrated-exposed-home"`
	}
	var d seqData
	c.Assert(json.Unmarshal(data, &d), IsNil)
	c.Assert(d.MigratedHidden, Equals, expected.HiddenSnapDataDir)
	c.Assert(d.MigratedToExposedHome, Equals, expected.MigratedToExposedHome)
}

func (s *snapmgrTestSuite) TestUndoInstallAfterDeletingRevisions(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// 1 and 2 should be deleted, 3 is target revision and 4 is current
	seq := []*snap.SideInfo{
		{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(1),
		},
		{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(2),
		},
		{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(3),
		},
		{
			RealName: "some-snap",
			SnapID:   "some-snap-id",
			Revision: snap.R(4),
		},
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos(seq),
		Current:  seq[len(seq)-1].Revision,
	})

	tr := config.NewTransaction(s.state)
	// remove the first two revisions so the old-candidate-index+1 (in undoLinkSnap) would be out of bounds if we didn't
	// account for discarded revisions
	c.Assert(tr.Set("core", "refresh.retain", 1), IsNil)
	tr.Commit()

	s.o.TaskRunner().AddHandler("fail", func(*state.Task, *tomb.Tomb) error {
		return errors.New("expected")
	}, nil)

	// install already stored revision
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: seq[len(seq)-2].Revision}, s.user.ID, snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)
	c.Assert(ts, NotNil)
	chg := s.state.NewChange("refresh", "")
	chg.AddAll(ts)

	// make update fail after removing old snaps
	failTask := s.state.NewTask("fail", "expected")
	disc := findLastTask(chg, "discard-snap")
	for _, lane := range disc.Lanes() {
		failTask.JoinLane(lane)
	}
	failTask.WaitFor(disc)
	chg.AddTask(failTask)

	s.settle(c)

	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.Sequence, DeepEquals, snapstatetest.NewSequenceFromSnapSideInfos(seq[2:]))

	linkTask := findLastTask(chg, "link-snap")
	c.Check(linkTask.Status(), Equals, state.UndoneStatus)

	var oldRevsBeforeCand []snap.Revision
	c.Assert(linkTask.Get("old-revs-before-cand", &oldRevsBeforeCand), IsNil)
	c.Assert(oldRevsBeforeCand, DeepEquals, []snap.Revision{seq[0].Revision, seq[1].Revision})
}

func findLastTask(chg *state.Change, kind string) *state.Task {
	var last *state.Task

	for _, task := range chg.Tasks() {
		if task.Kind() == kind {
			last = task
		}
	}

	return last
}

func (s *snapmgrTestSuite) TestUpdateBaseKernelSingleRebootHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(MakeModel(map[string]interface{}{
		"kernel": "kernel",
		"base":   "core18",
	}))
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siBase} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel and base")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core18"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core18", "kernel"})

	// Verify that correct dependencies have been set-up for single-reboot
	// which is a bit more tricky, as task-sets have been split up into pre-boot
	// things, and post-boot things.

	// Grab the correct task-sets
	var baseTs, kernelTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeKernel {
				kernelTs = ts
				break
			} else if snapsup.Type == snap.TypeBase {
				baseTs = ts
				break
			}
		}
		chg.AddAll(ts)
	}
	c.Assert(baseTs, NotNil)
	c.Assert(kernelTs, NotNil)

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel, err := kernelTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := baseTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of base (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Core18 and kernel should be in the same transactional lane
	c.Check(taskSetsShareLane(baseTs, kernelTs), Equals, true)

	// Manually verify the lanes of the initial task for the 4 task-sets
	c.Check(kernelTs.Tasks()[0].Lanes(), DeepEquals, []int{1, 2})
	c.Check(baseTs.Tasks()[0].Lanes(), DeepEquals, []int{2, 1})

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"core18": true,
	}

	s.settle(c)

	// mock restart for the 'link-snap' step and run change to
	// completion.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// a single system restart was requested
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartSystem,
	})

	for _, name := range []string{"kernel", "core18"} {
		snapID := "kernel-id"
		if name == "core18" {
			snapID = "core18-snap-id"
		}
		var snapst snapstate.SnapState
		err = snapstate.Get(s.state, name, &snapst)
		c.Assert(err, IsNil)

		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 2)
		c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Channel:  "",
			Revision: snap.R(7),
		}, nil))
		c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			Channel:  "latest/stable",
			SnapID:   snapID,
			Revision: snap.R(11),
		}, nil))
	}

	// ops come in semi random order, but we know that link and auto-connect
	// operations will be done in a specific order,
	ops := make([]string, 0, 8)
	for _, op := range s.fakeBackend.ops {
		if op.op == "link-snap" {
			split := strings.Split(op.path, "/")
			c.Assert(len(split) > 2, Equals, true)
			ops = append(ops, filepath.Join(split[len(split)-2:]...))
		} else if op.op == "cleanup-trash" {
			ops = append(ops, fmt.Sprintf("%s-%s", op.op, op.name))
		} else if strings.HasPrefix(op.op, "auto-connect:") || strings.HasPrefix(op.op, "setup-profiles:") {
			ops = append(ops, fmt.Sprintf("%s-%s/%s", op.op, op.name, op.revno))
		}
	}
	c.Assert(ops, HasLen, 8)
	c.Check(ops[0:4], testutil.DeepUnsortedMatches, []string{
		"setup-profiles:Doing-kernel/11", "kernel/11",
		"setup-profiles:Doing-core18/11", "core18/11",
	})
	c.Check(ops[4:6], DeepEquals, []string{
		"auto-connect:Doing-core18/11", "auto-connect:Doing-kernel/11",
	})
	c.Check(ops[6:], testutil.DeepUnsortedMatches, []string{
		"cleanup-trash-core18", "cleanup-trash-kernel",
	})
}

func (s *snapmgrTestSuite) TestUpdateGadgetKernelSingleRebootHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// Handle gadget update tasks to avoid making tests unnecessarily complex
	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siGadget := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
		SnapID:   "gadget-core18-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siGadget} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "gadget" {
			typ = "gadget"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel and gadget")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"gadget", "kernel"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"gadget", "kernel"})

	// Verify that correct dependencies have been set-up for single-reboot
	// which is a bit more tricky, as task-sets have been split up into pre-boot
	// things, and post-boot things.

	// Grab the correct task-sets
	var gadgetTs, kernelTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeKernel {
				kernelTs = ts
				break
			} else if snapsup.Type == snap.TypeGadget {
				gadgetTs = ts
				break
			}
		}
		chg.AddAll(ts)
	}
	c.Assert(gadgetTs, NotNil)
	c.Assert(kernelTs, NotNil)

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel, err := kernelTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := gadgetTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// Gadget and kernel should be in the same transactional lane
	c.Check(taskSetsShareLane(gadgetTs, kernelTs), Equals, true)

	// Manually verify the lanes of the initial task for the 4 task-sets
	c.Check(kernelTs.Tasks()[0].Lanes(), DeepEquals, []int{1, 2})
	c.Check(gadgetTs.Tasks()[0].Lanes(), DeepEquals, []int{2, 1})

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"gadget": true,
	}

	s.settle(c)

	// mock restart for the 'link-snap' step and run change to
	// completion.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// a single system restart was requested
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartSystem,
	})

	for _, name := range []string{"kernel", "gadget"} {
		snapID := "kernel-id"
		if name == "gadget" {
			snapID = "gadget-core18-id"
		}
		var snapst snapstate.SnapState
		err = snapstate.Get(s.state, name, &snapst)
		c.Assert(err, IsNil)

		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 2)
		c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Channel:  "",
			Revision: snap.R(7),
		}, nil))
		c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			Channel:  "latest/stable",
			SnapID:   snapID,
			Revision: snap.R(11),
		}, nil))
	}

	// ops come in semi random order, but we know that link and auto-connect
	// operations will be done in a specific order,
	ops := make([]string, 0, 8)
	for _, op := range s.fakeBackend.ops {
		if op.op == "link-snap" {
			split := strings.Split(op.path, "/")
			c.Assert(len(split) > 2, Equals, true)
			ops = append(ops, filepath.Join(split[len(split)-2:]...))
		} else if op.op == "cleanup-trash" {
			ops = append(ops, fmt.Sprintf("%s-%s", op.op, op.name))
		} else if strings.HasPrefix(op.op, "auto-connect:") || strings.HasPrefix(op.op, "setup-profiles:") {
			ops = append(ops, fmt.Sprintf("%s-%s/%s", op.op, op.name, op.revno))
		}
	}
	c.Assert(ops, HasLen, 8)
	c.Check(ops[0:4], testutil.DeepUnsortedMatches, []string{
		"setup-profiles:Doing-kernel/11", "kernel/11",
		"setup-profiles:Doing-gadget/11", "gadget/11",
	})
	c.Check(ops[4:6], DeepEquals, []string{
		"auto-connect:Doing-gadget/11", "auto-connect:Doing-kernel/11",
	})
	c.Check(ops[6:], testutil.DeepUnsortedMatches, []string{
		"cleanup-trash-gadget", "cleanup-trash-kernel",
	})
}

func (s *snapmgrTestSuite) TestUpdateBaseGadgetSingleRebootHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// Handle gadget update tasks to avoid making tests unnecessarily complex
	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	siGadget := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
		SnapID:   "gadget-core18-id",
	}
	for _, si := range []*snap.SideInfo{&siBase, &siGadget} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh base and gadget")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"gadget", "core18"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Check(affected, DeepEquals, []string{"core18", "gadget"})

	// Verify that correct dependencies have been set-up for single-reboot
	// which is a bit more tricky, as task-sets have been split up into pre-boot
	// things, and post-boot things.

	// Grab the correct task-sets
	var gadgetTs, baseTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeBase {
				baseTs = ts
				break
			} else if snapsup.Type == snap.TypeGadget {
				gadgetTs = ts
				break
			}
		}
		chg.AddAll(ts)
	}
	c.Assert(gadgetTs, NotNil)
	c.Assert(baseTs, NotNil)

	// Grab the tasks we need to check dependencies between
	linkTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := baseTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfGadget, err := gadgetTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)

	// - "prerequisites" (BeginEdge) of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of gadget (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Gadget and base should be in the same transactional lane
	c.Check(taskSetsShareLane(gadgetTs, baseTs), Equals, true)

	// Manually verify the lanes of the initial task for the 4 task-sets
	c.Check(baseTs.Tasks()[0].Lanes(), DeepEquals, []int{1, 2})
	c.Check(gadgetTs.Tasks()[0].Lanes(), DeepEquals, []int{2, 1})

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
		"gadget": true,
	}

	s.settle(c)

	// mock restart for the 'link-snap' step and run change to
	// completion.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// a single system restart was requested
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartSystem,
	})

	for _, name := range []string{"core18", "gadget"} {
		snapID := "core18-snap-id"
		if name == "gadget" {
			snapID = "gadget-core18-id"
		}
		var snapst snapstate.SnapState
		err = snapstate.Get(s.state, name, &snapst)
		c.Assert(err, IsNil)

		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 2)
		c.Assert(snapst.Sequence.Revisions[0], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Channel:  "",
			Revision: snap.R(7),
		}, nil))
		c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			Channel:  "latest/stable",
			SnapID:   snapID,
			Revision: snap.R(11),
		}, nil))
	}

	// ops come in semi random order, but we know that link and auto-connect
	// operations will be done in a specific order,
	ops := make([]string, 0, 8)
	for _, op := range s.fakeBackend.ops {
		if op.op == "link-snap" {
			split := strings.Split(op.path, "/")
			c.Assert(len(split) > 2, Equals, true)
			ops = append(ops, filepath.Join(split[len(split)-2:]...))
		} else if op.op == "cleanup-trash" {
			ops = append(ops, fmt.Sprintf("%s-%s", op.op, op.name))
		} else if strings.HasPrefix(op.op, "auto-connect:") || strings.HasPrefix(op.op, "setup-profiles:") {
			ops = append(ops, fmt.Sprintf("%s-%s/%s", op.op, op.name, op.revno))
		}
	}
	c.Assert(ops, HasLen, 8)
	c.Check(ops[0:4], testutil.DeepUnsortedMatches, []string{
		"setup-profiles:Doing-core18/11", "core18/11",
		"setup-profiles:Doing-gadget/11", "gadget/11",
	})
	c.Check(ops[4:6], DeepEquals, []string{
		"auto-connect:Doing-core18/11", "auto-connect:Doing-gadget/11",
	})
	c.Check(ops[6:], testutil.DeepUnsortedMatches, []string{
		"cleanup-trash-gadget", "cleanup-trash-core18",
	})
}

func (s *snapmgrTestSuite) TestUpdateBaseKernelSingleRebootWithCannotRebootSetHappy(c *C) {
	// Verify the single-reboot still works when using "cannot-reboot" variable, to maintain
	// backwards compatibility with the previous logic.
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(MakeModel(map[string]interface{}{
		"kernel": "kernel",
		"base":   "core18",
	}))
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siBase} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel and base")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core18"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core18", "kernel"})

	// Get the link-snap task of base, and set "cannot-reboot"
	var linkSnapOfBase *state.Task
	for _, ts := range tss {
		chg.AddAll(ts)

		for _, t := range ts.Tasks() {
			if t.Kind() != "link-snap" {
				continue
			}

			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeBase {
				linkSnapOfBase = t
				break
			}
		}
	}
	c.Assert(linkSnapOfBase, NotNil)

	// Fake an older snapd having set this in a previous change
	linkSnapOfBase.Set("cannot-reboot", true)

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"core18": true,
	}

	s.settle(c)

	// mock restart for the 'link-snap' step and run change to
	// completion.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// a single system restart was requested
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartSystem,
	})
	// verify that the log message appeared in the link-snap task
	c.Check(linkSnapOfBase.Log(), HasLen, 1)
	c.Check(linkSnapOfBase.Log()[0], Matches, `.* reboot postponed to later tasks`)
}

func (s *snapmgrTestSuite) TestUpdateBaseKernelSingleRebootUnsupportedWithCoreHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(DefaultModel())
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siCore := snap.SideInfo{
		RealName: "core",
		Revision: snap.R(7),
		SnapID:   "core-snap-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siCore} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel and base")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core", "kernel"})
	var kernelTs, coreTs *state.TaskSet
	for _, ts := range tss {
		chg.AddAll(ts)
		for _, tsk := range ts.Tasks() {
			switch tsk.Kind() {
			// setup-profiles should appear right before link-snap
			case "link-snap", "auto-connect", "setup-profiles", "set-auto-aliases":
				snapsup, err := snapstate.TaskSnapSetup(tsk)
				c.Assert(err, IsNil)
				if tsk.Kind() == "link-snap" {
					opts := 0
					verifyUpdateTasks(c, snapsup.Type, opts, 0, ts)
					if snapsup.Type == snap.TypeOS {
						coreTs = ts
					} else if snapsup.Type == snap.TypeKernel {
						kernelTs = ts
					}
				}
			}
		}
	}

	// Core should come first, and it's "prerequisite" task should have no
	// dependencies. The first task of kernel should depend on the last task
	// of core. Single-reboot is not supported, so we expect them to run in serial.
	firstTaskOfBase, err := coreTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	firstTaskOfKernel, err := kernelTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := coreTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	c.Check(firstTaskOfBase.WaitTasks(), HasLen, 0)
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Core and kernel should not be in the same transactional lane, as this
	// is behaviour we want to have on UC16
	c.Check(taskSetsShareLane(coreTs, kernelTs), Equals, false)

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"core":   true,
	}

	s.settle(c)

	// first 'auto-connect' restart here
	s.mockRestartAndSettle(c, chg)

	// second 'auto-connect' restart here
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	// when updating both kernel that uses core as base, and "core" we have two reboots
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartSystem,
		restart.RestartSystem,
	})

	for _, name := range []string{"kernel", "core"} {
		snapID := "kernel-id"
		if name == "core" {
			snapID = "core-snap-id"
		}
		var snapst snapstate.SnapState
		err = snapstate.Get(s.state, name, &snapst)
		c.Assert(err, IsNil)

		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 2)
		c.Assert(snapst.Sequence.Revisions[1], DeepEquals, sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: name,
			Channel:  "latest/stable",
			SnapID:   snapID,
			Revision: snap.R(11),
		}, nil))
	}
}

func (s *snapmgrTestSuite) TestUpdateBaseGadgetKernelSingleReboot(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(MakeModel(map[string]interface{}{
		"kernel": "kernel",
		"base":   "core18",
	}))
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	siGadget := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
		SnapID:   "gadget-core18-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siBase, &siGadget} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		} else if si.RealName == "gadget" {
			typ = "gadget"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel, base and gadget")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core18", "gadget"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core18", "gadget", "kernel"})
	var kernelTs, baseTs, gadgetTs *state.TaskSet
	for _, ts := range tss {
		chg.AddAll(ts)
	inner:
		for _, tsk := range ts.Tasks() {
			switch tsk.Kind() {
			// setup-profiles should appear right before link-snap,
			// while set-auto-aliase appears right after
			// auto-connect
			case "link-snap":
				snapsup, err := snapstate.TaskSnapSetup(tsk)
				c.Assert(err, IsNil)
				switch snapsup.InstanceName() {
				case "kernel":
					kernelTs = ts
				case "gadget":
					gadgetTs = ts
				case "core18":
					baseTs = ts
				}
				break inner
			}
		}
	}
	c.Assert(kernelTs, NotNil)
	c.Assert(baseTs, NotNil)
	c.Assert(gadgetTs, NotNil)

	// Core18, gadget and kernel should end up in the same transactional lane
	c.Check(taskSetsShareLane(baseTs, gadgetTs, kernelTs), Equals, true)
	// Grab the tasks we need to check dependencies between
	linkTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := baseTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfGadget, err := gadgetTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := gadgetTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := gadgetTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfKernel, err := kernelTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)

	// Things that must be correct between base and gadget:
	// - "prerequisites" (BeginEdge) of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Things that must be correct between gadget and kernel:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// Manually verify their lanes
	c.Check(linkTaskOfBase.Lanes(), testutil.DeepUnsortedMatches, []int{1, 2, 3})
	c.Check(linkTaskOfGadget.Lanes(), testutil.DeepUnsortedMatches, []int{1, 2, 3})
	c.Check(linkTaskOfKernel.Lanes(), testutil.DeepUnsortedMatches, []int{1, 2, 3})
}

func (s *snapmgrTestSuite) TestUpdateBaseKernelSingleRebootUndone(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	// use services-snap here to make sure services would be stopped/started appropriately
	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siBase} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh kernel and base")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core18"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core18", "kernel"})

	// Verify that correct dependencies have been set-up for single-reboot
	// which is a bit more tricky, as task-sets have been split up into pre-boot
	// things, and post-boot things.

	// Grab the correct task-sets
	var baseTs, kernelTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeKernel {
				kernelTs = ts
				break
			} else if snapsup.Type == snap.TypeBase {
				baseTs = ts
				break
			}
		}
		chg.AddAll(ts)
	}
	c.Assert(baseTs, NotNil)
	c.Assert(kernelTs, NotNil)

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel, err := kernelTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := kernelTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := baseTs.Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := baseTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of base (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"core18": true,
	}
	errInjected := 0
	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "auto-connect:Doing" && op.name == "kernel" {
			errInjected++
			return fmt.Errorf("auto-connect-kernel mock error")
		}
		return nil
	}

	s.settle(c)

	// both snaps have requested a restart at 'auto-connect', handle this here
	s.mockRestartAndSettle(c, chg)

	// both snaps have requested another restart along the undo path at 'unlink-current-snap'
	// because reboots are post-poned until the change have no more tasks to run, and how the
	// change is manipulated in this specific case, we only do one reboot along the undo-path as well now.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\(auto-connect-kernel mock error\)`)
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		// do path
		restart.RestartSystem,
		// undo
		restart.RestartSystem,
	})
	c.Check(errInjected, Equals, 1)

	// ops come in semi random order, but we know that link and auto-connect
	// operations will be done in a specific order,
	ops := make([]string, 0, 7)
	for _, op := range s.fakeBackend.ops {
		if op.op == "link-snap" {
			split := strings.Split(op.path, "/")
			c.Assert(len(split) > 2, Equals, true)
			ops = append(ops, filepath.Join(split[len(split)-2:]...))
		} else if strings.HasPrefix(op.op, "auto-connect:") {
			ops = append(ops, fmt.Sprintf("%s-%s/%s", op.op, op.name, op.revno))
		}
	}
	c.Assert(ops, HasLen, 7)
	c.Assert(ops[0:5], DeepEquals, []string{
		// link snaps
		"core18/11", "kernel/11",
		"auto-connect:Doing-core18/11",
		"auto-connect:Doing-kernel/11", // fails
		"auto-connect:Undoing-core18/11",
	})
	// those run unordered
	c.Assert(ops[5:], testutil.DeepUnsortedMatches, []string{"core18/7", "kernel/7"})
}

func (s *snapmgrTestSuite) TestUpdateBaseGadgetKernelSingleRebootUndone(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// make it easier for us to mock the whole gadget update so we don't
	// have to jump through to many hoops.
	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	siKernel := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(7),
		SnapID:   "kernel-id",
	}
	siBase := snap.SideInfo{
		RealName: "core18",
		Revision: snap.R(7),
		SnapID:   "core18-snap-id",
	}
	siGadget := snap.SideInfo{
		RealName: "gadget",
		Revision: snap.R(7),
		SnapID:   "gadget-core18-id",
	}
	for _, si := range []*snap.SideInfo{&siKernel, &siBase, &siGadget} {
		snaptest.MockSnap(c, fmt.Sprintf(`name: %s`, si.RealName), si)
		typ := "kernel"
		if si.RealName == "core18" {
			typ = "base"
		} else if si.RealName == "gadget" {
			typ = "gadget"
		}
		snapstate.Set(s.state, si.RealName, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        typ,
		})
	}

	chg := s.state.NewChange("refresh", "refresh base, gadget and kernel")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"kernel", "core18", "gadget"}, nil, s.user.ID, &snapstate.Flags{})
	c.Assert(err, IsNil)
	c.Assert(affected, DeepEquals, []string{"core18", "gadget", "kernel"})

	// Grab the correct task-sets
	var baseTs, gadgetTs, kernelTs *state.TaskSet
	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			snapsup, err := snapstate.TaskSnapSetup(t)
			if err != nil {
				continue
			}
			if snapsup.Type == snap.TypeKernel {
				kernelTs = ts
				break
			} else if snapsup.Type == snap.TypeBase {
				baseTs = ts
				break
			} else if snapsup.Type == snap.TypeGadget {
				gadgetTs = ts
				break
			}
		}
		chg.AddAll(ts)
	}
	c.Assert(baseTs, NotNil)
	c.Assert(kernelTs, NotNil)
	c.Assert(gadgetTs, NotNil)

	// have fake backend indicate a need to reboot for both snaps
	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"kernel": true,
		"core18": true,
		"gadget": true,
	}
	errInjected := 0
	s.fakeBackend.maybeInjectErr = func(op *fakeOp) error {
		if op.op == "auto-connect:Doing" && op.name == "kernel" {
			errInjected++
			return fmt.Errorf("auto-connect-kernel mock error")
		}
		return nil
	}

	s.settle(c)

	// kernel requests a reboot
	s.mockRestartAndSettle(c, chg)

	// base snap requests another restart along the undo path at 'unlink-current-snap'
	s.mockRestartAndSettle(c, chg)

	checkUndone := func(ts *state.TaskSet, name string) {
		for _, t := range ts.Tasks() {
			switch t.Status() {
			case state.UndoneStatus, state.ErrorStatus, state.HoldStatus:
				continue
			case state.DoneStatus:
				// following tasks don't have undo logic
				switch t.Kind() {
				case "prerequisites", "validate-snap", "run-hook", "cleanup":
					break
				default:
					c.Errorf("unexpected done-status for %s task %s", name, t.Kind())
				}
			default:
				c.Errorf("unexpected status for %s task %s: %s", name, t.Kind(), t.Status())
			}
		}
	}

	// Expect all task-sets to have been undone, as essential snaps are considered transactional
	// when updated together. (I.e all are done or all are undone)
	checkUndone(baseTs, "base")
	checkUndone(gadgetTs, "gadget")
	checkUndone(kernelTs, "kernel")

	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\(auto-connect-kernel mock error\)`)
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		// do path
		restart.RestartSystem,
		// undo
		restart.RestartSystem,
	})
	c.Check(errInjected, Equals, 1)
}

func failAfterLinkSnap(ol *overlord.Overlord, chg *state.Change) error {
	err := errors.New("expected")
	ol.TaskRunner().AddHandler("fail", func(*state.Task, *tomb.Tomb) error {
		return err
	}, nil)

	failingTask := ol.State().NewTask("fail", "expected failure")
	chg.AddTask(failingTask)
	linkTask := findLastTask(chg, "link-snap")
	failingTask.WaitFor(linkTask)
	for _, lane := range linkTask.Lanes() {
		failingTask.JoinLane(lane)
	}

	return err
}

func (s *snapmgrTestSuite) testUpdateEssentialSnapsOrder(c *C, order []string) {
	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// make it easier for us to mock the whole gadget update so we don't
	// have to jump through to many hoops. Streamline it a bit as well by
	// making sure that one of the tasks requires a reboot by the link-snap
	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	types := map[string]string{
		"snapd":          "snapd",
		"core18":         "base",
		"gadget":         "gadget",
		"kernel":         "kernel",
		"some-base":      "base",
		"some-base-snap": "app",
	}
	snapIds := map[string]string{
		"snapd":          "snapd-snap-id",
		"core18":         "core18-snap-id",
		"gadget":         "gadget-core18-id",
		"kernel":         "kernel-id",
		"some-base":      "some-base-id",
		"some-base-snap": "some-base-snap-id",
	}

	for _, sn := range order {
		si := &snap.SideInfo{RealName: sn, Revision: snap.R(1), SnapID: snapIds[sn]}
		snapYaml := fmt.Sprintf("name: %s\nversion: 1.2.3\ntype: %s", sn, types[sn])
		if sn == "some-base-snap" {
			// add a base for the app if the base is in the update list
			snapYaml += "\nbase: some-base"
		}

		snaptest.MockSnap(c, snapYaml, si)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})
	}

	chg := s.state.NewChange("refresh", fmt.Sprintf("refresh %v", order))
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		order, nil, s.user.ID, &snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Assert(affected, testutil.DeepUnsortedMatches, order)

	findTs := func(sn string) *state.TaskSet {
		for _, ts := range tss {
			for _, tsk := range ts.Tasks() {
				if tsk.Kind() == "prerequisites" {
					snapsup, err := snapstate.TaskSnapSetup(tsk)
					c.Assert(err, IsNil)
					if snapsup.InstanceName() == sn {
						return ts
					}
					break
				}
			}
		}
		return nil
	}

	// Map all relevant task-sets.
	tsByName := make(map[string]*state.TaskSet)
	for _, sn := range order {
		ts := findTs(sn)
		c.Assert(ts, NotNil)
		chg.AddAll(ts)
		tsByName[sn] = ts
	}

	// Ensure no circular dependency
	c.Check(chg.CheckTaskDependencies(), IsNil)

	// Ensure that all reboot participants pre-requisites are correctly linked
	var prevRebootTs *state.TaskSet
NextSnap1:
	for _, sn := range order {
	IsRebootSnap:
		switch sn {
		case "core18", "kernel", "gadget":
			break IsRebootSnap
		default:
			continue NextSnap1
		}

		currentTs := tsByName[sn]
		if prevRebootTs == nil {
			// For the first of these snaps, only one other can precede it, and that is
			// the snapd snap. If the snapd snap is present, make sure it's depending on
			// that
			if snapdTs := tsByName["snapd"]; snapdTs != nil {
				firstTaskOfCurrent, err := currentTs.Edge(snapstate.BeginEdge)
				c.Assert(err, IsNil)
				lastTaskOfPrev, err := snapdTs.Edge(snapstate.EndEdge)
				c.Assert(err, IsNil)
				c.Check(firstTaskOfCurrent.WaitTasks(), testutil.Contains, lastTaskOfPrev)
			}
		} else {
			firstTaskOfCurrent, err := currentTs.Edge(snapstate.BeginEdge)
			c.Assert(err, IsNil)
			linkSnapOfPrev, err := prevRebootTs.Edge(snapstate.MaybeRebootEdge)
			c.Assert(err, IsNil)
			c.Check(firstTaskOfCurrent.WaitTasks(), testutil.Contains, linkSnapOfPrev)
		}
		prevRebootTs = tsByName[sn]
	}

	// Ensure that auto-connect for the reboot-snaps is correctly linked.
	lastRebootTs := prevRebootTs
	var i int
NextSnap2:
	for _, sn := range order {
	IsRebootSnapAC:
		switch sn {
		case "core18", "kernel", "gadget":
			break IsRebootSnapAC
		default:
			continue NextSnap2
		}

		// Skip for last of these, since other task-sets are linked
		// differently.
		currentTs := tsByName[sn]
		if currentTs == lastRebootTs {
			break
		}

		// first "auto-connect" must depend on "link-snap" of previous TS
		if i == 0 {
			acTaskOfCurrent, err := currentTs.Edge(snapstate.MaybeRebootWaitEdge)
			c.Assert(err, IsNil)
			linkSnapOfPrev, err := prevRebootTs.Edge(snapstate.MaybeRebootEdge)
			c.Assert(err, IsNil)
			c.Check(acTaskOfCurrent.WaitTasks(), testutil.Contains, linkSnapOfPrev)
		} else {
			// other "auto-connect" must depend on last task of previous TS
			acTaskOfCurrent, err := currentTs.Edge(snapstate.MaybeRebootWaitEdge)
			c.Assert(err, IsNil)
			lastTaskOfPrev, err := prevRebootTs.Edge(snapstate.EndEdge)
			c.Assert(err, IsNil)
			c.Check(acTaskOfCurrent.WaitTasks(), testutil.Contains, lastTaskOfPrev)
		}
		prevRebootTs = currentTs
		i++
	}

	// Verify by using edges that other non-essential task-sets are correctly
	// connected to the previous task-set.
	var prevTs *state.TaskSet
NextSnap3:
	for _, sn := range order {
		currentTs := tsByName[sn]
	IsEssential:
		switch sn {
		case "snapd", "core18", "kernel", "gadget":
			prevTs = currentTs
			continue NextSnap3
		default:
			break IsEssential
		}

		// Validate the first task-set has no deps
		if prevTs == nil {
			firstTaskOfCurrent, err := currentTs.Edge(snapstate.BeginEdge)
			c.Assert(err, IsNil)
			c.Check(firstTaskOfCurrent.WaitTasks(), HasLen, 0)
			prevTs = currentTs
			continue
		}

		firstTaskOfCurrent, err := currentTs.Edge(snapstate.BeginEdge)
		c.Assert(err, IsNil)
		lastTaskOfPrev, err := prevTs.Edge(snapstate.EndEdge)
		c.Assert(err, IsNil)
		c.Check(firstTaskOfCurrent.WaitTasks(), testutil.Contains, lastTaskOfPrev)
		prevTs = currentTs
	}

	// determine the number of reboots we expect
	s.fakeBackend.linkSnapRebootFor = make(map[string]bool)
	for _, o := range order {
		switch o {
		case "core18", "kernel", "gadget":
			s.fakeBackend.linkSnapRebootFor[o] = true
		}
	}
	s.fakeBackend.linkSnapMaybeReboot = len(s.fakeBackend.linkSnapRebootFor) > 0

	s.settle(c)

	if !s.fakeBackend.linkSnapMaybeReboot {
		// no reboot expected, skip to next checks
	} else {
		c.Check(chg.IsReady(), Equals, false)
		c.Check(chg.Status(), Equals, state.WaitStatus)

		// always one reboot expected
		s.mockRestartAndSettle(c, chg)
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderAll(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"snapd", "core18", "gadget", "kernel"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderSnapdBase(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"snapd", "core18"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderBaseGadgetKernel(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"core18", "gadget", "kernel"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderBaseKernel(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"core18", "kernel"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderBaseGadget(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"core18", "gadget"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderBaseGadgetAndSnaps(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"core18", "gadget", "some-base", "some-base-snap"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderGadgetKernel(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"gadget", "kernel"})
}

func (s *snapmgrTestSuite) TestUpdateEssentialSnapsOrderGadgetKernelAndSnaps(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"gadget", "kernel", "some-base", "some-base-snap"})
}

func (s *snapmgrTestSuite) TestUpdateSnapsOrderSnapdBaseApp(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"snapd", "some-base", "some-base-snap"})
}

func (s *snapmgrTestSuite) TestUpdateSnapsOrderBaseApp(c *C) {
	s.testUpdateEssentialSnapsOrder(c, []string{"some-base", "some-base-snap"})
}

func (s *snapmgrTestSuite) TestUpdateBaseAndSnapdOrder(c *C) {
	// verify that when snapd and a base are updated in one go, the base is
	// set up to have snapd as a prerequisite

	restore := release.MockOnClassic(false)
	defer restore()
	restore = snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var restartRequested []restart.RestartType
	_, err := restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(t restart.RestartType) {
		restartRequested = append(restartRequested, t)
	}))
	c.Assert(err, IsNil)

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	siSnapd := &snap.SideInfo{RealName: "snapd", Revision: snap.R(1), SnapID: "snapd-snap-id"}
	snaptest.MockSnap(c, "name: snapd\nversion: 1.2.3\ntype: snapd", siSnapd)
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siSnapd}),
		Current:         siSnapd.Revision,
		TrackingChannel: "latest/stable",
		SnapType:        "snapd",
	})
	siBase := &snap.SideInfo{RealName: "core18", Revision: snap.R(1), SnapID: "core18-snap-id"}
	snaptest.MockSnap(c, "name: core18\nversion: 1.2.3\ntype: base", siBase)
	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siBase}),
		Current:         siBase.Revision,
		TrackingChannel: "latest/stable",
		SnapType:        "base",
	})

	chg := s.state.NewChange("refresh", "refresh snapd and base")
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"core18", "snapd"}, nil, s.user.ID, &snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)
	sort.Strings(affected)
	c.Assert(affected, DeepEquals, []string{"core18", "snapd"})

	// Core18 and snapd are both essential snaps, so we verify that the order between
	// them are correct. Snapd should *always* be updated first, and core18 must have
	// a dependency on snapd

	// grab the task sets of snapd and the base
	var snapdTs, baseTs *state.TaskSet
	for _, ts := range tss {
		chg.AddAll(ts)
		for _, tsk := range ts.Tasks() {
			if tsk.Kind() == "link-snap" {
				snapsup, err := snapstate.TaskSnapSetup(tsk)
				c.Assert(err, IsNil)
				if snapsup.InstanceName() == "snapd" {
					snapdTs = ts
				} else {
					baseTs = ts
				}
			}
		}
	}
	c.Assert(snapdTs, NotNil)
	c.Assert(baseTs, NotNil)

	// Use edges to verify there are correct dependencies
	firstTaskOfBase, err := baseTs.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	lastTaskOfSnapd, err := snapdTs.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	c.Check(firstTaskOfBase.WaitTasks(), testutil.Contains, lastTaskOfSnapd)

	s.fakeBackend.linkSnapMaybeReboot = true
	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
	}

	s.settle(c)

	// mock restart for the 'link-snap' step and run change to
	// completion.
	s.mockRestartAndSettle(c, chg)

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(restartRequested, DeepEquals, []restart.RestartType{
		restart.RestartDaemon,
		restart.RestartSystem,
	})
}

func (s *snapmgrTestSuite) TestUpdateConsidersProvenance(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "provenance-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "provenance-snap", SnapID: "provenance-snap-id", Revision: snap.R(7)}}),
		Current:         snap.R(7),
		SnapType:        "app",
	})

	ts, err := snapstate.Update(s.state, "provenance-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	var snapsup snapstate.SnapSetup
	err = ts.Tasks()[0].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Check(snapsup.ExpectedProvenance, Equals, "prov")
}

func (s *snapmgrTestSuite) TestGeneralRefreshSkipsGatedSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, name := range []string{"some-snap", "some-other-snap"} {
		snapID := fmt.Sprintf("%s-id", name)
		si := &snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Revision: snap.R(7),
		}

		snaptest.MockSnap(c, `name: some-snap`, si)
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  si.Revision,
		})
	}

	err := snapstate.HoldRefreshesBySystem(s.state, snapstate.HoldGeneral, "forever", []string{"some-snap"})
	c.Assert(err, IsNil)

	// advance time by a year (the last refreshed is determined by the snap file's
	// timestamp so we can't just mock time.Now() before to pin it)
	plusYearTime := time.Now().Add(365 * 24 * time.Hour)
	restore := snapstate.MockTimeNow(func() time.Time {
		return plusYearTime
	})
	defer restore()

	chg := s.state.NewChange("update", "update all snaps")
	updates, tss, err := snapstate.UpdateMany(context.Background(), s.state, nil, nil, s.user.ID, nil)
	c.Check(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-other-snap"})

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestSpecificRefreshRefreshesHeldSnaps(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, name := range []string{"some-snap", "some-other-snap"} {
		snapID := fmt.Sprintf("%s-id", name)
		si := &snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Revision: snap.R(7),
		}

		snaptest.MockSnap(c, `name: some-snap`, si)
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  si.Revision,
		})
	}

	err := snapstate.HoldRefreshesBySystem(s.state, snapstate.HoldGeneral, "forever", []string{"some-snap"})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("update", "update all snaps")
	updates, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-other-snap", "some-snap"}, nil, s.user.ID, nil)
	c.Check(err, IsNil)
	c.Check(updates, DeepEquals, []string{"some-other-snap", "some-snap"})

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestUpdateManyTransactionalWithLane(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, name := range []string{"some-snap", "some-other-snap"} {
		snapID := fmt.Sprintf("%s-id", name)
		si := &snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Revision: snap.R(7),
		}

		snaptest.MockSnap(c, `name: some-snap`, si)
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  si.Revision,
		})
	}

	lane := s.state.NewLane()
	flags := &snapstate.Flags{
		Transaction: client.TransactionAllSnaps,
		Lane:        lane,
		// the check rerefresh taskset doesn't run in the same lane
		NoReRefresh: true,
	}
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-other-snap"}, nil, s.user.ID, flags)
	c.Assert(err, IsNil)
	c.Check(affected, testutil.DeepUnsortedMatches, []string{"some-snap", "some-other-snap"})
	c.Check(tss, HasLen, 2)

	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{lane})
		}
	}
}

func (s *snapmgrTestSuite) TestUpdateManyLaneErrorsWithLaneButNoTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, name := range []string{"some-snap", "some-other-snap"} {
		snapID := fmt.Sprintf("%s-id", name)
		si := &snap.SideInfo{
			RealName: name,
			SnapID:   snapID,
			Revision: snap.R(7),
		}

		snaptest.MockSnap(c, `name: some-snap`, si)
		snapstate.Set(s.state, name, &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  si.Revision,
		})
	}

	lane := s.state.NewLane()
	flags := &snapstate.Flags{
		Lane: lane,
	}

	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-other-snap"}, nil, s.user.ID, flags)
	c.Assert(err, ErrorMatches, "cannot specify a lane without setting transaction to \"all-snaps\"")
	c.Check(tss, IsNil)
	c.Check(affected, IsNil)

	flags.Transaction = client.TransactionPerSnap

	affected, tss, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-other-snap"}, nil, s.user.ID, flags)
	c.Assert(err, ErrorMatches, "cannot specify a lane without setting transaction to \"all-snaps\"")
	c.Check(tss, IsNil)
	c.Check(affected, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateTransactionalWithLane(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	lane := s.state.NewLane()
	flags := &snapstate.Flags{
		Transaction: client.TransactionAllSnaps,
		Lane:        lane,
		// the check rerefresh taskset doesn't run in the same lane
		NoReRefresh: true,
	}

	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, *flags)
	c.Assert(err, IsNil)
	c.Assert(ts, Not(IsNil))
	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{lane})
	}
}

func (s *snapmgrTestSuite) TestUpdateLaneErrorsWithLaneButNoTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	lane := s.state.NewLane()
	flags := &snapstate.Flags{
		Lane: lane,
	}

	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, *flags)
	c.Assert(err, ErrorMatches, "cannot specify a lane without setting transaction to \"all-snaps\"")
	c.Check(ts, IsNil)

	flags.Transaction = client.TransactionPerSnap
	ts, err = snapstate.Update(s.state, "some-snap", nil, s.user.ID, *flags)
	c.Assert(err, ErrorMatches, "cannot specify a lane without setting transaction to \"all-snaps\"")
	c.Check(ts, IsNil)
}

func (s *snapmgrTestSuite) TestConditionalAutoRefreshCreatesPreDownloadChange(c *C) {
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapst := &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snap.TypeApp),
	}
	snapstate.Set(s.state, "some-snap", snapst)

	chg := s.state.NewChange("auto-refresh", "test change")
	task := s.state.NewTask("conditional-auto-refresh", "test task")
	chg.AddTask(task)

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		Flags:    snapstate.Flags{IsAutoRefresh: true},
	}
	task.Set("snap-setup", snapsup)
	task.Set("snaps", map[string]*snapstate.RefreshCandidate{
		"some-snap": {
			SnapSetup: *snapsup,
		}})

	s.settle(c)

	chgs := s.state.Changes()
	// sort "auto-refresh" into first and "pre-download" into second
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})

	c.Assert(chgs, HasLen, 2)
	c.Assert(chgs[0].Err(), IsNil)
	c.Assert(chgs[0].Status(), Equals, state.DoneStatus)

	checkPreDownloadChange(c, chgs[1], "some-snap", snap.R(2))
}

func (s *snapmgrTestSuite) TestAutoRefreshCreatePreDownload(c *C) {
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapst := &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snap.TypeApp),
	}
	snapstate.Set(s.state, "some-snap", snapst)
	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		Flags:    snapstate.Flags{IsAutoRefresh: true},
	}

	ts, err := snapstate.DoInstall(s.state, snapst, snapsup, nil, 0, "", inUseCheck)

	var busyErr *snapstate.TimedBusySnapError
	c.Assert(errors.As(err, &busyErr), Equals, true)
	refreshInfo := busyErr.PendingSnapRefreshInfo()
	c.Check(refreshInfo, DeepEquals, &userclient.PendingSnapRefreshInfo{
		InstanceName:  "some-snap",
		TimeRemaining: snapstate.MaxInhibitionDuration(s.state),
	})

	tasks := ts.Tasks()
	c.Assert(tasks, HasLen, 1)
	c.Assert(tasks[0].Kind(), Equals, "pre-download-snap")
	c.Assert(tasks[0].Summary(), testutil.Contains, "Pre-download snap \"some-snap\" (2) from channel")
}

func (s *snapmgrTestSuite) testAutoRefreshRefreshInhibitNoticeRecorded(c *C, markerInterfaceConnected bool, warningFallback bool) {
	refreshAppsCheckCalled := 0
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		refreshAppsCheckCalled++
		if si.SnapID == "some-snap-id" {
			return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	var monitored int
	restore = snapstate.MockCgroupMonitorSnapEnded(func(string, chan<- string) error {
		monitored++
		return nil
	})
	defer restore()

	var connCheckCalled int
	restore = snapstate.MockHasActiveConnection(func(st *state.State, iface string) (bool, error) {
		connCheckCalled++
		c.Check(iface, Equals, "snap-refresh-observe")
		return markerInterfaceConnected, nil
	})
	defer restore()

	// let's add some random warnings
	s.state.Lock()
	s.state.Warnf("this is a random warning 1")
	s.state.Warnf("this is a random warning 2")
	s.state.Unlock()

	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	// Trigger autorefresh.Ensure().
	err := s.snapmgr.Ensure()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chgs := s.state.Changes()
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})
	c.Assert(chgs, HasLen, 2)
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoStatus)
	c.Check(chgs[0].Kind(), Equals, "auto-refresh")
	c.Check(chgs[0].Status(), Equals, state.DoStatus)
	// No notices or warnings are recorded until auto-refresh change is marked as ready.
	checkRefreshInhibitNotice(c, s.state, 0)
	// no "refresh inhibition" warnings recorded
	checkNoRefreshInhibitWarning(c, s.state)

	s.settle(c)

	// - Twice in soft-check of original auto-refresh (once per snap).
	// - Once in pre-download (for some-snap).
	// - Once in hard-check of original auto-refresh (for some-other-snap).
	c.Check(refreshAppsCheckCalled, Equals, 4)
	// Pre-downloaded snap is monitored.
	c.Check(monitored, Equals, 1)

	chgs = s.state.Changes()
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})
	c.Assert(chgs, HasLen, 2)
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoneStatus)
	// A continued auto-refresh shouldn't be created because snap is still monitored.
	c.Check(chgs[0].Kind(), Equals, "auto-refresh")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)

	// Aggregate notice and warning is recorded when auto-refresh change is marked as ready.
	checkRefreshInhibitNotice(c, s.state, 1)
	if warningFallback {
		checkRefreshInhibitWarning(c, s.state, []string{"some-snap"}, time.Time{})
	} else {
		checkNoRefreshInhibitWarning(c, s.state)
	}
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeRecorded(c *C) {
	s.enableRefreshAppAwarenessUX()
	const markerInterfaceConnected = true
	const warningFallback = false
	s.testAutoRefreshRefreshInhibitNoticeRecorded(c, markerInterfaceConnected, warningFallback)
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeRecordedWarningFallback(c *C) {
	s.enableRefreshAppAwarenessUX()
	const markerInterfaceConnected = false
	const warningFallback = true
	s.testAutoRefreshRefreshInhibitNoticeRecorded(c, markerInterfaceConnected, warningFallback)
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeRecordedWarningFallbackNoRAAUX(c *C) {
	const markerInterfaceConnected = false
	const warningFallback = false
	s.testAutoRefreshRefreshInhibitNoticeRecorded(c, markerInterfaceConnected, warningFallback)
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeRecordedOnPreDownloadOnly(c *C) {
	refreshAppsCheckCalled := 0
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		refreshAppsCheckCalled++
		if refreshAppsCheckCalled == 1 {
			return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	// Trigger autorefresh.Ensure().
	err := s.snapmgr.Ensure()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chgs := s.state.Changes()
	c.Assert(chgs, HasLen, 1)
	c.Check(chgs[0].Kind(), Equals, "pre-download")
	c.Check(chgs[0].Status(), Equals, state.DoStatus)
	// If all refresh candidates are blocked from auto-refresh and only a
	// pre-download change is created for those snaps we should still record
	// a notice.
	checkRefreshInhibitNotice(c, s.state, 1)

	s.settle(c)

	// - Once in soft-check of original auto-refresh.
	// - Once in pre-download.
	// - Once in soft-check of continued auto-refresh.
	// - Once in hard-check of continued auto-refresh.
	c.Check(refreshAppsCheckCalled, Equals, 4)

	chgs = s.state.Changes()
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})

	c.Assert(chgs, HasLen, 2)
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoneStatus)
	// Continued auto-refresh.
	c.Check(chgs[0].Kind(), Equals, "auto-refresh")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)

	// No more snaps are marked as inhibited after the continued auto-refresh
	// The set of inhibtied snaps changed to an empty set, so another notice is recorded.
	checkRefreshInhibitNotice(c, s.state, 2)
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeNotRecorded(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	// Trigger autorefresh.Ensure().
	err := s.snapmgr.Ensure()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chgs := s.state.Changes()
	c.Assert(chgs, HasLen, 1)
	c.Check(chgs[0].Kind(), Equals, "auto-refresh")
	c.Check(chgs[0].Status(), Equals, state.DoStatus)
	// No notices are recorded.
	checkRefreshInhibitNotice(c, s.state, 0)

	s.settle(c)

	chgs = s.state.Changes()
	c.Assert(chgs, HasLen, 1)
	c.Check(chgs[0].Kind(), Equals, "auto-refresh")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)
	// No notices are recorded because no snaps are blocked.
	checkRefreshInhibitNotice(c, s.state, 0)
}

func (s *snapmgrTestSuite) TestAutoRefreshRefreshInhibitNoticeRecordedOnce(c *C) {
	refreshAppsCheckCalled := 0
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		refreshAppsCheckCalled++
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	// Never close monitoring channel to avoid triggering a continued auto-refresh
	var monitored int
	restore = snapstate.MockCgroupMonitorSnapEnded(func(string, chan<- string) error {
		monitored++
		return nil
	})
	defer restore()

	// Setup test snaps
	s.state.Lock()
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})
	// Verify list is empty
	checkLastRecordedInhibitedSnaps(c, s.state, nil)
	s.state.Unlock()

	snapstate.CanAutoRefresh = func(*state.State) (bool, error) { return true, nil }
	// Trigger autorefresh.Ensure().
	err := s.snapmgr.Ensure()
	if errors.Is(err, advisor.ErrNotSupported) {
		c.Skip("bolt is not supported")
	}
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// First auto-refresh attempt
	s.settle(c)
	chgs := s.state.Changes()
	c.Assert(chgs, HasLen, 1)
	c.Check(chgs[0].Kind(), Equals, "pre-download")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)
	// If all refresh candidates are blocked from auto-refresh and only a
	// pre-download change is created for those snaps we should still record
	// a notice.
	checkRefreshInhibitNotice(c, s.state, 1)
	checkLastRecordedInhibitedSnaps(c, s.state, []string{"some-snap"})

	forceAutoRefresh := func() {
		s.state.Unlock()
		// Fake nextRefresh to now.
		s.snapmgr.MockNextRefresh(time.Now())
		// Fake that the retryRefreshDelay is over
		restore = snapstate.MockRefreshRetryDelay(1 * time.Millisecond)
		defer restore()
		time.Sleep(10 * time.Millisecond)
		// Trigger autorefresh.Ensure().
		c.Assert(s.snapmgr.Ensure(), IsNil)
		s.state.Lock()
	}

	// Force another auto-refresh to check that notice is not repeated
	// for the same set of inhibited snaps.
	forceAutoRefresh()
	s.settle(c)

	chgs = s.state.Changes()
	c.Assert(chgs, HasLen, 2)
	// Old pre-download
	c.Check(chgs[0].Kind(), Equals, "pre-download")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)
	// New pre-download
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoneStatus)
	// No new notices recorded because the set of inhibited snaps was not changed.
	checkRefreshInhibitNotice(c, s.state, 1)
	checkLastRecordedInhibitedSnaps(c, s.state, []string{"some-snap"})

	// Add another inhibited snap.
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: string(snap.TypeApp),
	})

	// Force another auto-refresh to check that notice is recorded because
	// the set of inhibited snaps changed.
	forceAutoRefresh()
	s.settle(c)

	chgs = s.state.Changes()
	c.Assert(chgs, HasLen, 3)
	// Old pre-download
	c.Check(chgs[0].Kind(), Equals, "pre-download")
	c.Check(chgs[0].Status(), Equals, state.DoneStatus)
	// Old pre-download
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoneStatus)
	// Latest pre-download
	c.Check(chgs[1].Kind(), Equals, "pre-download")
	c.Check(chgs[1].Status(), Equals, state.DoneStatus)
	// New notice occurrence is recorded because the set of inhibited snaps changed.
	checkLastRecordedInhibitedSnaps(c, s.state, []string{"some-snap", "some-other-snap"})
	checkRefreshInhibitNotice(c, s.state, 2)
}

func checkLastRecordedInhibitedSnaps(c *C, st *state.State, snaps []string) {
	var lastRecordedInhibitedSnaps map[string]bool
	err := st.Get("last-recorded-inhibited-snaps", &lastRecordedInhibitedSnaps)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		c.Fail()
	}
	c.Assert(len(lastRecordedInhibitedSnaps), Equals, len(snaps))
	for _, snap := range snaps {
		c.Assert(lastRecordedInhibitedSnaps[snap], Equals, true)
	}
}

func checkRefreshInhibitNotice(c *C, st *state.State, occurrences int) {
	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.RefreshInhibitNotice}})
	if occurrences == 0 {
		c.Assert(notices, HasLen, 0)
		return
	}
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "refresh-inhibit")
	c.Check(n["key"], Equals, "-")
	c.Check(n["occurrences"], Equals, float64(occurrences))
	c.Check(n["user-id"], Equals, nil)
}

func (s *snapmgrTestSuite) TestAutoRefreshBusySnapButOngoingPreDownload(c *C) {
	restore := snapstate.MockRefreshAppsCheck(func(si *snap.Info) error {
		return snapstate.NewBusySnapError(si, []int{123}, nil, nil)
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapst := &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snap.TypeApp),
	}
	snapstate.Set(s.state, "some-snap", snapst)
	snapsup := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		Flags:    snapstate.Flags{IsAutoRefresh: true},
	}

	// create ongoing pre-download
	chg := s.state.NewChange("pre-download", "")
	task := s.state.NewTask("pre-download-snap", "")
	task.Set("snap-setup", snapsup)
	task.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "some-snap"})
	chg.AddTask(task)

	// don't create a pre-download task if one exists w/ these statuses
	for _, status := range []state.Status{state.DoStatus, state.DoingStatus} {
		task.SetStatus(status)
		ts, err := snapstate.DoInstall(s.state, snapst, snapsup, nil, 0, "", inUseCheck)

		var busyErr *snapstate.TimedBusySnapError
		c.Assert(errors.As(err, &busyErr), Equals, true)
		refreshInfo := busyErr.PendingSnapRefreshInfo()
		c.Check(refreshInfo, DeepEquals, &userclient.PendingSnapRefreshInfo{
			InstanceName:  "some-snap",
			TimeRemaining: snapstate.MaxInhibitionDuration(s.state),
		})
		c.Assert(ts, IsNil)

		// reset modified state to avoid conflicts
		snapst.RefreshInhibitedTime = nil
		snapstate.Set(s.state, "some-snap", snapst)
	}

	// a "Done" pre-download is ignored since the auto-refresh it causes might also be done
	task.SetStatus(state.DoneStatus)
	ts, err := snapstate.DoInstall(s.state, snapst, snapsup, nil, 0, "", inUseCheck)
	c.Assert(err, FitsTypeOf, &snapstate.TimedBusySnapError{})
	c.Assert(ts.Tasks(), HasLen, 1)
}

func (s *snapmgrTestSuite) TestReRefreshCreatesPreDownloadChange(c *C) {
	s.o.TaskRunner().AddHandler("pre-download-snap", func(*state.Task, *tomb.Tomb) error { return nil }, nil)

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	snapst := &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snap.TypeApp),
	}
	snapstate.Set(s.state, "some-snap", snapst)
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		Flags:    snapstate.Flags{IsAutoRefresh: true},
	}

	chg := s.state.NewChange("auto-refresh", "test change")
	// rerefresh looks for snaps by iterating through the other tasks in the change
	otherTask := s.state.NewTask("download-snap", "other test task")
	otherTask.Set("snap-setup", snapsup)
	otherTask.JoinLane(s.state.NewLane())
	otherTask.SetStatus(state.DoneStatus)
	otherTask.SetClean()

	chg.AddTask(otherTask)
	rerefreshTask := s.state.NewTask("check-rerefresh", "rerefresh task")
	rerefreshTask.Set("rerefresh-setup", snapstate.ReRefreshSetup{
		Flags: &snapstate.Flags{IsAutoRefresh: true},
	})
	chg.AddTask(rerefreshTask)

	restore := snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, []*snapstate.RevisionOptions, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, *snapstate.UpdateTaskSets, error) {
		task := s.state.NewTask("test-pre-download", "test task")
		task.Set("snap-setup", snapsup)
		ts := state.NewTaskSet(task)
		return nil, &snapstate.UpdateTaskSets{PreDownload: []*state.TaskSet{ts}}, nil
	})
	defer restore()
	s.settle(c)

	chgs := s.state.Changes()
	// sort "auto-refresh" into first and "pre-download" into second
	sort.Slice(chgs, func(i, j int) bool {
		return chgs[i].Kind() < chgs[j].Kind()
	})

	c.Assert(chgs, HasLen, 2)
	c.Assert(chgs[0].Err(), IsNil)

	preDlChg := chgs[1]
	c.Assert(preDlChg.Err(), IsNil)
	c.Assert(preDlChg.Kind(), Equals, "pre-download")
	c.Assert(preDlChg.Summary(), Equals, "Pre-download \"some-snap\" for auto-refresh")
	c.Assert(preDlChg.Tasks(), HasLen, 1)
}

func (s *snapmgrTestSuite) TestDownloadTaskWaitsForPreDownload(c *C) {
	now := time.Now()
	restore := state.MockTime(now)
	defer restore()

	var notified bool
	restore = snapstate.MockAsyncPendingRefreshNotification(func(context.Context, *userclient.PendingSnapRefreshInfo) {
		notified = true
	})
	defer restore()

	var monitored bool
	restore = snapstate.MockCgroupMonitorSnapEnded(func(string, chan<- string) error {
		monitored = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	dlChg := s.state.NewChange("download", "download change")
	dlTask := s.state.NewTask("download-snap", "download task")
	dlTask.Set("snap-setup", snapsup)
	// wait until the pre-download is running
	dlTask.At(now.Add(time.Hour))
	dlChg.AddTask(dlTask)

	var downloadCalls int
	s.fakeStore.downloadCallback = func() {
		downloadCalls++

		switch downloadCalls {
		case 1:
			s.state.Lock()
			// schedule download to run while pre-download is running
			dlTask.At(time.Time{})
			s.state.Unlock()

			c.Assert(s.o.TaskRunner().Ensure(), IsNil)

			for i := 0; i < 5; i++ {
				<-time.After(time.Second)
				s.state.Lock()
				atTime := dlTask.AtTime()
				s.state.Unlock()
				if atTime.IsZero() {
					continue
				}

				s.state.Lock()
				defer s.state.Unlock()

				// the download task registers itself w/ the pre-download and retries
				c.Assert(atTime.Equal(now.Add(2*time.Minute)), Equals, true)
				var taskIDs []string
				c.Assert(preDlTask.Get("waiting-tasks", &taskIDs), IsNil)
				c.Assert(taskIDs, DeepEquals, []string{dlTask.ID()})
				return
			}

			c.Fatal("download task hasn't run")
		case 2:
			return
		default:
			c.Fatal("only expected 2 calls to the store")
		}
	}

	s.settle(c)

	c.Assert(downloadCalls, Equals, 2)
	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)
	c.Assert(dlTask.Status(), Equals, state.DoneStatus)
	c.Check(notified, Equals, false)
	c.Check(monitored, Equals, false)
}

func (s *snapmgrTestSuite) TestPreDownloadTaskContinuesAutoRefreshIfSoftCheckOk(c *C) {
	var softChecked bool
	restore := snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		c.Assert(info.InstanceName(), Equals, "foo")
		softChecked = true
		return nil
	})
	defer restore()

	var notified bool
	restore = snapstate.MockAsyncPendingRefreshNotification(func(context.Context, *userclient.PendingSnapRefreshInfo) {
		notified = true
	})
	defer restore()

	var monitored bool
	restore = snapstate.MockCgroupMonitorSnapEnded(func(string, chan<- string) error {
		monitored = true
		return nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: foo`, si)
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
		// if there's no downloadInfo, the download goes into a fallback behaviour of requesting it from the store and
		// makes it harder to check that we don't request refresh info form the store
		DownloadInfo: &snap.DownloadInfo{DownloadURL: "my-url"},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"foo": {SnapSetup: *snapsup},
	})

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)

	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)

	c.Check(softChecked, Equals, true)
	c.Check(notified, Equals, false)
	c.Check(monitored, Equals, false)

	autoRefreshChg := findChange(s.state, "auto-refresh")
	c.Assert(autoRefreshChg, NotNil)
	c.Assert(autoRefreshChg.Status(), Equals, state.DoneStatus)

	// check that the auto-refresh was completed without asking the store for refresh info
	c.Assert(s.fakeBackend.ops.Count("storesvc-snap-action"), Equals, 0)
	c.Assert(s.fakeStore.downloads, HasLen, 2)
}

func findChange(st *state.State, kind string) *state.Change {
	for _, chg := range st.Changes() {
		if chg.Kind() == kind {
			return chg
		}
	}

	return nil
}

func (s *snapmgrTestSuite) TestDownloadTaskMonitorsSnapStoppedAndNotifiesOnSoftCheckFail(c *C) {
	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: foo`, si)
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
		// if there's no downloadInfo, the download goes into a fallback behaviour of requesting it from the store and
		// makes it harder to check that we don't request refresh info form the store
		DownloadInfo: &snap.DownloadInfo{DownloadURL: "my-url"},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"foo": {SnapSetup: *snapsup},
	})

	var softChecked bool
	inhibited := true
	restore := snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		c.Assert(info.InstanceName(), Equals, "foo")
		softChecked = true
		if inhibited {
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	var notified bool
	restore = snapstate.MockAsyncPendingRefreshNotification(func(context.Context, *userclient.PendingSnapRefreshInfo) {
		notified = true
	})
	defer restore()

	var monitorSignal chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		c.Assert(name, Equals, "foo")
		monitorSignal = done
		return nil
	})
	defer restore()

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)

	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)
	c.Assert(s.fakeStore.downloads, HasLen, 1)
	c.Check(s.fakeStore.downloads[0].name, Equals, "foo")

	// the soft check failed so we notified and started monitoring
	c.Check(softChecked, Equals, true)
	c.Check(notified, Equals, true)
	c.Assert(monitorSignal, NotNil)

	var hints map[string]*snapstate.RefreshCandidate
	err := s.state.Get("refresh-candidates", &hints)
	c.Assert(err, IsNil)
	c.Assert(hints, HasLen, 1)
	c.Assert(hints["foo"].Monitored, Equals, true)

	monitored := s.state.Cached("monitored-snaps")
	c.Assert(monitored, FitsTypeOf, map[string]context.CancelFunc{})
	c.Assert(monitored.(map[string]context.CancelFunc)["foo"], NotNil)

	// signal snap has stopped and wait for pending goroutine to finish
	s.state.Unlock()
	inhibited = false
	monitorSignal <- "foo"

	waitForMonitoringEnd(s.state, c)

	s.state.Lock()
	defer s.state.Unlock()
	s.settle(c)

	autoRefreshChg := findChange(s.state, "auto-refresh")
	c.Assert(autoRefreshChg, NotNil)
	c.Assert(autoRefreshChg.Status(), Equals, state.DoneStatus)
	c.Assert(s.state.Cached("monitored-snaps"), IsNil)

	// the refresh-candidates are removed at the end of the change (see pruneRefreshCandidates)
	err = s.state.Get("refresh-candidates", &hints)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	// check that the auto-refresh was completed without asking the store for refresh info
	c.Assert(s.fakeBackend.ops.Count("storesvc-snap-action"), Equals, 0)
	c.Assert(s.fakeStore.downloads, HasLen, 2)
}

func (s *snapmgrTestSuite) TestDownloadTaskMonitorsRepeated(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: foo`, si)
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"foo": {SnapSetup: *snapsup},
	})

	var softChecked bool
	inhibited := true
	restore := snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		c.Assert(info.InstanceName(), Equals, "foo")
		softChecked = true
		if inhibited {
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	var notified bool
	restore = snapstate.MockAsyncPendingRefreshNotification(func(context.Context, *userclient.PendingSnapRefreshInfo) {
		notified = true
	})
	defer restore()

	var monitorSignal chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		c.Assert(name, Equals, "foo")
		monitorSignal = done
		return nil
	})
	defer restore()

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)

	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)
	// monitoring snap
	monitored := s.state.Cached("monitored-snaps")
	c.Assert(monitored, FitsTypeOf, map[string]context.CancelFunc{})
	c.Assert(monitored.(map[string]context.CancelFunc)["foo"], NotNil)
	c.Assert(notified, Equals, true)

	// waiting for the monitoring to end
	c.Check(s.state.Cached("monitored-snaps"), NotNil)
	c.Assert(findChange(s.state, "auto-refresh"), IsNil)

	// start a new pre-download which shouldn't start monitoring
	preDlChg = s.state.NewChange("pre-download", "pre-download change")
	preDlTask = s.state.NewTask("pre-download-snap", "pre-download task")
	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	// reset the watcher variables
	notified = false
	softChecked = false
	firstMonitorSignal := monitorSignal
	monitorSignal = nil

	s.settle(c)

	c.Check(softChecked, Equals, true)
	c.Check(notified, Equals, true)
	// didn't wait for snap to stop because there's already a goroutine doing it
	c.Check(monitorSignal, IsNil)

	// make sure nothing is left running
	s.state.Unlock()
	inhibited = false
	firstMonitorSignal <- "foo"
	waitForMonitoringEnd(s.state, c)
	s.state.Lock()

	s.settle(c)
}

func (s *snapmgrTestSuite) TestUnlinkMonitorSnapOnHardCheckFailure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})

	s.fakeStore.downloads = []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     "some-snap",
		target:   filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
	}}

	var notified bool
	restore := snapstate.MockAsyncPendingRefreshNotification(func(_ context.Context, pendingInfo *userclient.PendingSnapRefreshInfo) {
		c.Check(pendingInfo.InstanceName, Equals, "some-snap")
		c.Check(pendingInfo.TimeRemaining, Equals, snapstate.MaxInhibitionDuration(s.state))
		notified = true
	})
	defer restore()

	var monitorSignal chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		c.Check(name, Equals, "some-snap")
		monitorSignal = done
		return nil
	})
	defer restore()

	var check int
	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		check++
		c.Check(info.InstanceName(), Equals, "some-snap")

		switch check {
		case 1:
			return nil
		case 2:
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		default:
			return nil
		}
	})
	defer restore()

	updated, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	c.Check(updated, DeepEquals, []string{"some-snap"})
	c.Assert(tss, NotNil)
	c.Check(tss.Refresh, NotNil)
	c.Check(tss.PreDownload, IsNil)

	chg := s.state.NewChange("refresh", "test refresh")
	for _, ts := range tss.Refresh {
		chg.AddAll(ts)
	}

	s.settle(c)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	c.Check(notified, Equals, true)
	c.Check(check, Equals, 2)
	c.Check(monitorSignal, NotNil)

	monitored := s.state.Cached("monitored-snaps")
	c.Assert(monitored, FitsTypeOf, map[string]context.CancelFunc{})
	c.Assert(monitored.(map[string]context.CancelFunc)["some-snap"], NotNil)

	// cleanup leftover routines
	s.state.Unlock()
	monitorSignal <- "some-snap"
	waitForMonitoringEnd(s.state, c)

	s.state.Lock()
	s.settle(c)
}

func (s *snapmgrTestSuite) TestRefreshForcedOnRefreshInhibitionTimeout(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	instant := time.Now()
	pastInstant := instant.Add(-snapstate.MaxInhibitionDuration(s.state) * 2)
	// Add first snap
	si1 := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si1)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		// Pretend inhibition is overdue.
		RefreshInhibitedTime: &pastInstant,
	})
	// Add second snap to check that the list is being appended to properly.
	si2 := &snap.SideInfo{
		RealName: "some-other-snap",
		SnapID:   "some-other-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-other-snap`, si2)
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		// Pretend inhibition is overdue.
		RefreshInhibitedTime: &pastInstant,
	})

	s.fakeStore.downloads = []fakeDownload{
		{
			macaroon: s.user.StoreMacaroon,
			name:     "some-snap",
			target:   filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
		},
		{
			macaroon: s.user.StoreMacaroon,
			name:     "some-other-snap",
			target:   filepath.Join(dirs.SnapBlobDir, "some-other-snap_instance_11.snap"),
		},
	}

	var notified int
	restore := snapstate.MockAsyncPendingRefreshNotification(func(_ context.Context, pendingInfo *userclient.PendingSnapRefreshInfo) {
		c.Check(pendingInfo.TimeRemaining, Equals, time.Duration(0))
		notified++
	})
	defer restore()

	check := make(map[string]int, 2)
	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		check[info.InstanceName()]++

		switch check[info.InstanceName()] {
		case 1:
			return nil
		case 2:
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		default:
			return nil
		}
	})
	defer restore()

	updated, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	sort.Slice(updated, func(i, j int) bool {
		return updated[i] < updated[j]
	})
	c.Check(updated, DeepEquals, []string{"some-other-snap", "some-snap"})
	c.Assert(tss, NotNil)
	c.Check(tss.Refresh, NotNil)
	c.Check(tss.PreDownload, IsNil)

	chg := s.state.NewChange("refresh", "test refresh")
	for _, ts := range tss.Refresh {
		chg.AddAll(ts)
	}

	s.settle(c)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	var apiData map[string]interface{}
	c.Assert(chg.Get("api-data", &apiData), IsNil)
	refreshForced := apiData["refresh-forced"].([]interface{})
	sort.Slice(refreshForced, func(i, j int) bool {
		return refreshForced[i].(string) < refreshForced[j].(string)
	})
	c.Check(refreshForced, DeepEquals, []interface{}{"some-other-snap", "some-snap"})

	notices := s.state.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(notices, HasLen, 1)
	n := noticeToMap(c, notices[0])
	c.Check(n["type"], Equals, "change-update")
	c.Check(n["key"], Equals, chg.ID())
	// 3 status changes (Default -> Doing -> Done) + 2 forced refreshes
	c.Check(n["occurrences"], Equals, 5.0)

	c.Check(notified, Equals, 2)
	c.Check(check["some-snap"], Equals, 2)
	c.Check(check["some-other-snap"], Equals, 2)
}

func (s *snapmgrTestSuite) TestRefreshForcedOnRefreshInhibitionTimeoutError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	instant := time.Now()
	pastInstant := instant.Add(-snapstate.MaxInhibitionDuration(s.state) * 2)
	// Add snap
	si1 := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si1)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  si1.Revision,
		// Pretend inhibition is overdue.
		RefreshInhibitedTime: &pastInstant,
	})

	s.fakeStore.downloads = []fakeDownload{
		{
			macaroon: s.user.StoreMacaroon,
			name:     "some-snap",
			target:   filepath.Join(dirs.SnapBlobDir, "some-snap_instance_11.snap"),
		},
	}

	restore := snapstate.MockAsyncPendingRefreshNotification(func(_ context.Context, pendingInfo *userclient.PendingSnapRefreshInfo) {})
	defer restore()

	var checkAppRunning int
	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		checkAppRunning++
		// pass on soft-check, fail on hard-check in unlink-current-snap
		if checkAppRunning > 1 {
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	updated, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	c.Check(updated, DeepEquals, []string{"some-snap"})
	c.Assert(tss, NotNil)
	c.Check(tss.Refresh, NotNil)
	c.Check(tss.PreDownload, IsNil)

	chg := s.state.NewChange("refresh", "test refresh")
	for _, ts := range tss.Refresh {
		chg.AddAll(ts)
	}

	restore = snapstate.MockOnRefreshInhibitionTimeout(func(chg *state.Change, snapName string) error {
		return fmt.Errorf("boom!")
	})
	defer restore()

	s.settle(c)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Assert(checkAppRunning, Equals, 2)

	lock, err := osutil.NewFileLock(filepath.Join(s.fakeBackend.lockDir, "some-snap.lock"))
	c.Assert(err, IsNil)
	defer lock.Close()
	c.Assert(lock.TryLock(), IsNil)
}

func (s *snapmgrTestSuite) TestDeletedMonitoredMapIsCorrectlyDeleted(c *C) {
	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: foo`, si)
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"foo": {SnapSetup: *snapsup},
	})

	inhibited := true
	restore := snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		if inhibited {
			return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
		}
		return nil
	})
	defer restore()

	var monitorSignal chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		c.Check(name, Equals, "foo")
		monitorSignal = done
		return nil
	})
	defer restore()

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)
	c.Assert(preDlChg.Status(), Equals, state.DoneStatus)

	// unblock the monitoring routine which will delete the "monitored-snaps" map
	s.state.Unlock()

	// let the continuing logic create an auto-refresh change
	inhibited = false
	monitorSignal <- "foo"
	waitForMonitoringEnd(s.state, c)

	s.state.Lock()
	c.Assert(s.state.Cached("monitored-snaps"), IsNil)

	// start a 2nd task that checks the state for the map of monitored snap
	preDlChg = s.state.NewChange("pre-download", "pre-download change")
	preDlTask = s.state.NewTask("pre-download-snap", "pre-download task")
	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "foo"})
	preDlChg.AddTask(preDlTask)

	// so we go into the monitoring
	inhibited = true

	s.settle(c)
	c.Assert(preDlChg.Status(), Equals, state.DoneStatus)

	// wait until the 2nd auto-refresh starts
	s.state.Unlock()
	inhibited = false
	monitorSignal <- "foo"
	waitFor(s.state, c, func() bool { return s.state.Change("4") != nil })
	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(s.state.Change("4").Kind(), Equals, "auto-refresh")
	s.settle(c)

	c.Assert(s.state.Cached("monitored-snaps"), IsNil)
}

func waitFor(st *state.State, c *C, cond func() bool) {
	for i := 0; i < 5; i++ {
		st.Lock()
		condMet := cond()
		st.Unlock()
		if condMet {
			return
		}

		<-time.After(time.Second)
	}

	c.Fatal("condition wasn't met within 5 seconds")
}

func waitForMonitoringEnd(st *state.State, c *C) {
	waitFor(st, c, func() bool {
		return findChange(st, "auto-refresh") != nil
	})
}

func (s *snapmgrTestSuite) TestPreDownloadWithIgnoreRunningRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"some-snap": {SnapSetup: *snapsup},
	})

	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {})
	defer restore()

	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		c.Assert(info.InstanceName(), Equals, "some-snap")
		return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
	})
	defer restore()

	var monitorSignal chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		c.Check(name, Equals, "some-snap")
		monitorSignal = done
		return nil
	})
	defer restore()

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "some-snap"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)

	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)
	// check there's still a goroutine monitoring the snap
	monitored := s.state.Cached("monitored-snaps")
	c.Assert(monitored, FitsTypeOf, map[string]context.CancelFunc{})
	c.Assert(monitored.(map[string]context.CancelFunc)["some-snap"], NotNil)

	updated, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, 0, &snapstate.Flags{IgnoreRunning: true})
	c.Assert(err, IsNil)
	c.Assert(updated, DeepEquals, []string{"some-snap"})
	c.Assert(tss, NotNil)

	chg := s.state.NewChange("refresh", "refresh change")
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(chg.Err(), IsNil)

	// wait for the monitoring to be cleared
	for i := 0; i < 5; i++ {
		s.state.Unlock()
		<-time.After(2 * time.Second)
		s.state.Lock()

		// the monitoring has stopped but no auto-refresh was or will be attempted
		if monitored := s.state.Cached("monitored-snaps"); monitored == nil {
			break
		}
	}

	// the monitoring has stopped but no auto-refresh was or will be attempted
	c.Check(s.state.Cached("monitored-snaps"), IsNil, Commentf("monitoring wasn't cleared after 10 seconds"))
	c.Check(s.state.Cached("auto-refresh-continue-attempt"), IsNil)
	lastRefresh, err := s.snapmgr.LastRefresh()
	c.Assert(err, IsNil)
	c.Check(lastRefresh.IsZero(), Equals, true)

	// if the snap stops, we don't block when using the channel to notify
	monitorSignal <- "some-snap"
}

func (s *snapmgrTestSuite) TestPreDownloadCleansSnapDownloads(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: sequence.SnapSequence{
			Revisions: []*sequence.RevisionSideState{{Snap: si}},
		},
		Current: si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"some-snap": {SnapSetup: *snapsup},
	})

	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {})
	defer restore()

	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		c.Assert(info.InstanceName(), Equals, "some-snap")
		return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
	})
	defer restore()

	// mock that snap is monitored (i.e. non-nil abort channel)
	mockAbortChans := map[string]interface{}{"some-snap": func() {}}
	s.state.Cache("monitored-snaps", mockAbortChans)

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "some-snap"})
	preDlChg.AddTask(preDlTask)

	cleanSnapDownloadsCalled := false
	restore = snapstate.MockCleanSnapDownloads(func(st *state.State, snapName string) error {
		if snapName == "some-snap" {
			cleanSnapDownloadsCalled = true
		}
		return nil
	})
	defer restore()

	s.settle(c)
	c.Check(cleanSnapDownloadsCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestRefreshNoRelatedMonitoring(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	s.state.Cache("monitored-snaps", map[string]context.CancelFunc{"other-snap": func() {}})

	_, tss, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, nil, 0, &snapstate.Flags{IgnoreRunning: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh change")
	for _, ts := range tss {
		chg.AddAll(ts)
	}
	s.settle(c)

	c.Assert(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestMonitoringIsPersistedAndRestored(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}
	// simulate a restart with an in-progress monitoring
	s.state.Set("refresh-candidates", map[string]*snapstate.RefreshCandidate{
		"some-snap": {SnapSetup: *snapsup, Monitored: true},
	})

	var notified bool
	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {})
	defer restore()

	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		return nil
	})
	defer restore()

	var stopMonitor chan<- string
	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		stopMonitor = done
		c.Check(name, Equals, "some-snap")
		return nil
	})
	defer restore()

	s.state.Unlock()
	defer s.state.Lock()
	af := snapstate.NewAutoRefresh(s.state)
	err := af.Ensure()
	c.Check(err, IsNil)

	// restores monitoring but doesn't notify again
	c.Assert(stopMonitor, NotNil)
	c.Assert(notified, Equals, false)

	s.state.Lock()
	aborts := s.state.Cached("monitored-snaps").(map[string]context.CancelFunc)
	abort := aborts["some-snap"]
	s.state.Unlock()
	c.Assert(abort, NotNil)

	stopMonitor <- "some-snap"
	waitForMonitoringEnd(s.state, c)

	s.state.Lock()
	defer s.state.Unlock()
	s.settle(c)

	// the refresh-candidates are removed at the end of the change (see pruneRefreshCandidates)
	var hints map[string]*snapstate.RefreshCandidate
	err = s.state.Get("refresh-candidates", &hints)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}

func (s *snapmgrTestSuite) TestNoMonitoringIfOnlyOtherRefreshCandidates(c *C) {
	s.testNoMonitoringWithCands(c, map[string]*snapstate.RefreshCandidate{
		"other-snap": {
			SnapSetup: snapstate.SnapSetup{
				SideInfo: &snap.SideInfo{
					Revision: snap.R(11),
				},
			},
		},
	})
}

func (s *snapmgrTestSuite) TestNoMonitoringIfNoRefreshCandidates(c *C) {
	s.testNoMonitoringWithCands(c, nil)
}

func (s *snapmgrTestSuite) testNoMonitoringWithCands(c *C, cands map[string]*snapstate.RefreshCandidate) {
	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(1),
	}
	snaptest.MockSnap(c, `name: some-snap`, si)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
	})
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "some-snap",
			Revision: snap.R(2),
		},
		Flags: snapstate.Flags{IsAutoRefresh: true},
	}

	// cands shouldn't include a refresh candidate for this snap so we can simulate
	// that the candidate was reverted before the pre-download task runs
	s.state.Set("refresh-candidates", cands)

	var notified bool
	restore := snapstate.MockAsyncPendingRefreshNotification(func(ctx context.Context, refreshInfo *userclient.PendingSnapRefreshInfo) {
		notified = true
	})
	defer restore()

	var inhibited bool
	restore = snapstate.MockRefreshAppsCheck(func(info *snap.Info) error {
		inhibited = true
		return snapstate.NewBusySnapError(info, []int{123}, nil, nil)
	})
	defer restore()

	restore = snapstate.MockCgroupMonitorSnapEnded(func(name string, done chan<- string) error {
		return nil
	})
	defer restore()

	buf, restore := logger.MockLogger()
	defer restore()

	preDlChg := s.state.NewChange("pre-download", "pre-download change")
	preDlTask := s.state.NewTask("pre-download-snap", "pre-download task")

	preDlTask.Set("snap-setup", snapsup)
	preDlTask.Set("refresh-info", &userclient.PendingSnapRefreshInfo{InstanceName: "some-snap"})
	preDlChg.AddTask(preDlTask)

	s.settle(c)

	// task finished without waiting for monitoring
	c.Assert(preDlTask.Status(), Equals, state.DoneStatus)
	c.Assert(s.state.Cached("monitored-snap"), IsNil)
	c.Assert(buf.String(), testutil.Contains, `cannot get refresh candidate for "some-snap" (possibly reverted): nothing to refresh`)

	// we didn't notify since there's no candidate to refresh to
	c.Assert(notified, Equals, false)
	c.Assert(inhibited, Equals, true)
}

func (s *snapmgrTestSuite) testUpdateDowngradeBlockedByOtherChanges(c *C, old, new string, revert bool) error {
	si1 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(2),
	}
	si3 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(3),
	}

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		var version string
		switch name {
		case "snapd":
			if (revert && si.Revision.N == 1) || (!revert && si.Revision.N == 2) {
				version = old
			} else if (revert && si.Revision.N == 2) || si.Revision.N == 3 {
				version = new
			} else {
				return nil, fmt.Errorf("unexpected revision for test")
			}
		default:
			version = "1.0"
		}
		return &snap.Info{
			SuggestedName: name,
			Version:       version,
			Architectures: []string{"all"},
			SideInfo:      *si,
		}, nil
	})
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("unrelated", "...")
	chg.AddTask(st.NewTask("task0", "..."))

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si1, &si2, &si3}),
		TrackingChannel: "latest/stable",
		Current:         si2.Revision,
	})

	var err error
	if revert {
		_, err = snapstate.Revert(s.state, "snapd", snapstate.Flags{}, "")
	} else {
		_, err = snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Revision: snap.R(3)}, s.user.ID, snapstate.Flags{})
	}
	return err
}

func (s *snapmgrTestSuite) TestUpdateDowngradeBlockedByOtherChanges(c *C) {
	err := s.testUpdateDowngradeBlockedByOtherChanges(c, "2.57.1", "2.56", false)
	c.Assert(err, ErrorMatches, `other changes in progress \(conflicting change "unrelated"\), change "snapd downgrade" not allowed until they are done`)
}

func (s *snapmgrTestSuite) TestUpdateDowngradeBlockedByOtherChangesAlsoWhenEmpty(c *C) {
	err := s.testUpdateDowngradeBlockedByOtherChanges(c, "2.57.1", "", false)
	c.Assert(err, ErrorMatches, `other changes in progress \(conflicting change "unrelated"\), change "snapd downgrade" not allowed until they are done`)
}

func (s *snapmgrTestSuite) TestUpdateDowngradeNotBlockedByOtherChanges(c *C) {
	err := s.testUpdateDowngradeBlockedByOtherChanges(c, "2.57.1", "2.58", false)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestRevertBlockedByOtherChanges(c *C) {
	// Swap values for revert case
	err := s.testUpdateDowngradeBlockedByOtherChanges(c, "2.56", "2.57.1", true)
	c.Assert(err, ErrorMatches, `other changes in progress \(conflicting change "unrelated"\), change "snapd downgrade" not allowed until they are done`)
}

func (s *snapmgrTestSuite) TestRevertBlockedByOtherChangesAlsoWhenEmpty(c *C) {
	// Swap values for revert case
	err := s.testUpdateDowngradeBlockedByOtherChanges(c, "2.58", "2.57.1", true)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) testUpdateNotAllowedWhileDowngrading(c *C, old, new string, revert bool) error {
	si1 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(1),
	}
	si2 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(2),
	}
	si3 := snap.SideInfo{
		RealName: "snapd",
		SnapID:   "snapd-id",
		Channel:  "latest",
		Revision: snap.R(3),
	}

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	}

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		var version string
		switch name {
		case "snapd":
			if (revert && si.Revision.N == 1) || (!revert && si.Revision.N == 2) {
				version = old
			} else if (revert && si.Revision.N == 2) || si.Revision.N == 3 {
				version = new
			} else {
				return nil, fmt.Errorf("unexpected revision for test")
			}
		default:
			version = "1.0"
		}
		return &snap.Info{
			SuggestedName: name,
			Version:       version,
			Architectures: []string{"all"},
			SideInfo:      *si,
		}, nil
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si1, &si2, &si3}),
		TrackingChannel: "latest/stable",
		Current:         si2.Revision,
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si}),
		TrackingChannel: "other-chanel/stable",
		Current:         si.Revision,
	})

	var err error
	var ts *state.TaskSet
	if revert {
		ts, err = snapstate.Revert(s.state, "snapd", snapstate.Flags{}, "")
	} else {
		ts, err = snapstate.Update(s.state, "snapd", &snapstate.RevisionOptions{Revision: snap.R(3)}, s.user.ID, snapstate.Flags{})
	}
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh-snap", "refresh snapd")
	chg.AddAll(ts)

	_, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	return err
}

func (s *snapmgrTestSuite) TestUpdateNotAllowedWhileDowngrading(c *C) {
	err := s.testUpdateNotAllowedWhileDowngrading(c, "2.57.1", "2.56", false)
	c.Assert(err, ErrorMatches, `snapd downgrade in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestUpdateNotAllowedWhileDowngradingAndWhenEmpty(c *C) {
	err := s.testUpdateNotAllowedWhileDowngrading(c, "2.57.1", "", false)
	c.Assert(err, ErrorMatches, `snapd downgrade in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestUpdateAllowedWhileUpgrading(c *C) {
	err := s.testUpdateNotAllowedWhileDowngrading(c, "2.57.1", "2.58", false)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateNotAllowedWhileRevertDowngrading(c *C) {
	err := s.testUpdateNotAllowedWhileDowngrading(c, "2.56", "2.57.1", true)
	c.Assert(err, ErrorMatches, `snapd downgrade in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestUpdateAllowedWhileRevertUpgrading(c *C) {
	err := s.testUpdateNotAllowedWhileDowngrading(c, "2.58", "2.57.1", true)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestUpdateSetsRestartBoundaries(c *C) {
	siGadget := snap.SideInfo{
		RealName: "brand-gadget",
		SnapID:   "brand-gadget-id",
		Revision: snap.R(7),
	}
	siSomeSnap := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(DefaultModel())
	defer r()
	snapstate.Set(s.state, "brand-gadget", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siGadget}),
		Current:  siGadget.Revision,
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&siSomeSnap}),
		Current:  siSomeSnap.Revision,
	})

	ts1, err := snapstate.Update(s.state, "brand-gadget", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	// only ensure that SetEssentialSnapsRestartBoundaries was actually called, we don't
	// test that all restart boundaries were set, one is enough
	linkSnap1 := ts1.MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnap1, NotNil)

	var boundary restart.RestartBoundaryDirection
	c.Check(linkSnap1.Get("restart-boundary", &boundary), IsNil)

	// also ensure that it's not set for normal snaps
	ts2, err := snapstate.Update(s.state, "some-snap", nil, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	linkSnap2 := ts2.MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnap2, NotNil)
	c.Check(linkSnap2.Get("restart-boundary", &boundary), ErrorMatches, `no state entry for key "restart-boundary"`)
}

func (s *snapmgrTestSuite) testUpdateManyRevOptsOrder(c *C, isThrottled map[string]bool) {
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "snap-c", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snap-c", SnapID: "snap-c-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	var requestSnapToAction map[string]*store.SnapAction
	sto := customStore{fakeStore: s.fakeStore}
	sto.customSnapAction = func(ctx context.Context, cs []*store.CurrentSnap, sa []*store.SnapAction, aq store.AssertionQuery, us *auth.UserState, ro *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
		if len(sa) == 0 {
			return nil, nil, nil
		}

		var actionResult []store.SnapActionResult
		for _, action := range sa {
			requestSnapToAction[action.InstanceName] = action

			// throttle refresh requests if this is an auto-refresh
			if isThrottled[action.SnapID] && ro.Scheduled {
				continue
			}
			info, err := s.fakeStore.lookupRefresh(refreshCand{snapID: action.SnapID})
			c.Assert(err, IsNil)
			actionResult = append(actionResult, store.SnapActionResult{Info: info})
		}

		return actionResult, nil, nil
	}
	snapstate.ReplaceStore(s.state, &sto)

	nameToRevOpts := map[string]*snapstate.RevisionOptions{
		"some-snap": {
			Revision:       snap.R(111),
			ValidationSets: []snapasserts.ValidationSetKey{"1", "1.1"},
		},
		"some-other-snap": {
			Revision:       snap.R(222),
			ValidationSets: []snapasserts.ValidationSetKey{"2", "2.2"},
		},
		"snap-c": {
			Revision:       snap.R(333),
			ValidationSets: []snapasserts.ValidationSetKey{"3", "3.3"},
		},
	}
	getRevOpts := func(names []string) (revOpts []*snapstate.RevisionOptions) {
		for _, name := range names {
			revOpts = append(revOpts, nameToRevOpts[name])
		}
		return revOpts
	}

	testOrder := func(names []string) {
		requestSnapToAction = make(map[string]*store.SnapAction, 3)
		revOpts := getRevOpts(names)
		flags := snapstate.Flags{IsAutoRefresh: isThrottled != nil}
		_, _, err := snapstate.UpdateMany(context.Background(), s.state, names, revOpts, 0, &flags)
		c.Assert(err, IsNil)
		c.Check(requestSnapToAction, NotNil)
		for name, action := range requestSnapToAction {
			c.Check(action.Revision, Equals, nameToRevOpts[name].Revision, Commentf("snap %q sent revision is incorrect", name))
			c.Check(action.ValidationSets, DeepEquals, nameToRevOpts[name].ValidationSets, Commentf("snap %q sent validation sets are incorrect", name))
		}
	}

	// let's check all permutations for good measure
	testOrder([]string{"some-snap", "some-other-snap", "snap-c"})
	testOrder([]string{"some-snap", "snap-c", "some-other-snap"})

	testOrder([]string{"some-other-snap", "some-snap", "snap-c"})
	testOrder([]string{"some-other-snap", "snap-c", "some-snap"})

	testOrder([]string{"snap-c", "some-snap", "some-other-snap"})
	testOrder([]string{"snap-c", "some-other-snap", "some-snap"})
}

func (s *snapmgrTestSuite) TestSnapdRefreshForRemodel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Set snapd state in the system: currently rev 44 installed, but also
	// 22 is in the system (simply to avoid trying to reach the store for
	// that rev when we want to refresh to it).
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(44)},
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(22)}}),
		Current:  snap.R(44),
		SnapType: "app",
	})

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		var version string
		switch name {
		case "snapd":
			switch si.Revision.N {
			case 22:
				version = "1.0"
			default:
				version = "2.0"
			}
		default:
			version = "1.0"
		}
		return &snap.Info{
			SuggestedName: name,
			Version:       version,
			Architectures: []string{"all"},
			SideInfo:      *si,
		}, nil
	})
	defer restore()

	// This is part of a remodeling change
	chg := s.state.NewChange("remodel", "...")
	chg.SetStatus(state.DoStatus)

	// Update to snapd rev 22, which is an older revision. This checks that
	// things are fine in that case and that there is no conflict detected
	// as we are doing this from the remodel change.
	opts := &snapstate.RevisionOptions{Channel: "stable", Revision: snap.R(22)}
	_, err := snapstate.UpdateWithDeviceContext(s.state, "snapd", opts, s.user.ID, snapstate.Flags{}, nil, nil, chg.ID())
	c.Check(err, IsNil)

	// But there is a conflict if this update is not part of the remodel change.
	_, err = snapstate.UpdateWithDeviceContext(s.state, "snapd", opts, s.user.ID, snapstate.Flags{}, nil, nil, "")
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Check(err, ErrorMatches, "remodeling in progress, no other changes allowed until this is done")
}

func (s *snapmgrTestSuite) TestUpdatePathWithDeviceContextLocalRevisionMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(8)}
	_, err := snapstate.UpdatePathWithDeviceContext(s.state, si, "path", "some-snap", &snapstate.RevisionOptions{Revision: snap.R(7)}, s.user.ID, snapstate.Flags{}, nil, nil, "")
	c.Check(err, ErrorMatches, `cannot install local snap "some-snap": 7 != 8 \(revision mismatch\)`)
}

func (s *snapmgrTestSuite) TestInstallPathWithDeviceContextLocalRevisionMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{RealName: "some-snap", Revision: snap.R(8)}
	_, err := snapstate.InstallPathWithDeviceContext(s.state, si, "path", "some-snap", &snapstate.RevisionOptions{Revision: snap.R(7)}, s.user.ID, snapstate.Flags{}, nil, nil, "")
	c.Check(err, ErrorMatches, `cannot install local snap "some-snap": 7 != 8 \(revision mismatch\)`)
}

func (s *snapmgrTestSuite) TestUpdateManyRevOptsOrder(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.testUpdateManyRevOptsOrder(c, nil)
}

func (s *snapmgrTestSuite) TestRefreshCandidatesThrottledRevOptsRemap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// simulate existing monitored refresh hint from older refresh
	cands := map[string]*snapstate.RefreshCandidate{
		"some-other-snap": {Monitored: true},
		"snap-c":          {Monitored: true},
	}
	s.state.Set("refresh-candidates", &cands)

	// simulate store throttling some snaps' during auto-refresh
	isThrottled := map[string]bool{
		"some-other-snap-id": true,
		"snap-c-id":          true,
	}

	s.testUpdateManyRevOptsOrder(c, isThrottled)
}

func (s *snapmgrTestSuite) TestUpdateManyFilteredForSnapsNotInOldHints(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// simulate existing refresh hint from older refresh with
	// some-other-snap being monitored
	cands := map[string]*snapstate.RefreshCandidate{
		"some-other-snap": {Monitored: true},
	}
	s.state.Set("refresh-candidates", &cands)

	storeSnapIDs := map[string]bool{}
	storeCalled := 0
	sto := customStore{fakeStore: s.fakeStore}
	sto.customSnapAction = func(ctx context.Context, cs []*store.CurrentSnap, sa []*store.SnapAction, aq store.AssertionQuery, us *auth.UserState, ro *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
		storeCalled++

		var actionResult []store.SnapActionResult
		for _, action := range sa {
			storeSnapIDs[action.SnapID] = true
			info, err := s.fakeStore.lookupRefresh(refreshCand{snapID: action.SnapID})
			c.Assert(err, IsNil)
			actionResult = append(actionResult, store.SnapActionResult{Info: info})
		}

		return actionResult, nil, nil
	}
	snapstate.ReplaceStore(s.state, &sto)

	names := []string{"some-snap"}
	filterCalled := 0
	filter := func(info *snap.Info, s *snapstate.SnapState) bool {
		filterCalled++
		c.Check(info, NotNil)
		c.Check(info.InstanceName(), Equals, "some-snap")
		c.Check(s, NotNil)
		return true
	}
	flags := snapstate.Flags{IsAutoRefresh: true}

	updatedNames, tss, err := snapstate.UpdateManyFiltered(context.Background(), s.state, names, nil, 0, filter, &flags, "")
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	c.Check(updatedNames, DeepEquals, []string{"some-snap"})

	c.Check(storeCalled, Equals, 1)
	c.Check(filterCalled, Equals, 1)

	// check that only passed names are updated
	c.Check(storeSnapIDs, HasLen, 1)
	c.Check(storeSnapIDs["some-snap-id"], Equals, true)
	c.Check(storeSnapIDs["some-other-snap-id"], Equals, false)

	// check that refresh-candidates in the state were updated
	var newCands map[string]*snapstate.RefreshCandidate
	err = s.state.Get("refresh-candidates", &newCands)
	c.Assert(err, IsNil)

	c.Assert(newCands, HasLen, 2)
	c.Check(newCands["some-snap"], NotNil)
	// a monitored snap was preserved
	c.Assert(newCands["some-other-snap"], NotNil)
	c.Check(newCands["some-other-snap"].Monitored, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyFilteredNotAutoRefreshNoRetry(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(1)},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})

	// simulate existing refresh hint from older refresh with
	// some-other-snap being monitored
	cands := map[string]*snapstate.RefreshCandidate{
		"some-other-snap": {Monitored: true},
	}
	s.state.Set("refresh-candidates", &cands)

	storeSnapIDs := map[string]bool{}
	storeCalled := 0
	sto := customStore{fakeStore: s.fakeStore}
	sto.customSnapAction = func(ctx context.Context, cs []*store.CurrentSnap, sa []*store.SnapAction, aq store.AssertionQuery, us *auth.UserState, ro *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
		storeCalled++

		var actionResult []store.SnapActionResult
		for _, action := range sa {
			storeSnapIDs[action.SnapID] = true

			// throttle some-other-snap to trigger retry
			if action.SnapID == "some-other-snap-id" {
				continue
			}

			info, err := s.fakeStore.lookupRefresh(refreshCand{snapID: action.SnapID})
			c.Assert(err, IsNil)
			actionResult = append(actionResult, store.SnapActionResult{Info: info})
		}

		return actionResult, nil, nil
	}
	snapstate.ReplaceStore(s.state, &sto)

	names := []string{"some-snap", "some-other-snap"}
	flags := snapstate.Flags{IsAutoRefresh: false}

	updatedNames, tss, err := snapstate.UpdateManyFiltered(context.Background(), s.state, names, nil, 0, nil, &flags, "")
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	c.Check(updatedNames, DeepEquals, []string{"some-snap"})

	// no retry should be attempted because this is not an auto-refresh
	c.Check(storeCalled, Equals, 1)

	c.Check(storeSnapIDs, HasLen, 2)
	c.Check(storeSnapIDs["some-snap-id"], Equals, true)
	c.Check(storeSnapIDs["some-other-snap-id"], Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateSnapdAndSnapPullingNewBase(c *C) {
	// classic = true, avoid messing with snapd services
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	fakeAutoConnect := func(task *state.Task, _ *tomb.Tomb) error {
		st := task.State()
		st.Lock()
		defer st.Unlock()
		kind := task.Kind()
		status := task.Status()
		snapsup, err := snapstate.TaskSnapSetup(task)
		if err != nil {
			return err
		}
		if status == state.DoingStatus {
			if snapsup.Type == snap.TypeSnapd {
				si := snapsup.SideInfo
				// snapd is installed by now
				snaptest.MockSnapCurrent(c, `
name: snapd
version: snapdVer
type: snapd
`, snapsup.SideInfo)
				snapstate.Set(st, "snapd", &snapstate.SnapState{
					SnapType:        "snapd",
					Active:          true,
					Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
					Current:         si.Revision,
					Flags:           snapstate.Flags{Required: true},
					TrackingChannel: si.Channel,
				})
			}
			// fake restart
			restart.MockPending(st, restart.RestartUnset)
			if err := snapstate.FinishRestart(task, snapsup); err != nil {
				panic(err)
			}
		}
		return s.fakeBackend.ForeignTask(kind, status, snapsup, nil)
	}

	s.o.TaskRunner().AddHandler("auto-connect", fakeAutoConnect, fakeAutoConnect)

	snapstate.Set(s.state, "core22", nil) // NOTE: core22 is not installed.
	snapstate.Set(s.state, "some-snap-with-new-base", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: "some-snap-with-new-base",
				SnapID:   "some-snap-with-new-base-id",
				Revision: snap.R(1),
			},
		}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: "snapd",
				SnapID:   "snapd-without-version-id",
				Revision: snap.R(1),
			},
		}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})

	updated, taskSets, err := snapstate.UpdateMany(context.Background(), s.state,
		[]string{"snapd", "some-snap-with-new-base"},
		[]*snapstate.RevisionOptions{{
			Channel: "latest/stable",
		}, {
			Channel: "some-channel",
		}},
		s.user.ID, &snapstate.Flags{
			IgnoreRunning: true,
			Transaction:   client.TransactionPerSnap, // XXX: it is unclear if this represents the real-world behavior.
		})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh-snap", "refresh snapd and some other snap pulling in a base")

	for _, taskSet := range taskSets {
		chg.AddAll(taskSet)
	}

	// XXX: This mimics newChange from daemon/api.go
	if updated != nil {
		chg.Set("snap-names", updated)
	}

	s.state.EnsureBefore(0)

	s.settle(c)

	didDownloadCore22 := false

	for _, fakeOp := range s.fakeBackend.ops {
		if fakeOp.op == "storesvc-download" && fakeOp.name == "core22" {
			didDownloadCore22 = true
		}
	}

	c.Assert(didDownloadCore22, Equals, true, Commentf("core22 was *not* downloaded"))
}

// prepare a refresh/install of essential and non-essential snaps, optionally
// with an app depending on the model base, to test that the update doesn't make
// apps wait for the reboot required by the essential snaps.
func (s *snapmgrTestSuite) setupSplitRefreshAppDependsOnModelBase(c *C, core18BasedApp bool) (names []string, infos []*snap.SideInfo) {
	restore := release.MockOnClassic(true)
	s.AddCleanup(restore)
	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	s.AddCleanup(restore)

	snaps := []string{"snapd", "kernel", "core18", "gadget", "some-base", "some-base-snap"}
	if core18BasedApp {
		// add an app that depends on the model base so test a cross set dependency
		snaps = append(snaps, "some-snap-with-core18-base")
		// we expect an app to have to wait for a base, shorten the retry timeout
		restore = snapstate.MockPrerequisitesRetryTimeout(200 * time.Millisecond)
		s.AddCleanup(restore)
	} else {
		snaps = append(snaps, "some-snap")
	}

	types := map[string]string{
		"snapd":                      "snapd",
		"core18":                     "base",
		"gadget":                     "gadget",
		"kernel":                     "kernel",
		"some-base":                  "base",
		"some-base-snap":             "app",
		"some-snap-with-core18-base": "app",
		"some-snap":                  "app",
	}
	snapIds := map[string]string{
		"snapd":                      "snapd-snap-id",
		"core18":                     "core18-snap-id",
		"gadget":                     "gadget-core18-id",
		"kernel":                     "kernel-id",
		"some-base":                  "some-base-id",
		"some-snap-with-core18-base": "some-snap-with-core18-base",
		"some-base-snap":             "some-base-snap-id",
		"some-snap":                  "some-snap-id",
	}
	bases := map[string]string{
		// create a dependency between the two task sets
		"some-snap-with-core18-base": "core18",
		"some-base-snap":             "some-base",
	}

	var paths []string
	for _, sn := range snaps {
		yaml := fmt.Sprintf("name: %s\nversion: 1.0\nepoch: 1\ntype: %s", sn, types[sn])
		if base, ok := bases[sn]; ok {
			yaml += fmt.Sprintf("\nbase: %s", base)
		}

		oldSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(1)}
		newSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(11)}

		path, _ := snaptest.MakeTestSnapInfoWithFiles(c, yaml, nil, newSi)
		paths = append(paths, path)
		infos = append(infos, newSi)

		snaptest.MockSnap(c, yaml, oldSi)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oldSi}),
			Current:         oldSi.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})
	}

	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
		"kernel": true,
		"gadget": true,
	}
	s.fakeBackend.linkSnapMaybeReboot = true

	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	return paths, infos
}

func (s *snapmgrTestSuite) TestUpdateManySplitEssentialWithSharedBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	sharedBase := true
	_, infos := s.setupSplitRefreshAppDependsOnModelBase(c, sharedBase)

	var snaps []string
	for _, info := range infos {
		snaps = append(snaps, info.RealName)
	}

	chg := s.state.NewChange("refresh", fmt.Sprintf("refresh %v", snaps))
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, snaps, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(affected, testutil.DeepUnsortedMatches, snaps)

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	c.Check(chg.CheckTaskDependencies(), IsNil)

	s.settle(c)

	checkRerefresh := true
	s.checkSplitRefreshWithSharedBase(c, chg, checkRerefresh)
}

func (s *snapmgrTestSuite) TestOldStyleAutoRefreshSplitEssentialWithSharedBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := release.MockOnClassic(true)
	defer restore()
	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	restore = snapstate.MockPrerequisitesRetryTimeout(200 * time.Millisecond)
	defer restore()

	snaps := []string{"snapd", "kernel", "core18", "gadget", "some-snap-with-core18-base"}

	types := map[string]string{
		"snapd":                      "snapd",
		"core18":                     "base",
		"gadget":                     "gadget",
		"kernel":                     "kernel",
		"some-snap-with-core18-base": "app",
	}
	snapIds := map[string]string{
		"snapd":                      "snapd-snap-id",
		"core18":                     "core18-snap-id",
		"gadget":                     "gadget-core18-id",
		"kernel":                     "kernel-id",
		"some-snap-with-core18-base": "some-snap-with-core18-base",
	}
	bases := map[string]string{
		// create a dependency between the two task sets
		"some-snap-with-core18-base": "core18",
	}

	for _, sn := range snaps {
		yaml := fmt.Sprintf("name: %s\nversion: 1.0\nepoch: 1\ntype: %s", sn, types[sn])
		if base, ok := bases[sn]; ok {
			yaml += fmt.Sprintf("\nbase: %s", base)
		}

		oldSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(1)}
		newSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(11)}

		snaptest.MakeTestSnapInfoWithFiles(c, yaml, nil, newSi)

		snaptest.MockSnap(c, yaml, oldSi)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oldSi}),
			Current:         oldSi.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})
	}

	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
		"kernel": true,
		"gadget": true,
	}
	s.fakeBackend.linkSnapMaybeReboot = true

	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	chg := s.state.NewChange("auto-refresh", fmt.Sprintf("auto-refresh %v", snaps))
	affected, tss, err := snapstate.AutoRefresh(context.Background(), s.state)
	c.Assert(err, IsNil)
	c.Assert(affected, testutil.DeepUnsortedMatches, snaps)

	for _, ts := range tss.Refresh {
		chg.AddAll(ts)
	}

	c.Check(chg.CheckTaskDependencies(), IsNil)

	s.settle(c)

	// some-snap-with-core18-base depends on the base but the prereq code only waits
	// for the base link-snap so it can complete before the reboot
	for _, snap := range []string{"snapd", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for %q to be \"Do\": %s", t.Kind(), snap, t.Status()))
	}

	// check that the rerefresh task is done because the essential tasks are
	// ignored
	rerefreshTask := findLastTask(chg, "check-rerefresh")
	c.Assert(rerefreshTask, NotNil, Commentf("cannot find check-rerefresh task"))
	c.Assert(rerefreshTask.Status(), Equals, state.DoneStatus)

	t := findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus, Commentf("expected kernel's link-snap to be waiting for restart"))
	s.mockRestartAndSettle(c, chg)

	for _, snap := range []string{"kernel", "gadget", "core18", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be in \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) checkSplitRefreshWithSharedBase(c *C, chg *state.Change, checkRerefresh bool) {
	// some-snap-with-core18-base depends on the base but the prereq code only waits
	// for the base link-snap so it can complete before the reboot
	for _, snap := range []string{"snapd", "some-base", "some-base-snap", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for %q to be \"Do\": %s", t.Kind(), snap, t.Status()))
	}

	if checkRerefresh {
		// check that the rerefresh task is done because the essential tasks are
		// ignored
		rerefreshTask := findLastTask(chg, "check-rerefresh")
		c.Assert(rerefreshTask, NotNil, Commentf("cannot find check-rerefresh task"))
		c.Assert(rerefreshTask.Status(), Equals, state.DoneStatus)
	}

	t := findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus, Commentf("expected kernel's link-snap to be waiting for restart"))
	s.mockRestartAndSettle(c, chg)

	for _, snap := range []string{"kernel", "gadget", "core18", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be in \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestUpdateManySplitEssentialWithoutSharedBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	sharedBase := false
	_, infos := s.setupSplitRefreshAppDependsOnModelBase(c, sharedBase)

	var snaps []string
	for _, info := range infos {
		snaps = append(snaps, info.RealName)
	}

	chg := s.state.NewChange("refresh", fmt.Sprintf("refresh %v", snaps))
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, snaps, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(affected, testutil.DeepUnsortedMatches, snaps)

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	c.Check(chg.CheckTaskDependencies(), IsNil)

	s.settle(c)

	for _, snap := range []string{"snapd", "some-snap", "some-base", "some-base-snap"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for snap %q to be done: %s", t.Kind(), snap, t.Status()))
	}

	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for snap %q to be do: %s", t.Kind(), snap, t.Status()))
	}

	// check that the check-rerefresh task completed because it only considers
	// non-essential snap refreshes and those can finish before the reboot
	var rerefreshTask *state.Task
	for _, ts := range tss {
		if len(ts.Tasks()) == 1 && ts.Tasks()[0].Kind() == "check-rerefresh" {
			rerefreshTask = ts.Tasks()[0]
			break
		}
	}
	c.Assert(rerefreshTask, NotNil, Commentf("cannot find check-rerefresh task"))
	c.Assert(rerefreshTask.Status(), Equals, state.DoneStatus)

	t := findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus, Commentf("expected kernel's link-snap to be waiting for restart"))

	// waiting for reboot
	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for snap %q to be do: %s", t.Kind(), snap, t.Status()))
	}
	s.mockRestartAndSettle(c, chg)

	// completed after restart
	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for snap %q to be done: %s", t.Kind(), snap, t.Status()))
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestSplitRefreshUsesSameTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	restore = release.MockOnClassic(true)
	defer restore()

	s.fakeBackend.linkSnapRebootFor = map[string]bool{"kernel": true}
	s.fakeBackend.linkSnapMaybeReboot = true

	s.o.TaskRunner().AddHandler("fail", func(*state.Task, *tomb.Tomb) error {
		return fmt.Errorf("expected error")
	}, nil)

	snaps := []string{"kernel", "some-snap"}
	types := map[string]string{
		"kernel":    "kernel",
		"some-snap": "app",
	}
	snapIds := map[string]string{
		"kernel":    "kernel-id",
		"some-snap": "some-snap-id",
	}

	for _, sn := range snaps {
		si := &snap.SideInfo{RealName: sn, Revision: snap.R(1), SnapID: snapIds[sn]}
		snapYaml := fmt.Sprintf("name: %s\nversion: 1.2.3\ntype: %s", sn, types[sn])
		snaptest.MockSnap(c, snapYaml, si)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:         si.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})
	}

	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state,
		snaps, nil, s.user.ID, &snapstate.Flags{NoReRefresh: true, Transaction: client.TransactionAllSnaps})
	c.Assert(err, IsNil)
	c.Assert(affected, testutil.DeepUnsortedMatches, snaps)

	// fail the kernel refresh at the end
	ts, err := snapstate.MaybeFindTasksetForSnap(tss, "kernel")
	c.Assert(err, IsNil)
	lastTask := ts.MaybeEdge(snapstate.EndEdge)
	failTask := s.state.NewTask("fail", "")
	failTask.JoinLane(tss[0].Tasks()[0].Lanes()[0])
	failTask.WaitFor(lastTask)
	ts.AddTask(failTask)

	chg := s.state.NewChange("refresh", fmt.Sprintf("refresh %v", snaps))
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	for _, ts := range tss {
		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{1})
		}
	}

	s.settle(c)

	t := findTaskForSnap(c, chg, "auto-connect", "some-snap")
	c.Assert(t.Status(), Equals, state.DoneStatus)

	t = findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus)

	s.mockRestartAndSettle(c, chg)

	for _, name := range []string{"kernel", "some-snap"} {
		t := findTaskForSnap(c, chg, "auto-connect", name)
		c.Assert(t.Status(), Equals, state.UndoneStatus)
	}

	c.Check(chg.Status(), Equals, state.ErrorStatus)
}

func (s *snapmgrTestSuite) TestSplitEssentialSnapUpdates(c *C) {
	updatesToNames := func(updates []snapstate.SnapUpdate) []string {
		names := make([]string, 0, len(updates))
		for _, up := range updates {
			names = append(names, up.Setup.InstanceName())
		}
		return names
	}

	types := map[string]string{
		"snapd":          "snapd",
		"core18":         "base",
		"gadget":         "gadget",
		"kernel":         "kernel",
		"some-base":      "base",
		"some-base-snap": "app",
		"some-snap":      "app",
	}

	type testcase struct {
		modelBase         string
		snaps             []string
		bases             map[string]string
		essentialSnaps    []string
		nonEssentialSnaps []string
	}

	tcs := []testcase{
		{
			modelBase: "core18",
			snaps:     []string{"snapd", "kernel", "core18", "gadget", "some-snap", "some-base", "some-base-snap"},
			bases:     map[string]string{"some-base-snap": "some-base", "some-snap": "core18"},
			// core18 is in the essential group because it's the model base
			essentialSnaps:    []string{"snapd", "kernel", "core18", "gadget"},
			nonEssentialSnaps: []string{"some-snap", "some-base", "some-base-snap"},
		},
		{
			modelBase:      "core22",
			snaps:          []string{"snapd", "kernel", "core18", "gadget", "some-snap", "some-base", "some-base-snap"},
			bases:          map[string]string{"some-base-snap": "some-base", "some-snap": "core18"},
			essentialSnaps: []string{"snapd", "kernel", "gadget"},
			// core18 is in the non-essential group w/ its dependency bc it's not the model base
			nonEssentialSnaps: []string{"some-snap", "some-base", "some-base-snap", "core18"},
		},
		{
			modelBase: "core18",
			snaps:     []string{"snapd", "some-snap", "some-base", "some-base-snap"},
			bases:     map[string]string{"some-base-snap": "some-base", "some-snap": "core18"},
			// snapd is in the non-essential taskset because there are no other essential snaps
			nonEssentialSnaps: []string{"some-snap", "some-base", "some-base-snap", "snapd"},
		},
		{
			modelBase: "core18",
			snaps:     []string{"snapd", "some-snap", "core18"},
			bases:     map[string]string{"some-snap": "core18"},
			// no kernel/gadget so snapd and core18 can be refreshed with the app
			nonEssentialSnaps: []string{"some-snap", "core18", "snapd"},
		},
	}

	for _, tc := range tcs {
		updates := make([]snapstate.SnapUpdate, 0, len(tc.snaps))
		for _, sn := range tc.snaps {
			updates = append(updates, snapstate.SnapUpdate{
				Setup: snapstate.SnapSetup{
					SideInfo: &snap.SideInfo{RealName: sn, Revision: snap.R(1), SnapID: sn + "-id"},
					Type:     snap.Type(types[sn]),
					Base:     tc.bases[sn],
				},
			})
		}

		ctx := &snapstatetest.TrivialDeviceContext{DeviceModel: ModelWithBase(tc.modelBase)}
		essential, nonEssential := snapstate.SplitEssentialUpdates(ctx, updates)
		c.Assert(updatesToNames(essential), testutil.DeepUnsortedMatches, tc.essentialSnaps)
		c.Assert(updatesToNames(nonEssential), testutil.DeepUnsortedMatches, tc.nonEssentialSnaps)
	}
}

type hybridContentStore struct {
	*fakeStore
	state *state.State
}

func (s hybridContentStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	sars, _, err := s.fakeStore.SnapAction(ctx, currentSnaps, actions, assertQuery, user, opts)
	if err != nil {
		return nil, nil, err
	}

	var res []store.SnapActionResult
	for _, s := range sars {
		info := s.Info
		switch info.InstanceName() {
		case "snap-content-plug":
			info.Plugs = map[string]*snap.PlugInfo{
				"some-plug": {
					Snap:      info,
					Name:      "shared-content",
					Interface: "content",
					Attrs: map[string]interface{}{
						"default-provider": "snap-content-slot",
						"content":          "shared-content",
					},
				},
			}
		case "snap-content-slot":
			info.Slots = map[string]*snap.SlotInfo{
				"some-slot": {
					Snap:      info,
					Name:      "shared-content",
					Interface: "content",
					Attrs: map[string]interface{}{
						"content": "shared-content",
					},
				},
			}
			// default provider depends on the model base
			info.Base = "core18"
		}
		res = append(res, store.SnapActionResult{Info: info})
	}
	return res, nil, err
}

func (s *snapmgrTestSuite) TestSplitRefreshWithDefaultProviderDependingOnModelBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, hybridContentStore{fakeStore: s.fakeStore, state: s.state})
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	restore := release.MockOnClassic(true)
	defer restore()
	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()
	restore = snapstate.MockPrerequisitesRetryTimeout(200 * time.Millisecond)
	defer restore()

	snaps := []string{"snapd", "kernel", "core18", "gadget", "snap-content-plug"}
	types := map[string]string{
		"snapd":             "snapd",
		"core18":            "base",
		"gadget":            "gadget",
		"kernel":            "kernel",
		"snap-content-plug": "app",
	}
	snapIds := map[string]string{
		"snapd":             "snapd-snap-id",
		"core18":            "core18-snap-id",
		"gadget":            "gadget-core18-id",
		"kernel":            "kernel-id",
		"snap-content-plug": "snap-content-plug-id",
	}

	for _, sn := range snaps {
		yaml := fmt.Sprintf("name: %s\nversion: 1.0\nepoch: 1\ntype: %s", sn, types[sn])

		oldSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(1)}
		newSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(11)}

		snaptest.MakeTestSnapInfoWithFiles(c, yaml, nil, newSi)

		snaptest.MockSnap(c, yaml, oldSi)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oldSi}),
			Current:         oldSi.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})
	}

	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
		"kernel": true,
		"gadget": true,
	}
	s.fakeBackend.linkSnapMaybeReboot = true

	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	chg := s.state.NewChange("refresh", fmt.Sprintf("refresh %v", snaps))
	affected, tss, err := snapstate.UpdateMany(context.Background(), s.state, snaps, nil, s.user.ID, &snapstate.Flags{NoReRefresh: true})
	c.Assert(err, IsNil)
	c.Assert(affected, testutil.DeepUnsortedMatches, snaps)

	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	// the plug waits for the default provider which in turn waits for core18 to complete its link-snap (but not reboot)
	// that needs to wait for a reboot (not tested here, but in ifacestate)
	for _, snap := range []string{"snapd", "snap-content-plug", "snap-content-slot"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for snap %q to be done: %s", t.Kind(), snap, t.Status()))
	}

	// waiting for reboot
	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for snap %q to be do: %s", t.Kind(), snap, t.Status()))
	}

	t := findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus, Commentf("expected kernel's link-snap to be waiting for restart"))

	s.mockRestartAndSettle(c, chg)

	// completed after restart
	for _, snap := range []string{"kernel", "gadget", "core18", "snap-content-slot"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for snap %q to be done: %s", t.Kind(), snap, t.Status()))
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func (s *snapmgrTestSuite) TestAutoRefreshSplitRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := release.MockOnClassic(true)
	defer restore()

	restore = snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer restore()

	restore = snapstate.MockPrerequisitesRetryTimeout(200 * time.Millisecond)
	defer restore()

	snaps := []string{"snapd", "kernel", "core18", "gadget", "some-snap-with-core18-base"}
	types := map[string]string{
		"snapd":                      "snapd",
		"core18":                     "base",
		"gadget":                     "gadget",
		"kernel":                     "kernel",
		"some-snap-with-core18-base": "app",
	}
	snapIds := map[string]string{
		"snapd":                      "snapd-snap-id",
		"core18":                     "core18-snap-id",
		"gadget":                     "gadget-core18-id",
		"kernel":                     "kernel-id",
		"some-snap-with-core18-base": "some-snap-with-core18-base",
	}
	bases := map[string]string{
		// create a dependency between the two task sets
		"some-snap-with-core18-base": "core18",
	}

	chg := s.state.NewChange("auto-refresh", "test change")
	task := s.state.NewTask("conditional-auto-refresh", "mock conditional auto refresh task")
	chg.AddTask(task)

	cands := make(map[string]*snapstate.RefreshCandidate, len(snaps))
	for _, sn := range snaps {
		yaml := fmt.Sprintf("name: %s\nversion: 1.0\nepoch: 1\ntype: %s", sn, types[sn])
		if base, ok := bases[sn]; ok {
			yaml += fmt.Sprintf("\nbase: %s", base)
		}

		oldSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(1)}
		newSi := &snap.SideInfo{RealName: sn, SnapID: snapIds[sn], Revision: snap.R(11)}

		snaptest.MakeTestSnapInfoWithFiles(c, yaml, nil, newSi)

		snaptest.MockSnap(c, yaml, oldSi)
		snapstate.Set(s.state, sn, &snapstate.SnapState{
			Active:          true,
			Sequence:        snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{oldSi}),
			Current:         oldSi.Revision,
			TrackingChannel: "latest/stable",
			SnapType:        types[sn],
		})

		cands[sn] = &snapstate.RefreshCandidate{
			SnapSetup: snapstate.SnapSetup{
				SideInfo: newSi,
				Version:  sn + "Ver",
				Base:     bases[sn],
				Type:     snap.Type(types[sn]),
				Flags:    snapstate.Flags{IsAutoRefresh: true},
			},
		}
	}

	s.fakeBackend.linkSnapRebootFor = map[string]bool{
		"core18": true,
		"kernel": true,
		"gadget": true,
	}
	s.fakeBackend.linkSnapMaybeReboot = true

	s.o.TaskRunner().AddHandler("update-gadget-assets",
		func(task *state.Task, tomb *tomb.Tomb) error {
			task.State().Lock()
			defer task.State().Unlock()
			chg := task.Change()
			chg.Set("gadget-restart-required", true)
			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	s.o.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error { return nil },
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	task.Set("snaps", cands)

	s.settle(c)

	// some-snap-with-core18-base depends on the base but the prereq code only waits
	// for the base link-snap so it can complete before the reboot
	for _, snap := range []string{"snapd", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	for _, snap := range []string{"kernel", "gadget", "core18"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoStatus, Commentf("expected task %q for %q to be \"Do\": %s", t.Kind(), snap, t.Status()))
	}

	// check that the rerefresh task is done because the essential tasks are
	// ignored
	rerefreshTask := findLastTask(chg, "check-rerefresh")
	c.Assert(rerefreshTask, NotNil, Commentf("cannot find check-rerefresh task"))
	c.Assert(rerefreshTask.Status(), Equals, state.DoneStatus)

	t := findTaskForSnap(c, chg, "link-snap", "kernel")
	c.Assert(t.Status(), Equals, state.WaitStatus, Commentf("expected kernel's link-snap to be waiting for restart"))
	s.mockRestartAndSettle(c, chg)

	for _, snap := range []string{"kernel", "gadget", "core18", "some-snap-with-core18-base"} {
		t := findTaskForSnap(c, chg, "auto-connect", snap)
		c.Assert(t.Status(), Equals, state.DoneStatus, Commentf("expected task %q for %q to be in \"Done\": %s", t.Kind(), snap, t.Status()))
	}

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
}

func findTaskForSnap(c *C, chg *state.Change, kind, snap string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Kind() != kind {
			continue
		}

		snapsup, err := snapstate.TaskSnapSetup(t)
		c.Assert(err, IsNil)
		if snapsup.SnapName() == snap {
			return t
		}
	}

	c.Fatalf("couldn't find %q task for %q", kind, snap)
	return nil
}

func (s *snapmgrTestSuite) TestUpdateStateConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state})

	_, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Check(err, testutil.ErrorIs, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestUpdateStateConflictRemoved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.ReplaceStore(s.state, sneakyStore{fakeStore: s.fakeStore, state: s.state, remove: true})

	_, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Check(err, testutil.ErrorIs, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `snap "some-snap" has changes in progress`)
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsBackToPrevRevision(c *C) {
	const (
		snapName    = "some-snap"
		instanceKey = "key"
		snapID      = "some-snap-id"
		channel     = "channel-for-components"
	)

	components := []string{"test-component", "kernel-modules-component"}

	currentSnapRev := snap.R(11)
	prevSnapRev := snap.R(7)
	instanceName := snap.InstanceName(snapName, instanceKey)

	sort.Strings(components)

	compNameToType := func(name string) snap.ComponentType {
		typ := strings.TrimSuffix(name, "-component")
		if typ == name {
			c.Fatalf("unexpected component name %q", name)
		}
		return snap.ComponentType(typ)
	}

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Fatalf("unexpected call to snapResourcesFn")
		return nil
	}

	// we start without the auxiliary store info (or with an older one)
	c.Check(snapstate.AuxStoreInfoFilename(snapID), testutil.FileAbsent)

	currentSI := snap.SideInfo{
		RealName: snapName,
		Revision: currentSnapRev,
		SnapID:   snapID,
		Channel:  channel,
	}
	snaptest.MockSnapInstance(c, instanceName, fmt.Sprintf("name: %s", snapName), &currentSI)

	restore := snapstate.MockRevisionDate(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// right now, we don't expect to hit the store for this case. we might if we
	// choose to start checking the store for an updated list of compatible
	// components.
	snapstate.ReplaceStore(s.state, &storetest.Store{})

	if instanceKey != "" {
		tr := config.NewTransaction(s.state)
		tr.Set("core", "experimental.parallel-instances", true)
		tr.Commit()
	}

	prevSI := snap.SideInfo{
		RealName: snapName,
		Revision: prevSnapRev,
		SnapID:   snapID,
		Channel:  channel,
	}

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&prevSI, &currentSI})

	for i, comp := range components {
		err := seq.AddComponentForRevision(prevSnapRev, &sequence.ComponentState{
			SideInfo: &snap.ComponentSideInfo{
				Component: naming.NewComponentRef(snapName, comp),
				Revision:  snap.R(i + 1),
			},
			CompType: compNameToType(comp),
		})
		c.Assert(err, IsNil)

		err = seq.AddComponentForRevision(currentSnapRev, &sequence.ComponentState{
			SideInfo: &snap.ComponentSideInfo{
				Component: naming.NewComponentRef(snapName, comp),
				Revision:  snap.R(i + 2),
			},
			CompType: compNameToType(comp),
		})
		c.Assert(err, IsNil)
	}

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, info *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component:         csi.Component,
			Type:              compNameToType(csi.Component.ComponentName),
			Version:           "1.0",
			ComponentSideInfo: *csi,
		}, nil
	}))

	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active:          true,
		Sequence:        seq,
		Current:         currentSI.Revision,
		SnapType:        "app",
		TrackingChannel: channel,
		InstanceKey:     instanceKey,
	})

	ts, err := snapstate.Update(s.state, instanceName, &snapstate.RevisionOptions{
		Revision: prevSnapRev,
	}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	// check unlink-reason
	unlinkTask := findLastTask(chg, "unlink-current-snap")
	c.Assert(unlinkTask, NotNil)
	var unlinkReason string
	unlinkTask.Get("unlink-reason", &unlinkReason)
	c.Check(unlinkReason, Equals, "refresh")

	// local modifications, edge must be set
	te := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	c.Assert(te.Kind(), Equals, "prepare-snap")

	s.settle(c)

	c.Assert(chg.Err(), IsNil, Commentf("change tasks:\n%s", printTasks(chg.Tasks())))

	var expected fakeOps
	for i, compName := range components {
		csi := snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(i + 1),
		}

		if strings.HasPrefix(compName, string(snap.KernelModulesComponent)) {
			expected = append(expected, fakeOp{
				op: "setup-kernel-modules-components",
				// note that currentComps is empty here. this test ensures that
				// we don't accidentally consider components that were already
				// installed with a previous revision of a snap, when refreshing
				// to that revision
				currentComps: nil,
				compsToInstall: []*snap.ComponentSideInfo{{
					Component: naming.NewComponentRef(snapName, compName),
					Revision:  csi.Revision,
				}},
			})
		}
	}

	expected = append(expected, fakeOp{
		op:   "remove-snap-aliases",
		name: instanceName,
	})

	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        instanceName,
			inhibitHint: "refresh",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, instanceName, currentSnapRev.String()),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, instanceName, prevSnapRev.String()),
			old:  filepath.Join(dirs.SnapMountDir, instanceName, currentSnapRev.String()),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, instanceName),
		},
	}...)

	expected = append(expected, fakeOps{
		{
			op:    "setup-profiles:Doing",
			name:  instanceName,
			revno: prevSnapRev,
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: snapName,
				SnapID:   snapID,
				Channel:  channel,
				Revision: prevSnapRev,
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, instanceName, prevSnapRev.String()),
		},
	}...)

	for i, compName := range components {
		expected = append(expected, fakeOp{
			op:   "link-component",
			path: snap.ComponentMountDir(compName, snap.R(i+1), instanceName),
		})
	}

	expected = append(expected, fakeOps{
		{
			op:    "auto-connect:Doing",
			name:  instanceName,
			revno: prevSnapRev,
		},
		{
			op: "update-aliases",
		},
		{
			op:    "cleanup-trash",
			name:  instanceName,
			revno: prevSnapRev,
		},
	}...)

	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	task := ts.Tasks()[1]

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: channel,
		UserID:  s.user.ID,

		SnapPath:  filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%v.snap", instanceName, prevSnapRev)),
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		Version:   "some-snapVer",
		PlugsOnly: true,
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
		InstanceKey: instanceKey,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: snapName,
		Revision: prevSnapRev,
		Channel:  channel,
		SnapID:   snapID,
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, instanceName, &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.LastRefreshTime, NotNil)
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence.Revisions, HasLen, 2)

	// link-snap should put the revision we refreshed to at the end of the
	// sequence. in this case, swapping their positions.
	c.Assert(snapst.Sequence.Revisions[1], DeepEquals, seq.Revisions[0])
	c.Assert(snapst.Sequence.Revisions[0], DeepEquals, seq.Revisions[1])
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsRunThrough(c *C) {
	const (
		undo                  = false
		refreshAppAwarenessUX = false
		instanceKey           = ""
	)
	s.testUpdateWithComponentsRunThrough(c, instanceKey, []string{"test-component", "kernel-modules-component"}, refreshAppAwarenessUX, undo)
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsRunThroughNoComponents(c *C) {
	const (
		undo                  = false
		refreshAppAwarenessUX = false
		instanceKey           = ""
	)
	s.testUpdateWithComponentsRunThrough(c, instanceKey, nil, refreshAppAwarenessUX, undo)
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsRunThroughUndo(c *C) {
	const (
		undo                  = true
		refreshAppAwarenessUX = true
		instanceKey           = ""
	)
	s.testUpdateWithComponentsRunThrough(c, instanceKey, []string{"test-component", "kernel-modules-component"}, refreshAppAwarenessUX, undo)
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsRunThroughInstanceKey(c *C) {
	const (
		undo                  = false
		refreshAppAwarenessUX = true
		instanceKey           = "key"
	)
	s.testUpdateWithComponentsRunThrough(c, instanceKey, []string{"test-component", "kernel-modules-component"}, refreshAppAwarenessUX, undo)
}

func (s *snapmgrTestSuite) TestUpdateWithComponentsRunThroughInstanceKeyUndo(c *C) {
	const (
		undo                  = true
		refreshAppAwarenessUX = true
		instanceKey           = "key"
	)
	s.testUpdateWithComponentsRunThrough(c, instanceKey, []string{"test-component", "kernel-modules-component"}, refreshAppAwarenessUX, undo)
}

func (s *snapmgrTestSuite) testUpdateWithComponentsRunThrough(c *C, instanceKey string, components []string, refreshAppAwarenessUX, undo bool) {
	if refreshAppAwarenessUX {
		s.enableRefreshAppAwarenessUX()
	}

	const (
		snapName = "some-snap"
		snapID   = "some-snap-id"
		channel  = "channel-for-components"
	)

	currentSnapRev := snap.R(7)
	newSnapRev := snap.R(11)
	instanceName := snap.InstanceName(snapName, instanceKey)

	sort.Strings(components)

	compNameToType := func(name string) snap.ComponentType {
		typ := strings.TrimSuffix(name, "-component")
		if typ == name {
			c.Fatalf("unexpected component name %q", name)
		}
		return snap.ComponentType(typ)
	}

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.InstanceName(), DeepEquals, instanceName)
		var results []store.SnapResourceResult
		for i, compName := range components {
			results = append(results, store.SnapResourceResult{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: "http://example.com/" + compName,
				},
				Name:      compName,
				Revision:  i + 2,
				Type:      fmt.Sprintf("component/%s", compNameToType(compName)),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			})
		}
		return results
	}

	// we start without the auxiliary store info (or with an older one)
	c.Check(snapstate.AuxStoreInfoFilename(snapID), testutil.FileAbsent)

	si := snap.SideInfo{
		RealName: snapName,
		Revision: currentSnapRev,
		SnapID:   snapID,
		Channel:  channel,
	}
	snaptest.MockSnapInstance(c, instanceName, fmt.Sprintf("name: %s", snapName), &si)
	fi, err := os.Stat(snap.MountFile(instanceName, si.Revision))
	c.Assert(err, IsNil)

	refreshedDate := fi.ModTime()

	restore := snapstate.MockRevisionDate(nil)
	defer restore()

	now, err := time.Parse(time.RFC3339, "2021-06-10T10:00:00Z")
	c.Assert(err, IsNil)

	restore = snapstate.MockTimeNow(func() time.Time {
		return now
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	if instanceKey != "" {
		tr := config.NewTransaction(s.state)
		tr.Set("core", "experimental.parallel-instances", true)
		tr.Commit()
	}

	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{&si})

	for i, comp := range components {
		err := seq.AddComponentForRevision(currentSnapRev, &sequence.ComponentState{
			SideInfo: &snap.ComponentSideInfo{
				Component: naming.NewComponentRef(snapName, comp),
				Revision:  snap.R(i + 1),
			},
			CompType: compNameToType(comp),
		})
		c.Assert(err, IsNil)
	}

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, info *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		return &snap.ComponentInfo{
			Component:         csi.Component,
			Type:              compNameToType(csi.Component.ComponentName),
			Version:           "1.0",
			ComponentSideInfo: *csi,
		}, nil
	}))

	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active:          true,
		Sequence:        seq,
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: channel,
		InstanceKey:     instanceKey,
	})

	ts, err := snapstate.Update(s.state, instanceName, nil, s.user.ID, snapstate.Flags{
		NoReRefresh: true,
	})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	if undo {
		last := lastWithLane(ts.Tasks())
		c.Assert(last, NotNil)

		terr := s.state.NewTask("error-trigger", "provoking total undo")
		terr.WaitFor(last)
		terr.JoinLane(last.Lanes()[0])
		chg.AddTask(terr)
	}

	// check unlink-reason
	unlinkTask := findLastTask(chg, "unlink-current-snap")
	c.Assert(unlinkTask, NotNil)
	var unlinkReason string
	unlinkTask.Get("unlink-reason", &unlinkReason)
	c.Check(unlinkReason, Equals, "refresh")

	// local modifications, edge must be set
	te := ts.MaybeEdge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(te, NotNil)
	c.Assert(te.Kind(), Equals, "validate-snap")

	s.settle(c)

	if undo {
		c.Assert(chg.Err(), NotNil, Commentf("change tasks:\n%s", printTasks(chg.Tasks())))
	} else {
		c.Assert(chg.Err(), IsNil, Commentf("change tasks:\n%s", printTasks(chg.Tasks())))
	}

	expected := fakeOps{
		{
			op: "storesvc-snap-action",
			curSnaps: []store.CurrentSnap{{
				InstanceName:    instanceName,
				SnapID:          snapID,
				Revision:        currentSnapRev,
				TrackingChannel: channel,
				RefreshedDate:   refreshedDate,
				Epoch:           snap.E("1*"),
			}},
			userID: 1,
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "refresh",
				InstanceName: instanceName,
				SnapID:       snapID,
				Channel:      channel,
				Flags:        store.SnapActionEnforceValidation,
			},
			revno:  newSnapRev,
			userID: 1,
		},
		{
			op:   "storesvc-download",
			name: snapName,
		},
		{
			op:    "validate-snap:Doing",
			name:  instanceName,
			revno: newSnapRev,
		},
		{
			op:  "current",
			old: filepath.Join(dirs.SnapMountDir, instanceName, currentSnapRev.String()),
		},
		{
			op:   "open-snap-file",
			path: filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%v.snap", instanceName, newSnapRev)),
			sinfo: snap.SideInfo{
				RealName: snapName,
				SnapID:   snapID,
				Channel:  channel,
				Revision: newSnapRev,
			},
		},
		{
			op:    "setup-snap",
			name:  instanceName,
			path:  filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%v.snap", instanceName, newSnapRev)),
			revno: newSnapRev,
		},
	}

	for i, compName := range components {
		csi := snap.ComponentSideInfo{
			Component: naming.NewComponentRef(snapName, compName),
			Revision:  snap.R(i + 2),
		}

		containerName := fmt.Sprintf("%s+%s", instanceName, compName)
		filename := fmt.Sprintf("%s_%v.comp", containerName, csi.Revision)

		expected = append(expected, []fakeOp{{
			op:   "storesvc-download",
			name: csi.Component.String(),
		}, {
			op:                "validate-component:Doing",
			name:              instanceName,
			revno:             newSnapRev,
			componentName:     compName,
			componentPath:     filepath.Join(dirs.SnapBlobDir, filename),
			componentRev:      csi.Revision,
			componentSideInfo: csi,
		}, {
			op:                "setup-component",
			containerName:     containerName,
			containerFileName: filename,
		}}...)

		if strings.HasPrefix(compName, string(snap.KernelModulesComponent)) {
			expected = append(expected, fakeOp{
				op: "setup-kernel-modules-components",
				compsToInstall: []*snap.ComponentSideInfo{{
					Component: naming.NewComponentRef(snapName, compName),
					Revision:  csi.Revision,
				}},
			})
		}
	}

	if !refreshAppAwarenessUX {
		expected = append(expected, fakeOp{
			op:   "remove-snap-aliases",
			name: instanceName,
		})
	}

	expected = append(expected, fakeOps{
		{
			op:          "run-inhibit-snap-for-unlink",
			name:        instanceName,
			inhibitHint: "refresh",
		},
		{
			op:                 "unlink-snap",
			path:               filepath.Join(dirs.SnapMountDir, instanceName, currentSnapRev.String()),
			unlinkSkipBinaries: refreshAppAwarenessUX,
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, instanceName, newSnapRev.String()),
			old:  filepath.Join(dirs.SnapMountDir, instanceName, currentSnapRev.String()),
		},
		{
			op:   "setup-snap-save-data",
			path: filepath.Join(dirs.SnapDataSaveDir, instanceName),
		},
	}...)

	expected = append(expected, fakeOps{
		{
			op:    "setup-profiles:Doing",
			name:  instanceName,
			revno: snap.R(11),
		},
		{
			op: "candidate",
			sinfo: snap.SideInfo{
				RealName: snapName,
				SnapID:   snapID,
				Channel:  channel,
				Revision: snap.R(11),
			},
		},
		{
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, instanceName, newSnapRev.String()),
		},
	}...)

	for i, compName := range components {
		expected = append(expected, fakeOp{
			op:   "link-component",
			path: snap.ComponentMountDir(compName, snap.R(i+2), instanceName),
		})
	}

	expected = append(expected, fakeOps{
		{
			op:    "auto-connect:Doing",
			name:  instanceName,
			revno: snap.R(11),
		},
		{
			op: "update-aliases",
		},
	}...)

	if undo {
		expected = append(expected, undoOps(instanceName, newSnapRev, currentSnapRev, components)...)
	} else {
		expected = append(expected, fakeOp{
			op:    "cleanup-trash",
			name:  instanceName,
			revno: newSnapRev,
		})
	}

	downloads := []fakeDownload{{
		macaroon: s.user.StoreMacaroon,
		name:     snapName,
		target:   filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%v.snap", instanceName, newSnapRev)),
	}}
	for i, compName := range components {
		downloads = append(downloads, fakeDownload{
			macaroon: s.user.StoreMacaroon,
			name:     fmt.Sprintf("%s+%s", snapName, compName),
			target:   filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s+%s_%d.comp", instanceName, compName, i+2)),
		})
	}

	c.Check(s.fakeStore.downloads, DeepEquals, downloads)

	c.Check(s.fakeStore.seenPrivacyKeys["privacy-key"], Equals, true, Commentf("salts seen: %v", s.fakeStore.seenPrivacyKeys))
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)

	// check progress
	task := ts.Tasks()[1]
	_, cur, total := task.Progress()
	c.Assert(cur, Equals, s.fakeStore.fakeCurrentProgress)
	c.Assert(total, Equals, s.fakeStore.fakeTotalProgress)

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Assert(snapsup, DeepEquals, snapstate.SnapSetup{
		Channel: channel,
		UserID:  s.user.ID,

		SnapPath: filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%v.snap", instanceName, newSnapRev)),
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		SideInfo:  snapsup.SideInfo,
		Type:      snap.TypeApp,
		Version:   "some-snapVer",
		PlugsOnly: true,
		Flags: snapstate.Flags{
			Transaction: client.TransactionPerSnap,
		},
		InstanceKey: instanceKey,
	})
	c.Assert(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: snapName,
		Revision: snap.R(11),
		Channel:  channel,
		SnapID:   snapID,
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, instanceName, &snapst)
	c.Assert(err, IsNil)

	if !undo {
		c.Assert(snapst.LastRefreshTime, NotNil)
		c.Check(snapst.LastRefreshTime.Equal(now), Equals, true)
		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 2)
		c.Assert(snapst.Sequence.Revisions[0], DeepEquals, seq.Revisions[0])

		cand := sequence.NewRevisionSideState(&snap.SideInfo{
			RealName: snapName,
			Channel:  channel,
			SnapID:   snapID,
			Revision: newSnapRev,
		}, nil)

		for i, comp := range components {
			cand.Components = append(cand.Components, &sequence.ComponentState{
				SideInfo: &snap.ComponentSideInfo{
					Component: naming.NewComponentRef(snapName, comp),
					Revision:  snap.R(i + 2),
				},
				CompType: compNameToType(comp),
			})
		}

		// add our new revision to the sequence
		seq.Revisions = append(seq.Revisions, cand)

		c.Assert(snapst.Sequence, DeepEquals, seq)

		// we end up with the auxiliary store info
		c.Check(snapstate.AuxStoreInfoFilename(snapID), testutil.FilePresent)
	} else {
		// make sure everything is back to how it started
		c.Assert(snapst.Active, Equals, true)
		c.Assert(snapst.Sequence.Revisions, HasLen, 1)
		c.Assert(snapst.Sequence.Revisions[0], DeepEquals, seq.Revisions[0])
	}
}

func (s *snapmgrTestSuite) TestRefreshCandidates(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "app",
	})

	snapstate.Set(s.state, "some-other-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "channel-for-7/stable",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-other-snap", SnapID: "some-other-snap-id", Revision: snap.R(7)},
		}),
		Current:  snap.R(7),
		SnapType: "app",
	})

	candidates, err := snapstate.RefreshCandidates(s.state, nil)
	c.Assert(err, IsNil)
	c.Assert(candidates, HasLen, 1)
	c.Check(candidates[0].InstanceName(), Equals, "some-snap")
}

func (s *snapmgrTestSuite) TestUpdateTasksWithComponentsRemoved(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	restore := release.MockOnClassic(false)
	defer restore()

	si1 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)}
	si2 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)}
	si3 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)}
	si4 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)}
	si5 := &snap.SideInfo{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(5)}
	cref1 := naming.NewComponentRef("some-snap", "comp1")
	cref2 := naming.NewComponentRef("some-snap", "comp2")
	comp1si := snap.NewComponentSideInfo(cref1, snap.R(11))
	comp2si := snap.NewComponentSideInfo(cref2, snap.R(22))
	s.AddCleanup(snapstate.MockReadComponentInfo(func(compMntDir string,
		snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		switch csi.Component.ComponentName {
		case "comp1":
			return &snap.ComponentInfo{
				Component:         cref1,
				Type:              snap.TestComponent,
				ComponentSideInfo: *csi,
			}, nil
		case "comp2":
			return &snap.ComponentInfo{
				Component:         cref2,
				Type:              snap.TestComponent,
				ComponentSideInfo: *csi,
			}, nil
		}
		return nil, errors.New("unexpected component")
	}))
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(si1,
				[]*sequence.ComponentState{
					sequence.NewComponentState(
						comp1si, snap.TestComponent),
					sequence.NewComponentState(
						comp2si, snap.TestComponent),
				}),
			sequence.NewRevisionSideState(si2, nil),
			sequence.NewRevisionSideState(si3, nil),
			sequence.NewRevisionSideState(si4,
				[]*sequence.ComponentState{
					sequence.NewComponentState(
						comp1si, snap.TestComponent),
					sequence.NewComponentState(
						comp2si, snap.TestComponent),
				}),
			sequence.NewRevisionSideState(si5, nil),
		})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        seq,
		Current:         snap.R(3),
		SnapType:        "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	c.Assert(taskKinds(ts.Tasks()), DeepEquals, []string{
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
		"run-hook[pre-refresh]",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"copy-snap-data",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[post-refresh]",
		"start-snap-services",
		"clear-snap",
		"unlink-component",
		"discard-component",
		"unlink-component",
		"discard-component",
		"discard-snap",
		"clear-snap",
		"discard-snap",
		"clear-snap",
		"unlink-component",
		"discard-component",
		"unlink-component",
		"discard-component",
		"discard-snap",
		"cleanup",
		"run-hook[configure]",
		"run-hook[check-health]",
		"check-rerefresh",
	})

	// and ensure that it will remove the components - si1 is cleaned
	// because of garbage collection and si4 and si5 because they are after
	// current.
	var compSup snapstate.ComponentSetup
	tasks := ts.Tasks()

	i := len(tasks) - 17
	c.Check(tasks[i].Kind(), Equals, "unlink-component")
	err = tasks[i].Get("component-setup", &compSup)
	c.Assert(err, IsNil)
	c.Check(compSup.CompSideInfo.Component, Equals, cref1)

	i = len(tasks) - 15
	c.Check(tasks[i].Kind(), Equals, "unlink-component")
	err = tasks[i].Get("component-setup", &compSup)
	c.Assert(err, IsNil)
	c.Check(compSup.CompSideInfo.Component, Equals, cref2)

	i = len(tasks) - 9
	c.Check(tasks[i].Kind(), Equals, "unlink-component")
	err = tasks[i].Get("component-setup", &compSup)
	c.Assert(err, IsNil)
	c.Check(compSup.CompSideInfo.Component, Equals, cref1)

	i = len(tasks) - 7
	c.Check(tasks[i].Kind(), Equals, "unlink-component")
	err = tasks[i].Get("component-setup", &compSup)
	c.Assert(err, IsNil)
	c.Check(compSup.CompSideInfo.Component, Equals, cref2)
}
