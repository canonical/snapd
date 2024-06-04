// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	. "github.com/snapcore/snapd/testutil"
)

type reRefreshSuite struct {
	baseHandlerSuite
}

var _ = Suite(&reRefreshSuite{})

func logstr(task *state.Task) string {
	return strings.Join(task.Log(), "\n")
}

func changeWithLanesAndSnapSetups(st *state.State, snapNames ...string) *state.Change {
	chg := st.NewChange("sample", "...")
	for _, snapName := range snapNames {
		lane := st.NewLane()
		tsk := st.NewTask("download-snap", fmt.Sprintf("a-task-for-snap-%s-in-lane-%d", snapName, lane))
		tsk.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{RealName: snapName},
		})
		chg.AddTask(tsk)
		tsk.JoinLane(lane)
		tsk.SetStatus(state.DoneStatus)
	}
	return chg
}

func (s *reRefreshSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.state.Lock()
	defer s.state.Unlock()

	_, err := restart.Manager(s.state, "boot-id-1", nil)
	c.Assert(err, IsNil)
}

func (s *reRefreshSuite) TestDoCheckReRefreshFailsWithoutReRefreshSetup(c *C) {
	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("check-rerefresh", "test")
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(logstr(task), Contains, `no state entry for key`)
}

func (s *reRefreshSuite) TestDoCheckReRefreshFailsIfUpdateFails(c *C) {
	defer snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, []*snapstate.RevisionOptions, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, *snapstate.UpdateTaskSets, error) {
		return nil, nil, errors.New("bzzt")
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("check-rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(logstr(task), Contains, `bzzt`)
}

func (s *reRefreshSuite) TestDoCheckReRefreshNoReRefreshes(c *C) {
	updaterCalled := false
	defer snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, []*snapstate.RevisionOptions, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, *snapstate.UpdateTaskSets, error) {
		updaterCalled = true
		return nil, nil, nil
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("check-rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `No re-refreshes found.`)
	c.Check(updaterCalled, Equals, true)
}

func (s *reRefreshSuite) TestDoCheckReRefreshPassesReRefreshSetupData(c *C) {
	var chgID string
	defer snapstate.MockReRefreshUpdateMany(func(_ context.Context, _ *state.State, snaps []string, _ []*snapstate.RevisionOptions, userID int, _ snapstate.UpdateFilter, flags *snapstate.Flags, changeID string) ([]string, *snapstate.UpdateTaskSets, error) {
		c.Check(changeID, Equals, chgID)
		expected := []string{"foo", "bar", "baz"}
		sort.Strings(expected)
		sort.Strings(snaps)
		c.Check(snaps, DeepEquals, expected)
		c.Check(userID, Equals, 42)
		c.Check(flags, DeepEquals, &snapstate.Flags{
			DevMode:  true,
			JailMode: true,
		})
		return nil, nil, nil
	})()

	s.state.Lock()
	task := s.state.NewTask("check-rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{
		"user-id":  42,
		"devmode":  true,
		"jailmode": true,
	})
	chg := changeWithLanesAndSnapSetups(s.state, "foo", "bar", "baz")
	chg.AddTask(task)
	chgID = chg.ID()
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `No re-refreshes found.`)
}

func (s *reRefreshSuite) TestDoCheckReRefreshAddsNewTasks(c *C) {
	defer snapstate.MockReRefreshUpdateMany(func(_ context.Context, st *state.State, snaps []string, _ []*snapstate.RevisionOptions, _ int, _ snapstate.UpdateFilter, _ *snapstate.Flags, _ string) ([]string, *snapstate.UpdateTaskSets, error) {
		expected := []string{"foo", "bar", "baz"}
		sort.Strings(expected)
		sort.Strings(snaps)
		c.Check(snaps, DeepEquals, expected)

		task := st.NewTask("witness", "witness")

		tasksetGrp := &snapstate.UpdateTaskSets{Refresh: []*state.TaskSet{state.NewTaskSet(task)}}
		return []string{"foo"}, tasksetGrp, nil
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "foo", "bar", "baz")
	task := s.state.NewTask("check-rerefresh", "check rerefresh")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `Found re-refresh for "foo".`)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 5)
	for i, kind := range []string{
		"a-task-for-snap-foo-in-lane-1",
		"a-task-for-snap-bar-in-lane-2",
		"a-task-for-snap-baz-in-lane-3",
		"check rerefresh",
		"witness",
	} {
		c.Check(tasks[i].Summary(), Equals, kind)
	}
}

