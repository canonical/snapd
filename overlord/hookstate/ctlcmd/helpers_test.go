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

type helperSuite struct {
	testutil.BaseTest
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&helperSuite{})

func (s *helperSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.mockHandler = hooktest.NewMockHandler()
}

// setupChangeAndContext creates a state, a change (with an optional initiator),
// and a non-ephemeral hook context for "test-snap". It also populates the change
// and task with realistic data for testing StateChangeToChangeInfo conversion.
func (s *helperSuite) setupChangeAndContext(c *C, taskStatus state.Status, initiatorSnap string) (*state.State, *hookstate.Context, string, *state.Change) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// Create a change with a meaningful kind and summary
	chg := st.NewChange("snapctl-install", "install components for test-snap")

	// Create multiple tasks to test task conversion
	task1 := st.NewTask("prepare-components", "preparing components")
	task2 := st.NewTask("download-component", "downloading component")
	task3 := st.NewTask("install-component", "installing component")

	chg.AddTask(task1)
	chg.AddTask(task2)
	chg.AddTask(task3)

	// Set task dependencies to test task relationships
	task2.WaitFor(task1)
	task3.WaitFor(task2)

	// Populate the first task with realistic data
	task1.SetStatus(state.DoneStatus)
	task1.Logf("Starting component preparation")
	task1.Logf("Component prepared successfully")
	task1.SetProgress("Preparing component", 1, 1)

	// Set the status of task2 based on parameter
	task2.SetStatus(taskStatus)
	task2.Logf("Downloading component from store")
	task2.SetProgress("Downloading", 50, 100)

	// Keep task3 in waiting status
	task3.SetStatus(state.DoStatus)
	task3.SetProgress("Waiting to install", 0, 1)

	// Set the requested task status parameter for the second task
	if taskStatus == state.DoneStatus {
		task2.SetStatus(state.DoneStatus)
		task2.Logf("Downloading component from store")
		task2.Logf("Download complete")
		task2.SetProgress("Downloading", 100, 100)
	}

	// Add change-level data (api-data)
	chg.Set("api-data", map[string]interface{}{
		"snap-names": []string{"test-snap"},
		"kind":       "install-components",
	})

	if initiatorSnap != "" {
		chg.Set("initiated-by-snap", initiatorSnap)
	}

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task1, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return st, ctx, chg.ID(), chg
}

// TestStateChangeToChangeInfo tests the StateChangeToChangeInfo function,
// verifying that state changes are correctly converted to ChangeInfo structs
// and that the data can be successfully marshalled and unmarshalled.
func (s *helperSuite) TestStateChangeToChangeInfo(c *C) {
	st, _, changeID, _ := s.setupChangeAndContext(c, state.DoStatus, "test-snap")

	st.Lock()
	chg := st.Change(changeID)
	c.Assert(chg, NotNil)

	// Convert the state.Change to ChangeInfo
	changeInfo := ctlcmd.StateChangeToChangeInfo(chg)
	st.Unlock()

	// Verify basic change information
	c.Check(changeInfo.ID, Equals, changeID)
	c.Check(changeInfo.Kind, Equals, "snapctl-install")
	c.Check(changeInfo.Summary, Equals, "install components for test-snap")
	c.Check(changeInfo.Status, Equals, "Do")
	c.Check(changeInfo.Ready, Equals, false)
	c.Check(changeInfo.SpawnTime.IsZero(), Equals, false)

	// Verify tasks were converted
	c.Assert(changeInfo.Tasks, HasLen, 3)

	// Check first task (should be Done)
	task1Info := changeInfo.Tasks[0]
	c.Check(task1Info.Kind, Equals, "prepare-components")
	c.Check(task1Info.Summary, Equals, "preparing components")
	c.Check(task1Info.Status, Equals, "Done")
	// Log entries include timestamps, so check that they contain the expected messages
	c.Assert(task1Info.Log, HasLen, 2)
	c.Check(strings.Contains(task1Info.Log[0], "Starting component preparation"), Equals, true)
	c.Check(strings.Contains(task1Info.Log[1], "Component prepared successfully"), Equals, true)
	c.Check(task1Info.Progress.Label, Equals, "Preparing component")
	c.Check(task1Info.Progress.Done, Equals, 1)
	c.Check(task1Info.Progress.Total, Equals, 1)

	// Check second task (should be in Do status)
	task2Info := changeInfo.Tasks[1]
	c.Check(task2Info.Kind, Equals, "download-component")
	c.Check(task2Info.Status, Equals, "Do")
	c.Check(task2Info.Progress.Label, Equals, "Downloading")
	c.Check(task2Info.Progress.Done, Equals, 50)
	c.Check(task2Info.Progress.Total, Equals, 100)

	// Check third task (should be in Do status)
	task3Info := changeInfo.Tasks[2]
	c.Check(task3Info.Kind, Equals, "install-component")
	c.Check(task3Info.Status, Equals, "Do")

	// Verify change-level data (api-data)
	c.Assert(changeInfo.Data, NotNil)
	c.Assert(changeInfo.Data["snap-names"], NotNil)

	// Test JSON marshalling
	jsonData, err := json.Marshal(changeInfo)
	c.Assert(err, IsNil)
	c.Assert(jsonData, NotNil)
	c.Assert(len(jsonData) > 0, Equals, true)

	// Test JSON unmarshalling
	var unmarshalled ctlcmd.ChangeInfo
	err = json.Unmarshal(jsonData, &unmarshalled)
	c.Assert(err, IsNil)

	// Verify unmarshalled data matches original
	c.Check(unmarshalled.ID, Equals, changeInfo.ID)
	c.Check(unmarshalled.Kind, Equals, changeInfo.Kind)
	c.Check(unmarshalled.Summary, Equals, changeInfo.Summary)
	c.Check(unmarshalled.Status, Equals, changeInfo.Status)
	c.Check(unmarshalled.Ready, Equals, changeInfo.Ready)
	c.Assert(unmarshalled.Tasks, HasLen, len(changeInfo.Tasks))

	// Verify task details survived marshalling/unmarshalling
	c.Check(unmarshalled.Tasks[0].Kind, Equals, task1Info.Kind)
	c.Check(unmarshalled.Tasks[0].Status, Equals, task1Info.Status)
	c.Check(unmarshalled.Tasks[1].Progress.Label, Equals, task2Info.Progress.Label)
	c.Check(unmarshalled.Tasks[1].Progress.Done, Equals, task2Info.Progress.Done)
}
