// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"net/http"
	"net/http/httptest"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = check.Suite(&apiQuotaSuite{})

type apiQuotaSuite struct {
	apiBaseSuite
}

func (s *apiQuotaSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemon(c)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	tr := config.NewTransaction(st)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	r := servicestate.MockSystemdVersion(248)
	s.AddCleanup(r)

	// POST requires root
	s.expectedWriteAccess = daemon.RootAccess{}
}

func mockQuotas(st *state.State, c *check.C) {
	err := servicestate.CreateQuota(st, "foo", "", nil, 9000)
	c.Assert(err, check.IsNil)
	err = servicestate.CreateQuota(st, "bar", "foo", nil, 1000)
	c.Assert(err, check.IsNil)
	err = servicestate.CreateQuota(st, "baz", "foo", nil, 2000)
	c.Assert(err, check.IsNil)
}

func (s *apiQuotaSuite) TestPostQuotaUnknownAction(c *check.C) {
	data, err := json.Marshal(daemon.PostQuotaGroupData{Action: "foo", GroupName: "bar"})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, `unknown quota action "foo"`)
}

func (s *apiQuotaSuite) TestPostQuotaInvalidGroupName(c *check.C) {
	data, err := json.Marshal(daemon.PostQuotaGroupData{Action: "ensure", GroupName: "$$$"})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `invalid quota group name: .*`)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUnhappy(c *check.C) {
	daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
		c.Check(name, check.Equals, "booze")
		c.Check(parentName, check.Equals, "foo")
		c.Check(snaps, check.DeepEquals, []string{"bar"})
		c.Check(memoryLimit, check.DeepEquals, quantity.Size(1000))
		return fmt.Errorf("boom")
	})

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "booze",
		Parent:    "foo",
		Snaps:     []string{"bar"},
		MaxMemory: 1000,
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `boom`)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaHappy(c *check.C) {
	var called int
	daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
		called++
		c.Check(name, check.Equals, "booze")
		c.Check(parentName, check.Equals, "foo")
		c.Check(snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(memoryLimit, check.DeepEquals, quantity.Size(1000))
		return nil
	})

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "booze",
		Parent:    "foo",
		Snaps:     []string{"some-snap"},
		MaxMemory: 1000,
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(called, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostRemoveQuotaHappy(c *check.C) {
	var called int
	daemon.MockServicestateRemoveQuota(func(st *state.State, name string) error {
		called++
		c.Check(name, check.Equals, "booze")
		return nil
	})

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	s.asRootAuth(req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, check.Equals, 200)
	c.Assert(called, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostRemoveQuotaUnhappy(c *check.C) {
	daemon.MockServicestateRemoveQuota(func(st *state.State, name string) error {
		c.Check(name, check.Equals, "booze")
		return fmt.Errorf("boom")
	})

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `boom`)
}

func (s *systemsSuite) TestPostQuotaRequiresRoot(c *check.C) {
	s.daemon(c)

	daemon.MockServicestateRemoveQuota(func(st *state.State, name string) error {
		c.Fatalf("remove quota should not get called")
		return nil
	})

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Check(rec.Code, check.Equals, 403)
}

func (s *apiQuotaSuite) TestListQuotas(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/quotas", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []daemon.QuotaGroupResultJSON{})
	res := rsp.Result.([]daemon.QuotaGroupResultJSON)
	c.Check(res, check.DeepEquals, []daemon.QuotaGroupResultJSON{
		{
			GroupName: "bar",
			Parent:    "foo",
			MaxMemory: 1000,
		},
		{
			GroupName: "baz",
			Parent:    "foo",
			MaxMemory: 2000,
		},
		{
			GroupName: "foo",
			SubGroups: []string{"bar", "baz"},
			MaxMemory: 9000,
		},
	})
}

func (s *apiQuotaSuite) TestGetQuota(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/quotas/bar", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, daemon.QuotaGroupResultJSON{})
	res := rsp.Result.(daemon.QuotaGroupResultJSON)
	c.Check(res, check.DeepEquals, daemon.QuotaGroupResultJSON{
		GroupName: "bar",
		Parent:    "foo",
		MaxMemory: 1000,
	})
}

func (s *apiQuotaSuite) TestGetQuotaInvalidName(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/quotas/000", nil)
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `invalid quota group name: .*`)
}

func (s *apiQuotaSuite) TestGetQuotaNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/quotas/unknown", nil)
	c.Assert(err, check.IsNil)
	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 404)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, `cannot find quota group "unknown"`)
}