func (s *reRefreshSuite) TestDoCheckReRefreshWaitOnPendingRestart(c *C) {
	s.runner.AddHandler("a-task-for-snap-foo", func(task *state.Task, tomb *tomb.Tomb) error {
		st := task.State()
		st.Lock()
		defer st.Unlock()
		return restart.FinishTaskWithRestart(task, state.DoneStatus, restart.RestartSystem, "foo", nil)
	}, nil)

	// setup the change for check-rerefresh, then we manually request for
	// one of the tasks to reboot
	s.state.Lock()

	chg := s.state.NewChange("sample", "...")
	lane := s.state.NewLane()
	tsk := s.state.NewTask("a-task-for-snap-foo", "test")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "foo"},
	})
	// In order for the code to be tested we need a task to go into WaitStatus.
	restart.MarkTaskAsRestartBoundary(tsk, restart.RestartBoundaryDirectionDo)
	chg.AddTask(tsk)
	tsk.JoinLane(lane)
	task := s.state.NewTask("check-rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	// retry will be hit, so we must ensure again
	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()

		s.state.Lock()
		status := task.Status()
		s.state.Unlock()
		if status == state.WaitStatus {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.WaitStatus)
	c.Check(task.WaitedStatus(), Equals, state.DoStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	c.Check(logstr(task), Matches, `*. INFO Task set to wait until a system restart allows to continue`)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 2)
	for i, kind := range []string{
		"a-task-for-snap-foo",
		"check-rerefresh",
	} {
		c.Check(tasks[i].Kind(), Equals, kind)
	}
}

// wrapper around snapstate.RefreshedSnaps for easier testing
func refreshedSnaps(c *C, task *state.Task) string {
	snaps, _, err := snapstate.RefreshedSnaps(task, nil)
	c.Assert(err, IsNil)
	sort.Strings(snaps)
	return strings.Join(snaps, ",")
}

// add a lane with two tasks to chg, the first one with a SnapSetup
// for a snap with t1snap, the second one with status t2status.
func addLane(st *state.State, chg *state.Change, t1snap string, t2status state.Status) {
	lane := st.NewLane()
	t1 := st.NewTask("download-snap", "...")
	t1.JoinLane(lane)
	t1.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: t1snap}})
	t1.SetStatus(state.DoneStatus)
	chg.AddTask(t1)

	t2 := st.NewTask("test2", "...")
	t2.JoinLane(lane)
	t2.WaitFor(t1)
	t2.SetStatus(t2status)
	chg.AddTask(t2)
}

func (s *reRefreshSuite) TestLaneSnapsSimple(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("testing", "...")
	addLane(s.state, chg, "aaa", state.DoneStatus)
	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	c.Check(refreshedSnaps(c, task), Equals, "aaa")
}

func (s *reRefreshSuite) TestLaneSnapsMoreLanes(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("testing", "...")
	addLane(s.state, chg, "aaa", state.DoneStatus)
	// more lanes, no problem
	addLane(s.state, chg, "bbb", state.DoneStatus)
	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	c.Check(refreshedSnaps(c, task), Equals, "aaa,bbb")
}

func (s *reRefreshSuite) TestLaneSnapsFailedLane(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("testing", "...")
	addLane(s.state, chg, "aaa", state.DoneStatus)
	addLane(s.state, chg, "bbb", state.DoneStatus)
	// a lane that's failed, no problem
	addLane(s.state, chg, "ccc", state.ErrorStatus)
	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	c.Check(refreshedSnaps(c, task), Equals, "aaa,bbb")
}

func (s *reRefreshSuite) TestLaneSnapsRerefreshResets(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("testing", "...")
	addLane(s.state, chg, "aaa", state.DoneStatus)
	addLane(s.state, chg, "bbb", state.DoneStatus)
	// a check-rerefresh task resets the list
	chg.AddTask(s.state.NewTask("check-rerefresh", "..."))
	addLane(s.state, chg, "ddd", state.DoneStatus)
	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	c.Check(refreshedSnaps(c, task), Equals, "ddd")
}

