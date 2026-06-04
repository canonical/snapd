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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type helperSuite struct {
	testutil.BaseTest
}

var _ = Suite(&helperSuite{})

// TestStateChangeToChangeInfo tests the StateChangeToChangeInfo function,
// verifying that state changes are correctly converted to ChangeInfo structs
// and that the data can be successfully marshaled and unmarshaled.
func (s *helperSuite) TestStateChangeToChangeInfo(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install components for test-snap")
	task := st.NewTask("prepare-components", "preparing components")
	chg.AddTask(task)

	task.SetStatus(state.DoneStatus)
	task.Logf("Component prepared successfully")
	task.SetProgress("Preparing component", 1, 1)
	chg.Set("api-data", map[string]any{
		"snap-names": []string{"test-snap"},
		"kind":       "install-components",
	})

	// Convert the state.Change to ChangeInfo. Need to copy dynamic fields.
	c.Assert(chg, NotNil)
	changeInfo := ctlcmd.StateChangeToChangeInfo(chg)

	// Build the expected json.RawMessage byte arrays for the Data map
	snapNamesMsg := json.RawMessage(`["test-snap"]`)
	kindMsg := json.RawMessage(`"install-components"`)

	chgReadyTime := chg.ReadyTime()
	taskReadyTime := task.ReadyTime()

	// Construct the fully expected struct directly from the raw values and state properties
	expected := &ctlcmd.ChangeInfo{
		ID:        chg.ID(),
		Kind:      "snapctl-install",
		Summary:   "install components for test-snap",
		Status:    "Done",
		Ready:     true,
		SpawnTime: chg.SpawnTime(),
		ReadyTime: &chgReadyTime,
		Data: map[string]*json.RawMessage{
			"snap-names": &snapNamesMsg,
			"kind":       &kindMsg,
		},
		Tasks: []ctlcmd.TaskInfo{
			{
				ID:      task.ID(),
				Kind:    "prepare-components",
				Summary: "preparing components",
				Status:  "Done",
				Progress: client.TaskProgress{
					Label: "Preparing component",
					Done:  1,
					Total: 1,
				},
				Log:       task.Log(),
				SpawnTime: task.SpawnTime(),
				ReadyTime: &taskReadyTime,
			},
		},
	}

	// Deep equals check on the complete structure
	c.Check(changeInfo, DeepEquals, expected)
}

// TestChangeInfoToClientChangeNilReadyTime verifies that ChangeInfoToClientChange
// does not panic when ReadyTime is nil (i.e. the change/task is not yet complete).
func (s *helperSuite) TestChangeInfoToClientChangeNilReadyTime(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install components for test-snap")
	task := st.NewTask("prepare-components", "preparing components")
	chg.AddTask(task)
	// Leave change and task in default (non-Done) state so ReadyTime is zero/nil

	changeInfo := ctlcmd.StateChangeToChangeInfo(chg)
	c.Assert(changeInfo.ReadyTime, IsNil)
	c.Assert(changeInfo.Tasks[0].ReadyTime, IsNil)

	// Must not panic
	clientChg := ctlcmd.ChangeInfoToClientChange(changeInfo)

	c.Check(clientChg.ReadyTime.IsZero(), Equals, true)
	c.Assert(clientChg.Tasks, HasLen, 1)
	c.Check(clientChg.Tasks[0].ReadyTime.IsZero(), Equals, true)
}
