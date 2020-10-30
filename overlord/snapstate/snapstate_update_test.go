// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"

	// So it registers Configure.
	_ "github.com/snapcore/snapd/overlord/configstate"
)

func verifyUpdateTasks(c *C, opts, discards int, ts *state.TaskSet, st *state.State) {
	kinds := taskKinds(ts.Tasks())

	expected := []string{
		"prerequisites",
		"download-snap",
		"validate-snap",
		"mount-snap",
	}
	expected = append(expected, "run-hook[pre-refresh]")
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"stop-snap-services",
		)
	}
	if opts&unlinkBefore != 0 {
		expected = append(expected,
			"remove-aliases",
			"unlink-current-snap",
		)
	}
	if opts&updatesGadget != 0 {
		expected = append(expected, "update-gadget-assets")
	}
	expected = append(expected,
		"copy-snap-data",
		"setup-profiles",
		"export-content",
		"link-snap",
	)
	if opts&maybeCore != 0 {
		expected = append(expected, "setup-profiles")
	}
	expected = append(expected,
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"run-hook[post-refresh]",
		"start-snap-services")

	c.Assert(ts.Tasks()[len(expected)-2].Summary(), Matches, `Run post-refresh hook of .*`)
	for i := 0; i < discards; i++ {
		expected = append(expected,
			"clear-snap",
			"discard-snap",
		)
	}
	if opts&cleanupAfter != 0 {
		expected = append(expected,
			"cleanup",
		)
	}
	expected = append(expected,
		"run-hook[configure]",
		"run-hook[check-health]",
	)
	if opts&doesReRefresh != 0 {
		expected = append(expected, "check-rerefresh")
	}

	c.Assert(kinds, DeepEquals, expected)
}

func (s *snapmgrTestSuite) TestUpdateDoesGC(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	restore := release.MockOnClassic(false)
	defer restore()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	chg := s.state.NewChange("update", "update a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{&si},
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

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

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
		"unlink-snap",
		"copy-data",
		"setup-profiles:Doing",
		"export-content:Doing",
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
		Sequence:        []*snap.SideInfo{si1, si2, si3, si4},
		Current:         snap.R(2),
		SnapType:        "app",
	})

	// run the update
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 2, ts, s.state)

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

func (s *snapmgrTestSuite) TestUpdateCanDoBackwards(c *C) {
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
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si11.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Revision: snap.R(7)}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()
	expected := fakeOps{
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(7),
		},
		{
			op:    "export-content:Doing",
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
	// start with an easier-to-read error if this fails:
	c.Assert(s.fakeBackend.ops.Ops(), DeepEquals, expected.Ops())
	c.Assert(s.fakeBackend.ops, DeepEquals, expected)
}

func revs(seq []*snap.SideInfo) []int {
	revs := make([]int, len(seq))
	for i, si := range seq {
		revs[i] = si.Revision.N
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
		Sequence:        seq,
		Current:         snap.R(opts.current),
		SnapType:        "app",
	})

	var chg *state.Change
	var ts *state.TaskSet
	var err error
	if opts.revert {
		chg = s.state.NewChange("revert", "revert a snap")
		ts, err = snapstate.RevertToRevision(s.state, "some-snap", snap.R(opts.via), snapstate.Flags{})
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
			// sanity
			c.Assert(lanes, HasLen, 1)
			terr.JoinLane(lanes[0])
		}
		chg.AddTask(terr)
	}
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)
	c.Check(revs(snapst.Sequence), DeepEquals, opts.after)

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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
	c.Assert(err, ErrorMatches, "experimental feature disabled - test it by setting 'experimental.layouts' to true")

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	_, _, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	refreshes, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, HasLen, 0)

	// When layouts are enabled we can refresh multiple snaps if one of them depends on the feature.
	tr = config.NewTransaction(s.state)
	tr.Set("core", "experimental.layouts", true)
	tr.Commit()

	refreshes, _, err = snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Assert(refreshes, DeepEquals, []string{"some-snap"})
}