func (s *reRefreshSuite) TestLaneSnapsStopsAtSelf(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	chg := s.state.NewChange("testing", "...")
	addLane(s.state, chg, "aaa", state.DoneStatus)
	addLane(s.state, chg, "bbb", state.DoneStatus)
	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	addLane(s.state, chg, "ddd", state.DoneStatus)
	chg.AddTask(s.state.NewTask("check-rerefresh", "..."))

	// unless we're looking for _that_ task (this is defensive; can't really happen)
	c.Check(refreshedSnaps(c, task), Equals, "aaa,bbb")
}

func (s *reRefreshSuite) TestLaneSnapsTwoSetups(c *C) {
	// Verify that two snaps on the same lane is also detected
	// and returned correctly.
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("download-snap", "...")
	t1.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "one"}})
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("download-snap", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.DoneStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)

	c.Check(refreshedSnaps(c, task), Equals, "one,two")
}

func (s *reRefreshSuite) TestLaneSnapsTwoSetupsFailed(c *C) {
	// Verify that two snaps on the same lane, where one of them has failed
	// will result in both being not reported.
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("test1", "...")
	t1.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "one"}})
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("test2", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.ErrorStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)

	c.Check(refreshedSnaps(c, task), Equals, "")
}

func (s *reRefreshSuite) TestLaneSnapsInvalidSetup(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("download-snap", "...")
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("download-snap", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.DoneStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)

	snaps, _, err := snapstate.RefreshedSnaps(task, nil)
	c.Check(snaps, HasLen, 0)
	c.Check(err, ErrorMatches, `internal error: expected SnapSetup for download-snap: no state entry for key "snap-setup"`)
}

func (s *reRefreshSuite) TestLaneSnapsIgnoresOtherTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("test1", "...")
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("download-snap", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.DoneStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)
	c.Check(refreshedSnaps(c, task), Equals, "two")
}

func (*reRefreshSuite) TestFilterReturnsFalseIfEpochEqual(c *C) {
	// these work because we're mocking ReadInfo
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
		}),
		Current:  snap.R(7),
		SnapType: "app",
	}

	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.E("0")}, snapst), Equals, true)
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.E("1*")}, snapst), Equals, false)
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.E("1")}, snapst), Equals, true)
}

func (s *reRefreshSuite) TestFilterReturnsFalseIfEpochEqualZero(c *C) {
	// these work because we're mocking ReadInfo
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "snap-with-empty-epoch", Revision: snap.R(7)},
		}),
		Current:  snap.R(7),
		SnapType: "app",
	}
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.E("0")}, snapst), Equals, false)
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.Epoch{}}, snapst), Equals, false)
}

// validation-sets related tests

func (s *refreshSuite) TestMaybeRestoreValidationSetsAndRevertSnaps(c *C) {
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	refreshedSnaps := []string{"foo", "bar"}
	// nothing to do with no enforced validation sets
	ts, err := snapstate.MaybeRestoreValidationSetsAndRevertSnaps(st, refreshedSnaps, "")
	c.Assert(err, IsNil)
	c.Check(ts, IsNil)
}

