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
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestCallListChangesNoChanges(c *C) {
	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), state.New(nil), map[string]any{"select": "all"})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Check(changes, HasLen, 0)
}

func (s *snapMCPSuite) TestCallListChangesFiltersBySnap(c *C) {
	st := state.New(nil)
	st.Lock()
	chg1 := st.NewChange("install-snap", "install one")
	chg1.Set("snap-names", []string{"snap-a"})
	chg2 := st.NewChange("install-snap", "install two")
	chg2.Set("snap-names", []string{"snap-b"})
	st.Unlock()

	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), st, map[string]any{"select": "all", "snap_name": "snap-a"})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].(map[string]any)["id"], Equals, chg1.ID())
	_ = chg2
}

func (s *snapMCPSuite) TestCallListChangesOmitsTaskDetails(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install-snap", "install one")
	chg.Set("snap-names", []string{"snap-a"})
	task := st.NewTask("download-snap", "Download snap")
	chg.AddTask(task)
	st.Unlock()

	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), st, map[string]any{"select": "all"})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Assert(changes, HasLen, 1)
	_, hasTasks := changes[0].(map[string]any)["tasks"]
	c.Check(hasTasks, Equals, false)
}

func (s *snapMCPSuite) TestCallListChangesFiltersByKindAndDate(c *C) {
	st := state.New(nil)

	firstTime := time.Date(2026, time.January, 10, 10, 0, 0, 0, time.UTC)
	restoreTime := state.MockTime(firstTime)
	defer restoreTime()

	st.Lock()
	_ = st.NewChange("install-snap", "install one")
	st.Unlock()

	secondTime := firstTime.Add(4 * time.Hour)
	state.MockTime(secondTime)

	st.Lock()
	chg2 := st.NewChange("refresh-snap", "refresh one")
	st.Unlock()

	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), st, map[string]any{
		"select": "all",
		"kind":   "refresh-snap",
		"since":  firstTime.Add(2 * time.Hour).Format(time.RFC3339),
		"until":  secondTime.Add(2 * time.Hour).Format(time.RFC3339),
	})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Assert(changes, HasLen, 1)
	first := changes[0].(map[string]any)
	c.Check(first["id"], Equals, chg2.ID())
	c.Check(first["kind"], Equals, "refresh-snap")
	c.Check(first["spawn_time"], Equals, secondTime.Format(time.RFC3339))
}

func (s *snapMCPSuite) TestCallListChangesKindIsCaseInsensitive(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("refresh-snap", "refresh one")
	st.Unlock()

	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), st, map[string]any{
		"select": "all",
		"kind":   "REFRESH-SNAP",
	})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Assert(changes, HasLen, 1)
	first := changes[0].(map[string]any)
	c.Check(first["id"], Equals, chg.ID())
	c.Check(first["kind"], Equals, "refresh-snap")
}

func (s *snapMCPSuite) TestCallListChangesFiltersByStatus(c *C) {
	st := state.New(nil)
	st.Lock()
	_ = st.NewChange("install-snap", "install one")
	chg2 := st.NewChange("refresh-snap", "refresh one")
	chg2.SetStatus(state.ErrorStatus)
	st.Unlock()

	result, callErr := (snapstate.ListChangesTool{}).Call(context.Background(), st, map[string]any{
		"select": "all",
		"status": "failed",
	})
	c.Assert(callErr, IsNil)
	changes := resultToMap(c, result)["changes"].([]any)
	c.Assert(changes, HasLen, 1)
	first := changes[0].(map[string]any)
	c.Check(first["id"], Equals, chg2.ID())
	c.Check(first["status"], Equals, "Error")
}

func (s *snapMCPSuite) TestListChangesValidateInvalidDateRange(c *C) {
	_, err := (snapstate.ListChangesTool{}).Call(context.Background(), state.New(nil), map[string]any{
		"since": "2026-01-11T00:00:00Z",
		"until": "2026-01-10T00:00:00Z",
	})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "since must not be after until")
}

func (s *snapMCPSuite) TestListChangesValidateInvalidDateFormat(c *C) {
	err := (snapstate.ListChangesTool{}).Validate(map[string]any{
		"since": "not-a-date",
	})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*`)
}

func (s *snapMCPSuite) TestListChangesValidateInvalidStatusType(c *C) {
	err := (snapstate.ListChangesTool{}).Validate(map[string]any{
		"status": 1,
	})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*status.*`)
}