func (s *snapmgrTestSuite) TestUpdateFailsEarlyOnEpochMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-epoch-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-epoch-snap", SnapID: "some-epoch-snap-id", Revision: snap.R(1)},
		},
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "fakestore-please-error-on-refresh", Revision: snap.R(7)}},
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
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
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		"unlink-snap",
		"copy-data",
		"setup-profiles:Doing",
		"export-content:Doing",
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
			Flags:        store.SnapActionEnforceValidation,
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
		PlugsOnly: true,
		Flags:     snapstate.Flags{Amend: true},
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
	task = ts.Tasks()[15]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "some-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "",
		Revision: snap.R(-42),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		Channel:  "some-channel",
		SnapID:   "some-snap-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateRunThrough(c *C) {
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

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		{
			op:   "remove-snap-aliases",
			name: "services-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
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
	}

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
		PlugsOnly: true,
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
	task = ts.Tasks()[15]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	})
	c.Check(snapst.CohortKey, Equals, "some-cohort")

	// we end up with the auxiliary store info
	c.Check(snapstate.AuxStoreInfoFilename("services-snap-id"), testutil.FilePresent)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateRunThrough(c *C) {
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
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		SnapType:        "app",
		TrackingChannel: "latest/stable",
		InstanceKey:     "instance",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "services-snap_instance", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

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
		{
			op:   "remove-snap-aliases",
			name: "services-snap_instance",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "services-snap_instance/11"),
			old:  filepath.Join(dirs.SnapMountDir, "services-snap_instance/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "services-snap_instance",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
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
	}

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
		PlugsOnly:   true,
		InstanceKey: "instance",
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
	task = ts.Tasks()[15]
	c.Assert(task.Kind(), Equals, "run-hook")
	c.Assert(task.Summary(), Matches, `Run post-refresh hook of "services-snap_instance" snap if present`)

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "services-snap_instance", &snapst)
	c.Assert(err, IsNil)

	c.Assert(snapst.InstanceKey, Equals, "instance")
	c.Assert(snapst.Active, Equals, true)
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		SnapID:   "services-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "services-snap",
		Channel:  "some-channel",
		SnapID:   "services-snap-id",
		Revision: snap.R(11),
	})
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
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: []*snap.SideInfo{{
			RealName: "some-base",
			SnapID:   "some-base-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "base",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-base"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{si},
		Current:         snap.R(7),
		SnapType:        "app",
	})
	snapstate.Set(s.state, "snap-content-slot", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/stable",
		Sequence: []*snap.SideInfo{{
			RealName: "snap-content-slot",
			SnapID:   "snap-content-slot-id",
			Revision: snap.R(1),
		}},
		Current:  snap.R(1),
		SnapType: "app",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "snap-content-plug", &snapstate.RevisionOptions{Channel: "stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Check(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{macaroon: s.user.StoreMacaroon, name: "snap-content-plug", target: filepath.Join(dirs.SnapBlobDir, "snap-content-plug_11.snap")},
	})
}

func (s *snapmgrTestSuite) TestUpdateRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{&si},
		Current:         si.Revision,
		TrackingChannel: "18/stable",
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "kernel", &snapstate.RevisionOptions{Channel: "edge"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		PlugsOnly: true,
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
	c.Assert(snapst.Sequence, HasLen, 2)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		SnapID:   "kernel-id",
		Channel:  "",
		Revision: snap.R(7),
	})
	c.Assert(snapst.Sequence[1], DeepEquals, &snap.SideInfo{
		RealName: "kernel",
		Channel:  "18/edge",
		SnapID:   "kernel-id",
		Revision: snap.R(11),
	})
}

func (s *snapmgrTestSuite) TestUpdateManyMultipleCredsNoUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   2,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// do UpdateMany using user 2 as fallback
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 2, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence: []*snap.SideInfo{
			{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})
	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(2), SnapID: "services-snap-id"},
		},
		Current:  snap.R(2),
		SnapType: "app",
		UserID:   3,
	})

	chg := s.state.NewChange("refresh", "refresh all snaps")
	// no user is passed to use for UpdateMany
	updated, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	for _, ts := range tts {
		chg.AddAll(ts)
	}
	c.Check(updated, HasLen, 3)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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

