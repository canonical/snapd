// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ctlcmd_test

import (
	"encoding/json"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type tasksSuite struct {
	testutil.BaseTest
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&tasksSuite{})

func (s *tasksSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.mockHandler = hooktest.NewMockHandler()
}

// setupChangeAndContext creates a state, a change associated with "test-snap",
// and a non-ephemeral hook context for "test-snap".
func (s *tasksSuite) setupChangeAndContext(c *C, summary string, taskStatus state.Status) (*state.State, *hookstate.Context, string) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", summary)
	task := st.NewTask("test-task", summary)
	chg.AddTask(task)
	chg.Set("initiated-by-snap", "test-snap")

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")

	c.Assert(err, IsNil)

	return st, ctx, chg.ID()
}

// TestTasksCommandInvalidArguments tests error handling for invalid argument counts
func (s *tasksSuite) TestTasksCommandInvalidArguments(c *C) {
	_, ctx, _ := s.setupChangeAndContext(c, "test", state.DoneStatus)

	testCases := []struct {
		description string
		args        []string
	}{
		{
			description: "no arguments",
			args:        []string{"tasks"},
		},
		{
			description: "too many arguments",
			args:        []string{"tasks", "arg1", "arg2"},
		},
	}

	for _, tc := range testCases {
		_, _, err := ctlcmd.Run(ctx, tc.args, 0, nil)
		c.Assert(err, NotNil)
		c.Assert(strings.Contains(err.Error(), "invalid number of arguments"), Equals, true)

	}
}

// TestTasksCommandNormalOperation tests output for multiple changes/tasks
func (s *tasksSuite) TestTasksCommandNormalOperation(c *C) {
	st, ctx, chg1ID := s.setupChangeAndContext(c, "change-1-done", state.DoneStatus)

	st.Lock()
	// Create second task (doing) in same change
	chg := st.Change(chg1ID)
	task2 := st.NewTask("test-task-2", "change-2-doing")
	chg.AddTask(task2)
	chg.Set("initiated-by-snap", "test-snap")
	task2.SetStatus(state.DoingStatus)

	// Create third task / second change (error)
	chg2 := st.NewChange("snapctl-install", "change-3-error")
	task3 := st.NewTask("test-task-3", "change-3-error")
	chg2.AddTask(task3)
	chg2.Set("initiated-by-snap", "test-snap")
	task3.SetStatus(state.ErrorStatus)
	chg2ID := chg2.ID()
	st.Unlock()

	// Verify task 1 (done)
	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", chg1ID}, 0, nil)
	c.Assert(err, IsNil)
	output := string(stdout)

	// Validate table headers are present
	c.Assert(strings.Contains(output, "Status"), Equals, true)
	c.Assert(strings.Contains(output, "Spawn"), Equals, true)
	c.Assert(strings.Contains(output, "Ready"), Equals, true)
	c.Assert(strings.Contains(output, "Summary"), Equals, true)

	c.Assert(strings.Contains(output, "change-1-done"), Equals, true)
	c.Assert(strings.Contains(output, "Done"), Equals, true)

	// Verify task 2 (doing)
	stdout, _, err = ctlcmd.Run(ctx, []string{"tasks", chg1ID}, 0, nil)
	c.Assert(err, IsNil)
	output = string(stdout)
	c.Assert(strings.Contains(output, "change-2-doing"), Equals, true)
	c.Assert(strings.Contains(output, "Doing"), Equals, true)

	// Verify task 3 (error)
	stdout, _, err = ctlcmd.Run(ctx, []string{"tasks", chg2ID}, 0, nil)
	c.Assert(err, IsNil)
	output = string(stdout)
	c.Assert(strings.Contains(output, "change-3-error"), Equals, true)
	c.Assert(strings.Contains(output, "Error"), Equals, true)
}

// TestTasksCommandNoAssociatedChanges tests that a change without an associated snap returns an error
func (s *tasksSuite) TestTasksCommandNoAssociatedChanges(c *C) {
	st, ctx, _ := s.setupChangeAndContext(c, "associated-change", state.DoneStatus)

	st.Lock()
	// Create a change but don't associate it with test-snap
	chg := st.NewChange("snapctl-install", "unassociated-change")
	task := st.NewTask("test-task", "test task")
	chg.AddTask(task)
	unassociatedID := chg.ID()
	st.Unlock()

	_, _, err := ctlcmd.Run(ctx, []string{"tasks", unassociatedID}, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "not found"), Equals, true)
}

// TestTasksCommandFiltersOtherSnaps tests that changes from other snaps are not accessible
func (s *tasksSuite) TestTasksCommandFiltersOtherSnaps(c *C) {
	st, ctx, chg1ID := s.setupChangeAndContext(c, "test-snap-change", state.DoneStatus)

	st.Lock()
	// Create a change for a different snap
	chg2 := st.NewChange("snapctl-install", "other-snap-change")
	task2 := st.NewTask("test-task-2", "test task 2")
	chg2.AddTask(task2)
	chg2.Set("initiated-by-snap", "other-snap")
	task2.SetStatus(state.DoneStatus)
	chg2ID := chg2.ID()
	st.Unlock()

	// test-snap's own change should be accessible
	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", chg1ID}, 0, nil)
	c.Assert(err, IsNil)
	output := string(stdout)
	c.Assert(strings.Contains(output, "test-snap-change"), Equals, true)

	// other-snap's change should not be accessible
	_, _, err = ctlcmd.Run(ctx, []string{"tasks", chg2ID}, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "not found"), Equals, true)
}

// TestTasksCommandAllowedForUnprivilegedUser verifies that "tasks" is in the
// non-root allowlist and executes successfully when called with a non-zero UID.
func (s *tasksSuite) TestTasksCommandAllowedForUnprivilegedUser(c *C) {
	const unprivilegedUID = uint32(1000)
	_, ctx, chgID := s.setupChangeAndContext(c, "test-change", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", chgID}, unprivilegedUID, nil)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), testutil.Contains, "Done")
}

// TestTasksCommandJSONFormat tests JSON output format
func (s *tasksSuite) TestTasksCommandJSONFormat(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, "json-test-change", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", changeID, "--format", "json"}, 0, nil)
	c.Assert(err, IsNil)

	// Parse JSON output (single change object)
	var change map[string]any
	err = json.Unmarshal(stdout, &change)
	c.Assert(err, IsNil)

	// Verify the change ID, status, and summary match
	c.Assert(change["id"], Equals, changeID)
	c.Assert(change["status"], Equals, "Done")
	c.Assert(change["summary"], Equals, "json-test-change")
}
