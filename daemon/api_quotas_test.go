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

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
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
	err := servicestate.CreateQuota(st, "foo", "", nil, 11000)
	c.Assert(err, check.IsNil)
	err = servicestate.CreateQuota(st, "bar", "foo", nil, 6000)
	c.Assert(err, check.IsNil)
	err = servicestate.CreateQuota(st, "baz", "foo", nil, 5000)
	c.Assert(err, check.IsNil)
}

func (s *apiQuotaSuite) TestPostQuotaUnknownAction(c *check.C) {
	data, err := json.Marshal(daemon.PostQuotaGroupData{Action: "foo", GroupName: "bar"})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `unknown quota action "foo"`)
}

func (s *apiQuotaSuite) TestPostQuotaInvalidGroupName(c *check.C) {
	data, err := json.Marshal(daemon.PostQuotaGroupData{Action: "ensure", GroupName: "$$$"})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `invalid quota group name: .*`)
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
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `boom`)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateHappy(c *check.C) {
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

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateHappy(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	err := servicestate.CreateQuota(st, "ginger-ale", "", nil, 5000)
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) error {
		c.Errorf("should not have called create quota")
		return fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.QuotaGroupUpdate) error {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.QuotaGroupUpdate{
			AddSnaps:       []string{"some-snap"},
			NewMemoryLimit: 9000,
		})
		return nil
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "ginger-ale",
		Snaps:     []string{"some-snap"},
		MaxMemory: 9000,
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(updateCalled, check.Equals, 1)
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
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `boom`)
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

	calls := 0
	r := daemon.MockGetQuotaMemUsage(func(grp *quota.Group) (quantity.Size, error) {
		calls++
		switch grp.Name {
		case "bar":
			return quantity.Size(500), nil
		case "baz":
			return quantity.Size(1000), nil
		case "foo":
			return quantity.Size(5000), nil
		default:
			c.Errorf("unexpected call to get group memory usage for group %q", grp.Name)
			return 0, fmt.Errorf("broken test")
		}
	})
	defer r()
	defer func() {
		c.Assert(calls, check.Equals, 3)
	}()

	req, err := http.NewRequest("GET", "/v2/quotas", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.QuotaGroupResult{})
	res := rsp.Result.([]client.QuotaGroupResult)
	c.Check(res, check.DeepEquals, []client.QuotaGroupResult{
		{
			GroupName:     "bar",
			Parent:        "foo",
			MaxMemory:     6000,
			CurrentMemory: 500,
		},
		{
			GroupName:     "baz",
			Parent:        "foo",
			MaxMemory:     5000,
			CurrentMemory: 1000,
		},
		{
			GroupName:     "foo",
			Subgroups:     []string{"bar", "baz"},
			MaxMemory:     11000,
			CurrentMemory: 5000,
		},
	})
}

func (s *apiQuotaSuite) TestGetQuota(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	calls := 0
	r := daemon.MockGetQuotaMemUsage(func(grp *quota.Group) (quantity.Size, error) {
		calls++
		c.Assert(grp.Name, check.Equals, "bar")
		return quantity.Size(500), nil
	})
	defer r()
	defer func() {
		c.Assert(calls, check.Equals, 1)
	}()

	req, err := http.NewRequest("GET", "/v2/quotas/bar", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, client.QuotaGroupResult{})
	res := rsp.Result.(client.QuotaGroupResult)
	c.Check(res, check.DeepEquals, client.QuotaGroupResult{
		GroupName:     "bar",
		Parent:        "foo",
		MaxMemory:     6000,
		CurrentMemory: 500,
	})
}

func (s *apiQuotaSuite) TestGetQuotaInvalidName(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/quotas/000", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `invalid quota group name: .*`)
}

func (s *apiQuotaSuite) TestGetQuotaNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/quotas/unknown", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Matches, `cannot find quota group "unknown"`)
}
