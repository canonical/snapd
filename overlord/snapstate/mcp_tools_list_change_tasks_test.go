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

package snapstate_test

import (
	"context"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestCallListChangeTasksMissingChange(c *C) {
	result, callErr := (snapstate.ListChangeTasksTool{}).Call(context.Background(), state.New(nil), map[string]any{"change_id": "999"})
	c.Check(result, IsNil)
	c.Check(callErr, NotNil)
	c.Check(callErr.Error(), Equals, `cannot find change with id "999"`)
}

func (s *snapMCPSuite) TestCallListChangeTasks(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install-snap", "install snap")
	task := st.NewTask("download-snap", "Download snap")
	task.SetProgress("downloading", 1, 2)
	chg.AddTask(task)
	st.Unlock()

	result, callErr := (snapstate.ListChangeTasksTool{}).Call(context.Background(), st, map[string]any{"change_id": chg.ID()})
	c.Assert(callErr, IsNil)
	out := resultToMap(c, result)
	c.Check(out["change_id"], Equals, chg.ID())
	tasks := out["tasks"].([]any)
	c.Assert(tasks, HasLen, 1)
	taskMap := tasks[0].(map[string]any)
	c.Check(taskMap["kind"], Equals, "download-snap")
	progress := taskMap["progress"].(map[string]any)
	c.Check(progress["label"], Equals, "downloading")
	c.Check(progress["done"], Equals, float64(1))
	c.Check(progress["total"], Equals, float64(2))
}

func (s *snapMCPSuite) TestListChangeTasksValidateInvalidType(c *C) {
	err := (snapstate.ListChangeTasksTool{}).Validate(map[string]any{"change_id": true})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*change_id.*`)
}

func (s *snapMCPSuite) TestListChangeTasksValidateMissingChangeID(c *C) {
	err := (snapstate.ListChangeTasksTool{}).Validate(map[string]any{})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "change_id must not be empty")
}
