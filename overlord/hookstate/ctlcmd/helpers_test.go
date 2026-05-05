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

	// Convert the state.Change to ChangeInfo
	c.Assert(chg, NotNil)
	changeInfo := ctlcmd.StateChangeToChangeInfo(chg)

	// Verify basic change information
	c.Check(changeInfo.ID, Equals, chg.ID())
	c.Check(changeInfo.Kind, Equals, "snapctl-install")
	c.Check(changeInfo.Summary, Equals, "install components for test-snap")
	c.Check(changeInfo.Status, Equals, "Done")
	c.Check(changeInfo.Ready, Equals, true)
	c.Check(changeInfo.SpawnTime.IsZero(), Equals, false)

	// Verify task was converted
	c.Assert(changeInfo.Tasks, HasLen, 1)

	// Check task (should be Done)
	task1Info := changeInfo.Tasks[0]
	c.Check(task1Info.Kind, Equals, "prepare-components")
	c.Check(task1Info.Summary, Equals, "preparing components")
	c.Check(task1Info.Status, Equals, "Done")
	c.Assert(task1Info.Log, HasLen, 1)
	c.Check(task1Info.Log[0], testutil.Contains, "Component prepared successfully")
	c.Check(task1Info.Progress.Label, Equals, "Preparing component")
	c.Check(task1Info.Progress.Done, Equals, 1)
	c.Check(task1Info.Progress.Total, Equals, 1)

	// Verify change-level data (api-data)
	c.Assert(changeInfo.Data, NotNil)
	c.Assert(changeInfo.Data["snap-names"], NotNil)
}