func (s *snapmgrTestSuite) TestUpdateUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
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
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "export-content:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
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
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
		},
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
	}

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
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
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
		Sequence:        []*snap.SideInfo{&si2, &si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("some-snap", "foo", "bar")
	tr.Commit()

	var cfgs map[string]interface{}
	// we don't have config snapshots yet
	c.Assert(s.state.Get("revision-config", &cfgs), Equals, state.ErrNoState)

	chg := s.state.NewChange("update", "update a snap")
	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(2)}
	ts, err := snapstate.Update(s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)

	s.state.Lock()
	cfgs = nil
	// config copy of rev. 1 has been made
	c.Assert(s.state.Get("revision-config", &cfgs), IsNil)
	c.Assert(cfgs["some-snap"], DeepEquals, map[string]interface{}{
		"1": map[string]interface{}{
			"foo": "bar",
		},
	})
}

func (s *snapmgrTestSuite) TestUpdateTotalUndoRunThrough(c *C) {
	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:          true,
		Sequence:        []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:   "copy-data",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
			old:  filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op:    "setup-profiles:Doing",
			name:  "some-snap",
			revno: snap.R(11),
		},
		{
			op:    "export-content:Doing",
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
		// undoing everything from here down...
		{
			op:   "remove-snap-aliases",
			name: "some-snap",
		},
		{
			op: "current-snap-service-states",
		},
		{
			op:   "unlink-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/11"),
		},
		{
			op:    "export-content:Undoing",
			name:  "some-snap",
			revno: snap.R(11),
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
			op:   "link-snap",
			path: filepath.Join(dirs.SnapMountDir, "some-snap/7"),
		},
		{
			op: "update-aliases",
		},
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
	}

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
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "",
		Revision: snap.R(7),
	})
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
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	_, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, Equals, store.ErrNoUpdateAvailable)
}

func (s *snapmgrTestSuite) TestUpdateToRevisionRememberedUserRunThrough(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	chg := s.state.NewChange("refresh", "refresh a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(11)}, 0, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "other-channel/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Channel:  "channel-for-7/stable",
		SideInfo: snapsup.SideInfo,
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
	c.Assert(snapst.Sequence, HasLen, 1)
	c.Assert(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7/stable",
		Revision: snap.R(7),
	})
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7/stable"}, s.user.ID, snapstate.Flags{IgnoreValidation: true})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("refresh", "refresh a snap")
	chg.AddAll(ts)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify snapSetup info
	var snapsup snapstate.SnapSetup
	task := ts.Tasks()[0]
	err = task.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup, DeepEquals, snapstate.SnapSetup{
		SideInfo: snapsup.SideInfo,
		Flags: snapstate.Flags{
			IgnoreValidation: true,
		},
	})
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
		Channel:  "channel-for-7",
	})

	// verify snaps in the system state
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "some-snap", &snapst)
	c.Assert(err, IsNil)

	c.Check(snapst.Active, Equals, true)
	c.Check(snapst.Sequence, HasLen, 1)
	c.Check(snapst.Sequence[0], DeepEquals, &snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Channel:  "channel-for-7",
		Revision: snap.R(7),
	})
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
		Sequence: []*snap.SideInfo{&si},
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
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	validateErr := errors.New("refresh control error")
	validateRefreshes := func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int, deviceCtx snapstate.DeviceContext) ([]*snap.Info, error) {
		return nil, validateErr
	}
	// hook it up
	snapstate.ValidateRefreshes = validateRefreshes

	flags := snapstate.Flags{JailMode: true, IgnoreValidation: true}
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
		Sequence: []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
			Flags:        0,
		},
		userID: 1,
	})

	chg = s.state.NewChange("refresh", "refresh snaps")
	chg.AddAll(tts[0])

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active:      true,
		Sequence:    []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-snap_instance"}, s.user.ID, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})

	chg = s.state.NewChange("refresh", "refresh snaps")
	for _, ts := range tts[:len(tts)-1] {
		chg.AddAll(ts)
	}

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

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
						Flags:        0,
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
		TrackingChannel: "channel-for-7/stable",
		Current:         si.Revision,
	})

	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "channel-for-7"}, s.user.ID, snapstate.Flags{Amend: true})
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)

	// ensure we go from local to store revision-7
	var snapsup snapstate.SnapSetup
	tasks := ts.Tasks()
	c.Check(tasks[1].Kind(), Equals, "download-snap")
	err = tasks[1].Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)
	c.Check(snapsup.Revision(), Equals, snap.R(7))
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence: []*snap.SideInfo{&si7, &si11},
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
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
		Sequence: []*snap.SideInfo{&si7, &si11},
		Current:  si7.Revision,
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, s.user.ID, nil)
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

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
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

		updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, scenario.names, s.user.ID, nil)
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

	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "other-snap", SnapID: "other-snap-id", Revision: snap.R(2)},
		},
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
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
			j += 20
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
		Sequence: []*snap.SideInfo{&si},
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
		Sequence: []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{&si},
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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

	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{}, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateWithDeviceContextToRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// unset the global store, it will need to come via the device context
	snapstate.ReplaceStore(s.state, nil)

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: DefaultModel(),
		CtxStore:    s.fakeStore,
	}

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(5), SnapID: "some-snap-id"},
		},
		Current:  snap.R(5),
		SnapType: "app",
		UserID:   1,
	})

	opts := &snapstate.RevisionOptions{Channel: "some-channel", Revision: snap.R(11)}
	ts, err := snapstate.UpdateWithDeviceContext(s.state, "some-snap", opts, 0, snapstate.Flags{}, deviceCtx, "")
	c.Assert(err, IsNil)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 0, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateTasksCoreSetsIgnoreOnConfigure(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "latest/edge",
		Sequence:        []*snap.SideInfo{{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(7)}},
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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
		Sequence: []*snap.SideInfo{{RealName: "some-snap-now-classic", SnapID: "some-snap-now-classic-id", Revision: snap.R(7)}},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap-was-classic", SnapID: "some-snap-was-classic-id", Revision: snap.R(7)}},
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(4),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, expectedDiscards, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateCreatesDiscardAfterCurrentTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	ts, err := snapstate.Update(s.state, "some-snap", nil, 0, snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter|doesReRefresh, 3, ts, s.state)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestUpdateManyTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("seeded", nil)

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:  snap.R(7),
		SnapType: "app",
	})

	_, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
	c.Assert(err, ErrorMatches, `too early for operation, device not yet seeded or device model not acknowledged`)
}

