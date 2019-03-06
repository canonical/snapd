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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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
	chg := st.NewChange("dummy", "...")
	for _, snapName := range snapNames {
		lane := st.NewLane()
		tsk := st.NewTask(fmt.Sprintf("a-task-for-snap-%s-in-lane-%d", snapName, lane), "test")
		tsk.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{RealName: snapName},
		})
		chg.AddTask(tsk)
		tsk.JoinLane(lane)
		tsk.SetStatus(state.DoneStatus)
	}
	return chg
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
	defer snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, []*state.TaskSet, error) {
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
	defer snapstate.MockReRefreshUpdateMany(func(context.Context, *state.State, []string, int, snapstate.UpdateFilter, *snapstate.Flags, string) ([]string, []*state.TaskSet, error) {
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
	defer snapstate.MockReRefreshUpdateMany(func(ctx context.Context, st *state.State, snaps []string, userID int, filter snapstate.UpdateFilter, flags *snapstate.Flags, changeID string) ([]string, []*state.TaskSet, error) {
		c.Check(changeID, Equals, chgID)
		expected := []string{"won", "too", "tree"}
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
	chg := changeWithLanesAndSnapSetups(s.state, "won", "too", "tree")
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
	defer snapstate.MockReRefreshUpdateMany(func(ctx context.Context, st *state.State, snaps []string, userID int, filter snapstate.UpdateFilter, flags *snapstate.Flags, changeID string) ([]string, []*state.TaskSet, error) {
		expected := []string{"won", "too", "tree"}
		sort.Strings(expected)
		sort.Strings(snaps)
		c.Check(snaps, DeepEquals, expected)

		task := st.NewTask("witness", "...")

		return []string{"won"}, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "won", "too", "tree")
	task := s.state.NewTask("check-rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `Found re-refresh for "won".`)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 5)
	for i, kind := range []string{
		"a-task-for-snap-won-in-lane-1",
		"a-task-for-snap-too-in-lane-2",
		"a-task-for-snap-tree-in-lane-3",
		"check-rerefresh",
		"witness",
	} {
		c.Check(tasks[i].Kind(), Equals, kind)
	}
}

// wrapper around snapstate.RefreshedSnaps for easier testing
func refreshedSnaps(task *state.Task) string {
	snaps := snapstate.RefreshedSnaps(task)
	sort.Strings(snaps)
	return strings.Join(snaps, ",")
}

// add a lane with two tasks to chg, the first one with a SnapSetup
// for a snap with t1snap, the second one with status t2status.
func addLane(st *state.State, chg *state.Change, t1snap string, t2status state.Status) {
	lane := st.NewLane()
	t1 := st.NewTask("dummy1", "...")
	t1.JoinLane(lane)
	t1.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: t1snap}})
	t1.SetStatus(state.DoneStatus)
	chg.AddTask(t1)

	t2 := st.NewTask("dummy2", "...")
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
	c.Check(refreshedSnaps(task), Equals, "aaa")
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
	c.Check(refreshedSnaps(task), Equals, "aaa,bbb")
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
	c.Check(refreshedSnaps(task), Equals, "aaa,bbb")
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
	c.Check(refreshedSnaps(task), Equals, "ddd")
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
	c.Check(refreshedSnaps(task), Equals, "aaa,bbb")
}

func (s *reRefreshSuite) TestLaneSnapsTwoSetups(c *C) {
	// check that only the first SnapSetup is important
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("dummy1", "...")
	t1.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "one"}})
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("dummy2", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.DoneStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)

	c.Check(refreshedSnaps(task), Equals, "one")
}

func (s *reRefreshSuite) TestLaneSnapsBadSetup(c *C) {
	// check that a bad SnapSetup doesn't make the thing fail
	s.state.Lock()
	defer s.state.Unlock()

	ts := state.NewTaskSet()
	t1 := s.state.NewTask("dummy1", "...")
	t1.Set("snap-setup", "what is this")
	t1.SetStatus(state.DoneStatus)
	ts.AddTask(t1)
	t2 := s.state.NewTask("dummy2", "...")
	t2.Set("snap-setup", snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "two"}})
	t2.WaitFor(t1)
	ts.AddTask(t2)
	t2.SetStatus(state.DoneStatus)
	ts.JoinLane(s.state.NewLane())
	chg := s.state.NewChange("testing", "...")
	chg.AddAll(ts)

	task := s.state.NewTask("check-rerefresh", "...")
	chg.AddTask(task)

	c.Check(refreshedSnaps(task), Equals, "two")
}

func (*reRefreshSuite) TestFilterReturnsFalseIfEpochEqual(c *C) {
	// these work because we're mocking ReadInfo
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: "some-snap", Revision: snap.R(7)},
		},
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
		Sequence: []*snap.SideInfo{
			{RealName: "snap-with-empty-epoch", Revision: snap.R(7)},
		},
		Current:  snap.R(7),
		SnapType: "app",
	}
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.E("0")}, snapst), Equals, false)
	c.Check(snapstate.ReRefreshFilter(&snap.Info{Epoch: snap.Epoch{}}, snapst), Equals, false)
}
