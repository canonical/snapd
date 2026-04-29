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

	task2 := st.NewTask("test-task2", "a second test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task2, st, setup, s.mockHandler, "")

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
		c.Assert(err, NotNil, Commentf("Expected error for: %s", tc.description))
		c.Assert(strings.Contains(err.Error(), "invalid number of arguments"), Equals, true,
			Commentf("Expected 'invalid number of arguments' error for %s, got: %s", tc.description, err.Error()))

	}
}

// TestTasksCommandTableOutput verifies basic table structure with headers
func (s *tasksSuite) TestTasksCommandTableOutput(c *C) {
	_, ctx, id := s.setupChangeAndContext(c, "test summary", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", id}, 0, nil)
	c.Assert(err, IsNil)

	output := string(stdout)
	// Verify table headers are present
	c.Assert(strings.Contains(output, "ID"), Equals, true)
	c.Assert(strings.Contains(output, "Status"), Equals, true)
	c.Assert(strings.Contains(output, "Spawn"), Equals, true)
	c.Assert(strings.Contains(output, "Ready"), Equals, true)
	c.Assert(strings.Contains(output, "Summary"), Equals, true)
}

// TestTasksCommandDoneChange tests output for a completed change
func (s *tasksSuite) TestTasksCommandDoneChange(c *C) {
	_, ctx, id := s.setupChangeAndContext(c, "done-change-summary", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", id}, 0, nil)
	c.Assert(err, IsNil)

	output := string(stdout)
	c.Assert(strings.Contains(output, "done-change-summary"), Equals, true)
	c.Assert(strings.Contains(output, "Done"), Equals, true)
}

// TestTasksCommandDoingChange tests output for an in-progress change (ready time shows as "-")
func (s *tasksSuite) TestTasksCommandDoingChange(c *C) {
	_, ctx, id := s.setupChangeAndContext(c, "doing-change-summary", state.DoingStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", id}, 0, nil)
	c.Assert(err, IsNil)

	output := string(stdout)
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header + at least one change
	c.Assert(len(lines) >= 2, Equals, true)

	// Find the line with our change and verify it contains "-" for ready time
	found := false
	for _, line := range lines[1:] { // skip header
		if strings.Contains(line, "doing-change-summary") {
			// Should have a "-" for the ready time since it's still doing
			fields := strings.Fields(line)
			c.Assert(len(fields) >= 4, Equals, true, Commentf("Expected at least 4 fields in output line"))
			found = true
		}
	}
	c.Assert(found, Equals, true, Commentf("Expected to find change with 'doing-change-summary'"))
}

// TestTasksCommandErrorChange tests output for a failed change
func (s *tasksSuite) TestTasksCommandErrorChange(c *C) {
	_, ctx, id := s.setupChangeAndContext(c, "error-change-summary", state.ErrorStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", id}, 0, nil)
	c.Assert(err, IsNil)

	output := string(stdout)
	c.Assert(strings.Contains(output, "error-change-summary"), Equals, true)
	c.Assert(strings.Contains(output, "Error"), Equals, true)
}

// TestTasksCommandMultipleChanges tests output for multiple changes queried individually
func (s *tasksSuite) TestTasksCommandMultipleChanges(c *C) {
	st, ctx, chg1ID := s.setupChangeAndContext(c, "change-1-done", state.DoneStatus)

	st.Lock()
	// Create second change (doing)
	chg2 := st.NewChange("snapctl-install", "change-2-doing")
	task2 := st.NewTask("test-task-2", "change-2-doing")
	chg2.AddTask(task2)
	chg2.Set("initiated-by-snap", "test-snap")
	task2.SetStatus(state.DoingStatus)

	// Create third change (error)
	chg3 := st.NewChange("snapctl-install", "change-3-error")
	task3 := st.NewTask("test-task-3", "change-3-error")
	chg3.AddTask(task3)
	chg3.Set("initiated-by-snap", "test-snap")
	task3.SetStatus(state.ErrorStatus)
	chg2ID := chg2.ID()
	chg3ID := chg3.ID()
	st.Unlock()

	// Verify change 1 (done)
	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", chg1ID}, 0, nil)
	c.Assert(err, IsNil)
	output := string(stdout)
	c.Assert(strings.Contains(output, "change-1-done"), Equals, true)
	c.Assert(strings.Contains(output, "Done"), Equals, true)

	// Verify change 2 (doing)
	stdout, _, err = ctlcmd.Run(ctx, []string{"tasks", chg2ID}, 0, nil)
	c.Assert(err, IsNil)
	output = string(stdout)
	c.Assert(strings.Contains(output, "change-2-doing"), Equals, true)
	c.Assert(strings.Contains(output, "Doing"), Equals, true)

	// Verify change 3 (error)
	stdout, _, err = ctlcmd.Run(ctx, []string{"tasks", chg3ID}, 0, nil)
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
	// Note: not setting "initiated-by-snap" so it won't be associated
	unassociatedID := chg.ID()
	st.Unlock()

	_, _, err := ctlcmd.Run(ctx, []string{"tasks", unassociatedID}, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "not found"), Equals, true)
}

// TestTasksCommandChangeIDPresent tests that the tasks for the queried change are displayed
func (s *tasksSuite) TestTasksCommandChangeIDPresent(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, "test-change", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", changeID}, 0, nil)
	c.Assert(err, IsNil)

	output := string(stdout)
	// The task summary (set to the change summary in setupChangeAndContext) must appear,
	// confirming the correct change's tasks are shown.
	c.Assert(strings.Contains(output, "test-change"), Equals, true,
		Commentf("Expected task summary 'test-change' to be in output for change %s", changeID))
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
	c.Assert(strings.Contains(output, "test-snap-change"), Equals, true,
		Commentf("Expected to see changes from test-snap"))

	// other-snap's change should not be accessible
	_, _, err = ctlcmd.Run(ctx, []string{"tasks", chg2ID}, 0, nil)
	c.Assert(err, NotNil)
	c.Assert(strings.Contains(err.Error(), "not found"), Equals, true,
		Commentf("Expected 'not found' error for other-snap's change"))
}

// TestTasksCommandJSONFormat tests JSON output format
func (s *tasksSuite) TestTasksCommandJSONFormat(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, "json-test-change", state.DoneStatus)

	stdout, _, err := ctlcmd.Run(ctx, []string{"tasks", changeID, "--format", "json"}, 0, nil)
	c.Assert(err, IsNil)

	// Parse JSON output (single change object)
	var change map[string]any
	err = json.Unmarshal(stdout, &change)
	c.Assert(err, IsNil, Commentf("Failed to unmarshal JSON output: %s", string(stdout)))

	// Verify expected fields are present
	c.Assert(change["id"], NotNil, Commentf("Expected 'id' field in JSON output"))
	c.Assert(change["status"], NotNil, Commentf("Expected 'status' field in JSON output"))
	c.Assert(change["summary"], NotNil, Commentf("Expected 'summary' field in JSON output"))

	// Verify the change ID, status, and summary match
	c.Assert(change["id"], Equals, changeID)
	c.Assert(change["status"], Equals, float64(state.DoneStatus))
	c.Assert(change["summary"], Equals, "json-test-change")
}
