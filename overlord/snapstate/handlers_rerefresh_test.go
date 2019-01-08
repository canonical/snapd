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
	"errors"
	"strings"

	"golang.org/x/net/context"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	. "github.com/snapcore/snapd/testutil"
)

type reRefreshSuite struct {
	baseHandlerSuite
}

var _ = Suite(&reRefreshSuite{})

func logstr(task *state.Task) string {
	return strings.Join(task.Log(), "\n")
}

func (s *reRefreshSuite) TestDoReRefreshFailsWithoutReRefreshSetup(c *C) {
	s.state.Lock()
	task := s.state.NewTask("rerefresh", "test")
	s.state.NewChange("dummy", "...").AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(logstr(task), Contains, `no state entry for key`)
}

func (s *reRefreshSuite) TestDoReRefreshFailsIfUpdateFails(c *C) {
	defer snapstate.MockReRefreshUpdater(func(context.Context, *state.State, string, []string, int, *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		return nil, nil, errors.New("bzzt")
	})()

	s.state.Lock()
	task := s.state.NewTask("rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	s.state.NewChange("dummy", "...").AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.ErrorStatus)
	c.Check(logstr(task), Contains, `bzzt`)
}

func (s *reRefreshSuite) TestDoReRefreshNoReRefreshes(c *C) {
	defer snapstate.MockReRefreshUpdater(func(context.Context, *state.State, string, []string, int, *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		return nil, nil, nil
	})()

	s.state.Lock()
	task := s.state.NewTask("rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{})
	s.state.NewChange("dummy", "...").AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `no re-refreshes found`)
}

func (s *reRefreshSuite) TestDoReRefreshPassesReRefreshSetupData(c *C) {
	var chgID string
	defer snapstate.MockReRefreshUpdater(func(ctx context.Context, st *state.State, changeID string, snaps []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
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
		"snaps":    []string{"won", "too", "tree"},
		"user-id":  42,
		"devmode":  true,
		"jailmode": true,
	})
	chg := s.state.NewChange("dummy", "...")
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
	defer snapstate.MockReRefreshUpdater(func(ctx context.Context, st *state.State, changeID string, snaps []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(snaps, DeepEquals, []string{"won", "too", "tree"})

		task := st.NewTask("witness", "...")

		return []string{"won"}, []*state.TaskSet{state.NewTaskSet(task)}, nil
	})()

	s.state.Lock()
	task := s.state.NewTask("rerefresh", "test")
	task.Set("rerefresh-setup", map[string]interface{}{
		"snaps": []string{"won", "too", "tree"},
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(task)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(task.Status(), Equals, state.DoneStatus)
	c.Check(logstr(task), Contains, `found re-refresh for "won"`)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 2)
	c.Check(tasks[1].Kind(), Equals, "witness")
}