func (s *validationSetsSuite) TestMaybeRestoreValidationSetsAndRevertSnapsOneRevert(c *C) {
	var enforcedValidationSetsCalled int
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		enforcedValidationSetsCalled++

		vs := snapasserts.NewValidationSets()
		var snap1, snap2, snap3 map[string]interface{}
		snap3 = map[string]interface{}{
			"id":       "abcKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap3",
			"presence": "required",
		}

		switch enforcedValidationSetsCalled {
		case 1:
			// refreshed validation sets
			snap1 = map[string]interface{}{
				"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap1",
				"presence": "required",
				"revision": "3",
			}
			// require snap2 at revision 5 (if snap refresh succeeded, but it didn't, so
			// current revision of the snap is wrong)
			snap2 = map[string]interface{}{
				"id":       "bgtKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap2",
				"presence": "required",
				"revision": "5",
			}
		case 2:
			// validation sets restored from history
			snap1 = map[string]interface{}{
				"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap1",
				"presence": "required",
				"revision": "1",
			}
			snap2 = map[string]interface{}{
				"id":       "bgtKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap2",
				"presence": "required",
				"revision": "2",
			}
		default:
			c.Fatalf("unexpected call to EnforcedValidatioSets")
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", snap1, snap2, snap3)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	var restoreValidationSetsTrackingCalled int
	restoreRestoreValidationSetsTracking := snapstate.MockRestoreValidationSetsTracking(func(*state.State) error {
		restoreValidationSetsTrackingCalled++
		return nil
	})
	defer restoreRestoreValidationSetsTracking()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// snaps installed after partial refresh
	si1 := &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(3)}
	si11 := &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap1", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si11, si1}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap1`, si1)

	// some-snap2 failed to refresh and remains at revision 2
	si2 := &snap.SideInfo{RealName: "some-snap2", SnapID: "bgtKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(2)}
	snapstate.Set(s.state, "some-snap2", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  snap.R(2),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap2`, si2)

	si3 := &snap.SideInfo{RealName: "some-snap3", SnapID: "abcKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap3", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap3`, si3)

	chg := s.state.NewChange("install change", "...")
	t1 := s.state.NewTask("link-snap", "...")
	t1.Set("snap-setup", &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}})
	t1.SetStatus(state.DoneStatus)
	t2 := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(t1)
	chg.AddTask(t2)

	// some-snap2 failed to refresh
	refreshedSnaps := []string{"some-snap1", "some-snap3"}
	// pass change id to make sure revert doesn't conflict
	ts, err := snapstate.MaybeRestoreValidationSetsAndRevertSnaps(st, refreshedSnaps, chg.ID())
	c.Assert(err, IsNil)

	// we expect revert of snap1
	c.Assert(ts, HasLen, 1)
	revertTasks := ts[0].Tasks()
	c.Assert(taskKinds(revertTasks), DeepEquals, []string{
		"prerequisites",
		"prepare-snap",
		"stop-snap-services",
		"remove-aliases",
		"unlink-current-snap",
		"setup-profiles",
		"link-snap",
		"auto-connect",
		"set-auto-aliases",
		"setup-aliases",
		"start-snap-services",
		"run-hook[configure]",
		"run-hook[check-health]",
	})

	snapsup, err := snapstate.TaskSnapSetup(revertTasks[0])
	c.Assert(err, IsNil)
	c.Check(snapsup.Flags, Equals, snapstate.Flags{Revert: true, RevertStatus: snapstate.NotBlocked})
	c.Check(snapsup.InstanceName(), Equals, "some-snap1")
	c.Check(snapsup.Revision(), Equals, snap.R(1))

	c.Check(restoreValidationSetsTrackingCalled, Equals, 1)
	c.Check(enforcedValidationSetsCalled, Equals, 2)
}

func (s *validationSetsSuite) TestMaybeRestoreValidationSetsAndRevertNoSnapsRefreshed(c *C) {
	var enforcedValidationSetsCalled int
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		enforcedValidationSetsCalled++

		vs := snapasserts.NewValidationSets()
		snap1 := map[string]interface{}{
			"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap1",
			"presence": "required",
			"revision": "3",
		}

		c.Assert(enforcedValidationSetsCalled, Equals, 1, Commentf("unexpected call to EnforcedValidatioSets"))
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", snap1)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	var restoreValidationSetsTrackingCalled int
	restoreRestoreValidationSetsTracking := snapstate.MockRestoreValidationSetsTracking(func(*state.State) error {
		restoreValidationSetsTrackingCalled++
		return nil
	})
	defer restoreRestoreValidationSetsTracking()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// snaps in the system
	si1 := &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap1", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap1`, si1)

	// no snaps get refreshed
	ts, err := snapstate.MaybeRestoreValidationSetsAndRevertSnaps(st, nil, "")
	c.Assert(err, IsNil)

	// we expect no snap reverts
	c.Assert(ts, HasLen, 0)
	c.Check(restoreValidationSetsTrackingCalled, Equals, 1)
	c.Check(enforcedValidationSetsCalled, Equals, 1)
}