func (s *snapmgrTestSuite) TestUpdateMany(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})

	ts := tts[0]
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, ts, s.state)

	// check that the tasks are in non-default lane
	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{1})
	}
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks())+1) // 1==rerefresh

	// ensure edges information is still there
	te, err := ts.Edge(snapstate.DownloadAndChecksDoneEdge)
	c.Assert(te, NotNil)
	c.Assert(err, IsNil)

	checkIsAutoRefresh(c, ts.Tasks(), false)
}

func (s *snapmgrTestSuite) TestUpdateManyFailureDoesntUndoSnapdRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "some-base", "snapd"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 4)
	c.Assert(updates, HasLen, 3)

	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// refresh of some-snap fails on link-snap
	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "/some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// updated snap is devmode, updatemany doesn't update it
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
	})

	// if a snap installed without --classic gets a classic update it isn't installed
	_, tts, _ := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, nil)
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
		Sequence:        []*snap.SideInfo{{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(7)}},
		Current:         snap.R(7),
		SnapType:        "app",
		Flags:           snapstate.Flags{Classic: true},
	})

	// snap installed with classic: refresh gets classic
	_, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, s.user.ID, &snapstate.Flags{Classic: true})
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 1)
}

func (s *snapmgrTestSuite) TestUpdateAllDevMode(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Flags:  snapstate.Flags{DevMode: true},
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(updates, HasLen, 0)
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC16(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core", SnapID: "core-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "os",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core", "some-base"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 4)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, HasLen, 3)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	prereqTotal := len(tts[0].Tasks()) + len(tts[1].Tasks())
	prereqs := map[string]bool{}
	for i, task := range tts[2].Tasks() {
		waitTasks := task.WaitTasks()
		if i == 0 {
			c.Check(len(waitTasks), Equals, prereqTotal)
		} else if task.Kind() == "link-snap" {
			c.Check(len(waitTasks), Equals, prereqTotal+1)
			for _, pre := range waitTasks {
				if pre.Kind() == "link-snap" {
					snapsup, err := snapstate.TaskSnapSetup(pre)
					c.Assert(err, IsNil)
					prereqs[snapsup.InstanceName()] = true
				}
			}
		}
	}

	c.Check(prereqs, DeepEquals, map[string]bool{
		"core":      true,
		"some-base": true,
	})
}

func (s *snapmgrTestSuite) TestUpdateManyWaitForBasesUC18(c *C) {
	r := snapstatetest.MockDeviceModel(ModelWithBase("core18"))
	defer r()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "core18", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "core18", SnapID: "core18-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "some-base", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-base", SnapID: "some-base-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "base",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:         snap.R(1),
		SnapType:        "app",
		TrackingChannel: "channel-for-base/stable",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap", "core18", "some-base", "snapd"}, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 5)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, HasLen, 4)

	// to make TaskSnapSetup work
	chg := s.state.NewChange("refresh", "...")
	for _, ts := range tts {
		chg.AddAll(ts)
	}

	// Note that some-app only waits for snapd+some-base. The core18
	// base is not special to this snap and not waited for
	prereqTotal := len(tts[0].Tasks()) + len(tts[1].Tasks())
	prereqs := map[string]bool{}
	for i, task := range tts[3].Tasks() {
		waitTasks := task.WaitTasks()
		if i == 0 {
			c.Check(len(waitTasks), Equals, prereqTotal)
		} else if task.Kind() == "link-snap" {
			c.Check(len(waitTasks), Equals, prereqTotal+1)
			for _, pre := range waitTasks {
				if pre.Kind() == "link-snap" {
					snapsup, err := snapstate.TaskSnapSetup(pre)
					c.Assert(err, IsNil)
					prereqs[snapsup.InstanceName()] = true
				}
			}
		}
	}

	// Note that "core18" is not part of the prereqs for some-app
	// as it does not use this base.
	c.Check(prereqs, DeepEquals, map[string]bool{
		"some-base": true,
		"snapd":     true,
	})
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 2)
	verifyLastTasksetIsReRefresh(c, tts)
	c.Check(updates, DeepEquals, []string{"some-snap"})
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)

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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(2)},
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(3)},
		},
		Current:     snap.R(3),
		SnapType:    "app",
		InstanceKey: "instance",
	})

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
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

	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 3, tts[0], s.state)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 1, tts[1], s.state)
}

