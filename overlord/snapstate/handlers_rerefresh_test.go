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

func (s *reRefreshSuite) TestDoReRefreshFailsWithoutReRefreshSetup(c *C) {
	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("rerefresh", "test")
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(logstr(task), Contains, `no state entry for key`)
}

func (s *reRefreshSuite) TestDoReRefreshFailsIfUpdateFails(c *C) {
	defer snapstate.MockReRefreshUpdater(func(context.Context, *state.State, string, []string, int, snapstate.UpdateFilter, *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		return nil, nil, errors.New("bzzt")
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("rerefresh", "test")
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

func (s *reRefreshSuite) TestDoReRefreshNoReRefreshes(c *C) {
	updaterCalled := false
	defer snapstate.MockReRefreshUpdater(func(context.Context, *state.State, string, []string, int, snapstate.UpdateFilter, *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		updaterCalled = true
		return nil, nil, nil
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "some-snap")
	task := s.state.NewTask("rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `no re-refreshes found`)
	c.Check(updaterCalled, Equals, true)
}

func (s *reRefreshSuite) TestDoReRefreshPassesReRefreshSetupData(c *C) {
	var chgID string
	defer snapstate.MockReRefreshUpdater(func(ctx context.Context, st *state.State, changeID string, snaps []string, userID int, filter snapstate.UpdateFilter, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(changeID, Equals, chgID)
		c.Check(snaps, DeepEquals, []string{"won", "too", "tree"})
		c.Check(userID, Equals, 42)
		c.Check(flags, DeepEquals, &snapstate.Flags{
			DevMode:  true,
			JailMode: true,
		})
		return nil, nil, nil
	})()

	s.state.Lock()
	task := s.state.NewTask("rerefresh", "test")
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
	c.Check(logstr(task), Contains, `no re-refreshes found`)
}

func (s *reRefreshSuite) TestDoReRefreshAddsNewTasks(c *C) {
	defer snapstate.MockReRefreshUpdater(func(ctx context.Context, st *state.State, changeID string, snaps []string, userID int, filter snapstate.UpdateFilter, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(snaps, DeepEquals, []string{"won", "too", "tree"})

		task := st.NewTask("witness", "...")

		return []string{"won"}, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})()

	s.state.Lock()
	chg := changeWithLanesAndSnapSetups(s.state, "won", "too", "tree")
	task := s.state.NewTask("rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `found re-refresh for "won"`)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 5)
	for i, kind := range []string{
		"a-task-for-snap-won-in-lane-1",
		"a-task-for-snap-too-in-lane-2",
		"a-task-for-snap-tree-in-lane-3",
		"rerefresh",
		"witness",
	} {
		c.Check(tasks[i].Kind(), Equals, kind)
	}
}