func (s *validationSetsSuite) TestMaybeRestoreValidationSetsAndRevertJustValidationSetsRestore(c *C) {
	var enforcedValidationSetsCalled int
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		enforcedValidationSetsCalled++

		vs := snapasserts.NewValidationSets()
		var snap1, snap2 map[string]interface{}
		snap2 = map[string]interface{}{
			"id":       "abcKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap2",
			"presence": "required",
		}

		switch enforcedValidationSetsCalled {
		case 1:
			// refreshed validation sets
			// snap1 revision 3 is now required (but snap wasn't refreshed)
			snap1 = map[string]interface{}{
				"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap1",
				"presence": "required",
				"revision": "3",
			}
		case 2:
			// validation sets restored from history
			snap1 = map[string]interface{}{
				"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
				"name":     "some-snap1",
				"presence": "required",
				"revision": "1",
			}
		default:
			c.Fatalf("unexpected call to EnforcedValidatioSets")
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "2", snap1, snap2)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	var restoreValidationSetsTrackingCalled int
	restoreRestoreValidationSetsTracking := snapstate.MockRestoreValidationSetsTracking(func(*state.State) error {
		restoreValidationSetsTrackingCalled++
		return nil
	})
	defer restoreRestoreValidationSetsTracking()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// snaps in the system after partial refresh
	si1 := &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap1", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap1`, si1)

	si3 := &snap.SideInfo{RealName: "some-snap2", SnapID: "abcKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap2", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap2`, si3)

	refreshedSnaps := []string{"some-snap2"}
	ts, err := snapstate.MaybeRestoreValidationSetsAndRevertSnaps(st, refreshedSnaps, "")
	c.Assert(err, IsNil)

	// we expect no snap reverts
	c.Assert(ts, HasLen, 0)
	c.Check(restoreValidationSetsTrackingCalled, Equals, 1)
	c.Check(enforcedValidationSetsCalled, Equals, 2)
}

func (s *validationSetsSuite) TestMaybeRestoreValidationSetsAndRevertStillValid(c *C) {
	var enforcedValidationSetsCalled int
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		enforcedValidationSetsCalled++

		vs := snapasserts.NewValidationSets()
		snap2 := map[string]interface{}{
			"id":       "abcKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap2",
			"presence": "required",
		}

		c.Assert(enforcedValidationSetsCalled, Equals, 1, Commentf("unexpected call to EnforcedValidatioSets"))

		// refreshed validation sets
		// snap1 revision 3 is now required (but snap wasn't refreshed)
		snap1 := map[string]interface{}{
			"id":       "aaqKhntON3vR7kwEbVPsILm7bUViPDzx",
			"name":     "some-snap1",
			"presence": "required",
		}
		vsa1 := s.mockValidationSetAssert(c, "bar", "3", snap1, snap2)
		vs.Add(vsa1.(*asserts.ValidationSet))
		return vs, nil
	})
	defer restore()

	var restoreValidationSetsTrackingCalled int
	restoreRestoreValidationSetsTracking := snapstate.MockRestoreValidationSetsTracking(func(*state.State) error {
		restoreValidationSetsTrackingCalled++
		return nil
	})
	defer restoreRestoreValidationSetsTracking()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// snaps in the system after partial refresh
	si1 := &snap.SideInfo{RealName: "some-snap1", SnapID: "aaqKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(3)}
	snapstate.Set(s.state, "some-snap1", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si1}),
		Current:  snap.R(3),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap1`, si1)

	si3 := &snap.SideInfo{RealName: "some-snap2", SnapID: "abcKhntON3vR7kwEbVPsILm7bUViPDzx", Revision: snap.R(1)}
	snapstate.Set(s.state, "some-snap2", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  snap.R(1),
		SnapType: "app",
	})
	snaptest.MockSnap(c, `name: some-snap2`, si3)

	// pretend that some-snap1 was refreshed (and some-snap2 failed), some-snap1 is now at revision 3; validation set
	// with sequence 3 is still valid though so no snap reverts.
	refreshedSnaps := []string{"some-snap1"}
	ts, err := snapstate.MaybeRestoreValidationSetsAndRevertSnaps(st, refreshedSnaps, "")
	c.Assert(err, IsNil)

	// we expect no snap reverts
	c.Assert(ts, HasLen, 0)
	c.Check(restoreValidationSetsTrackingCalled, Equals, 0)
	c.Check(enforcedValidationSetsCalled, Equals, 1)
}