func (s *snapmgrTestSuite) TestParallelInstanceUpdateManyValidateRefreshes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.parallel-instances", true)
	tr.Commit()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})
	snapstate.Set(s.state, "some-snap_instance", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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

	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(tts, HasLen, 3)
	verifyLastTasksetIsReRefresh(c, tts)
	sort.Strings(updates)
	c.Check(updates, DeepEquals, []string{"some-snap", "some-snap_instance"})
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[0], s.state)
	verifyUpdateTasks(c, unlinkBefore|cleanupAfter, 0, tts[1], s.state)

	c.Check(validateCalled, Equals, true)
}

func (s *snapmgrTestSuite) TestUpdateManyValidateRefreshesUnhappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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
	updates, tts, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
	c.Assert(err, IsNil)
	c.Check(tts, HasLen, 0)
	c.Check(updates, HasLen, 0)

	// refresh some-snap => report error
	updates, tts, err = snapstate.UpdateMany(context.Background(), s.state, []string{"some-snap"}, 0, nil)
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

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []*snap.Info, userID int) (uint64, error) {
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "snapd", SnapID: "snapd-snap-id", Revision: snap.R(1)},
		},
		Current:  snap.R(1),
		SnapType: "app",
	})

	updates, _, err := snapstate.UpdateMany(context.Background(), s.state, nil, 0, nil)
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
		Sequence:                   []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:                   []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence:                   []*snap.SideInfo{&si},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap setup from the task from state
	endT := s.state.Task(t1.ID())
	finalsnapsup := &snapstate.SnapSetup{}
	endT.Get("snap-setup", finalsnapsup)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(finalsnapsup.LastActiveDisabledServices)
	c.Assert(finalsnapsup.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})

}

