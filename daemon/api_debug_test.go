// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

var _ = check.Suite(&postDebugSuite{})

type postDebugSuite struct {
	apiBaseSuite
}

func (s *postDebugSuite) TestPostDebugEnsureStateSoon(c *check.C) {
	s.daemonWithOverlordMock()
	s.expectRootAccess()

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	buf := bytes.NewBufferString(`{"action": "ensure-state-soon"}`)
	req, err := http.NewRequest("POST", "/v2/debug", buf)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.Equals, true)
	c.Check(soon, check.Equals, 1)
}

func (s *postDebugSuite) TestDebugConnectivityHappy(c *check.C) {
	_ = s.daemon(c)

	s.connectivityResult = map[string]bool{
		"good.host.com":         true,
		"another.good.host.com": true,
	}

	req, err := http.NewRequest("GET", "/v2/debug?aspect=connectivity", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.DeepEquals, daemon.ConnectivityStatus{
		Connectivity: true,
		Unreachable:  []string(nil),
	})
}

func (s *postDebugSuite) TestDebugConnectivityUnhappy(c *check.C) {
	_ = s.daemon(c)

	s.connectivityResult = map[string]bool{
		"good.host.com": true,
		"bad.host.com":  false,
	}

	req, err := http.NewRequest("GET", "/v2/debug?aspect=connectivity", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Result, check.DeepEquals, daemon.ConnectivityStatus{
		Connectivity: false,
		Unreachable:  []string{"bad.host.com"},
	})
}

func (s *postDebugSuite) TestGetDebugBaseDeclaration(c *check.C) {
	_ = s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/debug?aspect=base-declaration", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	c.Check(rsp.Result.(map[string]interface{})["base-declaration"],
		testutil.Contains, "type: base-declaration")
}

func mockDurationThreshold() func() {
	oldDurationThreshold := timings.DurationThreshold
	restore := func() {
		timings.DurationThreshold = oldDurationThreshold
	}
	timings.DurationThreshold = 0
	return restore
}

func (s *postDebugSuite) getDebugTimings(c *check.C, request string) []interface{} {
	defer mockDurationThreshold()()

	s.daemonWithOverlordMock()

	req, err := http.NewRequest("GET", request, nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()

	chg1 := st.NewChange("foo", "...")
	task1 := st.NewTask("bar", "...")
	chg1.AddTask(task1)
	task1.SetStatus(state.DoingStatus)

	chg2 := st.NewChange("foo", "...")
	task2 := st.NewTask("bar", "...")
	chg2.AddTask(task2)

	chg3 := st.NewChange("foo", "...")
	task3 := st.NewTask("bar", "...")
	chg3.AddTask(task3)

	tm1 := state.TimingsForTask(task3)
	sp1 := tm1.StartSpan("span", "span...")
	sp1.Stop()
	tm1.Save(st)

	tm2 := timings.New(map[string]string{"ensure": "foo", "change-id": chg1.ID()})
	sp2 := tm2.StartSpan("span", "span...")
	sp2.Stop()
	tm2.Save(st)

	tm3 := timings.New(map[string]string{"ensure": "foo", "change-id": chg2.ID()})
	sp3 := tm3.StartSpan("span", "span...")
	sp3.Stop()
	tm3.Save(st)

	tm4 := timings.New(map[string]string{"ensure": "bar", "change-id": chg3.ID()})
	sp4 := tm3.StartSpan("span", "span...")
	sp4.Stop()
	tm4.Save(st)

	st.Unlock()

	rsp := s.syncReq(c, req, nil)
	data, err := json.Marshal(rsp.Result)
	c.Assert(err, check.IsNil)
	var dataJSON []interface{}
	json.Unmarshal(data, &dataJSON)

	return dataJSON
}

func (s *postDebugSuite) TestGetDebugTimingsSingleChange(c *check.C) {
	dataJSON := s.getDebugTimings(c, "/v2/debug?aspect=change-timings&change-id=1")

	c.Check(dataJSON, check.HasLen, 1)
	tmData := dataJSON[0].(map[string]interface{})
	c.Check(tmData["change-id"], check.DeepEquals, "1")
	c.Check(tmData["change-timings"], check.NotNil)
}

func (s *postDebugSuite) TestGetDebugTimingsEnsureLatest(c *check.C) {
	dataJSON := s.getDebugTimings(c, "/v2/debug?aspect=change-timings&ensure=foo&all=false")
	c.Assert(dataJSON, check.HasLen, 1)

	tmData := dataJSON[0].(map[string]interface{})
	c.Check(tmData["change-id"], check.DeepEquals, "2")
	c.Check(tmData["change-timings"], check.NotNil)
	c.Check(tmData["total-duration"], check.NotNil)
}

func (s *postDebugSuite) TestGetDebugTimingsEnsureAll(c *check.C) {
	dataJSON := s.getDebugTimings(c, "/v2/debug?aspect=change-timings&ensure=foo&all=true")

	c.Assert(dataJSON, check.HasLen, 2)
	tmData := dataJSON[0].(map[string]interface{})
	c.Check(tmData["change-id"], check.DeepEquals, "1")
	c.Check(tmData["change-timings"], check.NotNil)
	c.Check(tmData["total-duration"], check.NotNil)

	tmData = dataJSON[1].(map[string]interface{})
	c.Check(tmData["change-id"], check.DeepEquals, "2")
	c.Check(tmData["change-timings"], check.NotNil)
	c.Check(tmData["total-duration"], check.NotNil)
}

func (s *postDebugSuite) TestGetDebugTimingsError(c *check.C) {
	s.daemonWithOverlordMock()

	req, err := http.NewRequest("GET", "/v2/debug?aspect=change-timings&ensure=unknown", nil)
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)

	req, err = http.NewRequest("GET", "/v2/debug?aspect=change-timings&change-id=9999", nil)
	c.Assert(err, check.IsNil)
	rsp = s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)
}

func (s *postDebugSuite) TestMinLane(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t := st.NewTask("bar", "")
	c.Check(daemon.MinLane(t), check.Equals, 0)

	lane1 := st.NewLane()
	t.JoinLane(lane1)
	c.Check(daemon.MinLane(t), check.Equals, lane1)

	lane2 := st.NewLane()
	t.JoinLane(lane2)
	c.Check(daemon.MinLane(t), check.Equals, lane1)

	// sanity
	c.Check(t.Lanes(), check.DeepEquals, []int{lane1, lane2})
}