func (s *snapmgrTestSuite) TestStopSnapServicesFirstSavesSnapSetupLastActiveDisabledServices(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	prevCurrentlyDisabled := s.fakeBackend.servicesCurrentlyDisabled
	s.fakeBackend.servicesCurrentlyDisabled = []string{"svc1", "svc2"}

	// reset the services to what they were before after the test is done
	defer func() {
		s.fakeBackend.servicesCurrentlyDisabled = prevCurrentlyDisabled
	}()

	snapstate.Set(s.state, "services-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{
			{RealName: "services-snap", Revision: snap.R(11)},
		},
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
	t := s.state.NewTask("stop-snap-services", "...")
	t.Set("stop-reason", snap.StopReasonDisable)
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)

	// get the snap setup from the task from state
	endT := s.state.Task(t.ID())
	finalsnapsup := &snapstate.SnapSetup{}
	endT.Get("snap-setup", finalsnapsup)

	// make sure that the disabled services in this snap's state is what we
	// provided
	sort.Strings(finalsnapsup.LastActiveDisabledServices)
	c.Assert(finalsnapsup.LastActiveDisabledServices, DeepEquals, []string{"svc1", "svc2"})
}

func (s *snapmgrTestSuite) TestRefreshDoesntRestoreRevisionConfig(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)

	s.state.Lock()
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

func (s *snapmgrTestSuite) TestRefreshFailureCausesErrorReport(c *C) {
	var errSnap, errMsg, errSig string
	var errExtra map[string]string
	var n int
	restore := snapstate.MockErrtrackerReport(func(aSnap, aErrMsg, aDupSig string, extra map[string]string) (string, error) {
		errSnap = aSnap
		errMsg = aErrMsg
		errSig = aDupSig
		errExtra = extra
		n += 1
		return "oopsid", nil
	})
	defer restore()

	si := snap.SideInfo{
		RealName: "some-snap",
		SnapID:   "some-snap-id",
		Revision: snap.R(7),
	}

	s.state.Lock()
	defer s.state.Unlock()

	s.state.Set("ubuntu-core-transition-retry", 7)
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{&si},
		Current:  si.Revision,
		SnapType: "app",
	})

	chg := s.state.NewChange("install", "install a snap")
	ts, err := snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	s.fakeBackend.linkSnapFailTrigger = filepath.Join(dirs.SnapMountDir, "some-snap/11")

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	// verify we generated a failure report
	c.Check(n, Equals, 1)
	c.Check(errSnap, Equals, "some-snap")
	c.Check(errExtra, DeepEquals, map[string]string{
		"UbuntuCoreTransitionCount": "7",
		"Channel":                   "some-channel",
		"Revision":                  "11",
	})
	c.Check(errMsg, Matches, `(?sm)change "install": "install a snap"
prerequisites: Undo
 snap-setup: "some-snap" \(11\) "some-channel"
download-snap: Undoing
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
auto-connect: Hold
set-auto-aliases: Hold
setup-aliases: Hold
run-hook: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)
	c.Check(errSig, Matches, `(?sm)snap-install:
prerequisites: Undo
 snap-setup: "some-snap"
download-snap: Undoing
validate-snap: Done
.*
link-snap: Error
 INFO unlink
 ERROR fail
auto-connect: Hold
set-auto-aliases: Hold
setup-aliases: Hold
run-hook: Hold
start-snap-services: Hold
cleanup: Hold
run-hook: Hold`)

	// run again with empty "ubuntu-core-transition-retry"
	s.state.Set("ubuntu-core-transition-retry", 0)
	chg = s.state.NewChange("install", "install a snap")
	ts, err = snapstate.Update(s.state, "some-snap", &snapstate.RevisionOptions{Channel: "some-channel"}, s.user.ID, snapstate.Flags{})
	c.Assert(err, IsNil)
	chg.AddAll(ts)
	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()
	// verify that we excluded this field from the bugreport
	c.Check(n, Equals, 2)
	c.Check(errExtra, DeepEquals, map[string]string{
		"Channel":  "some-channel",
		"Revision": "11",
	})
}

func (s *snapmgrTestSuite) TestNoReRefreshInUpdate(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(1)},
		},
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
		Sequence: []*snap.SideInfo{
			{RealName: "alias-snap", Revision: snap.R(11), SnapID: "alias-snap-id"},
		},
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

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

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

	restoreInstallSize := snapstate.MockInstallSize(func(st *state.State, snaps []*snap.Info, userID int) (uint64, error) {
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
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", SnapID: "some-snap-id", Revision: snap.R(4)},
		},
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
