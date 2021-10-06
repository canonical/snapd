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
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/systemd"
)

var _ = check.Suite(&apiQuotaSuite{})

type apiQuotaSuite struct {
	apiBaseSuite

	ensureSoonCalled int
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

	r := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(r)

	// POST requires root
	s.expectedWriteAccess = daemon.RootAccess{}

	s.ensureSoonCalled = 0
	_, r = daemon.MockEnsureStateSoon(func(st *state.State) {
		s.ensureSoonCalled++
	})
	s.AddCleanup(r)
}

func mockQuotas(st *state.State, c *check.C) {
	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, 11000)
	c.Assert(err, check.IsNil)
	err = servicestatetest.MockQuotaInState(st, "bar", "foo", nil, 6000)
	c.Assert(err, check.IsNil)
	err = servicestatetest.MockQuotaInState(st, "baz", "foo", nil, 5000)
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
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
		c.Check(name, check.Equals, "booze")
		c.Check(parentName, check.Equals, "foo")
		c.Check(snaps, check.DeepEquals, []string{"bar"})
		c.Check(memoryLimit, check.DeepEquals, quantity.Size(1000))
		return nil, fmt.Errorf("boom")
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"bar"},
		Constraints: client.QuotaValues{Memory: quantity.Size(1000)},
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `cannot create quota group: boom`)
	c.Assert(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateHappy(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
		createCalled++
		c.Check(name, check.Equals, "booze")
		c.Check(parentName, check.Equals, "foo")
		c.Check(snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(memoryLimit, check.DeepEquals, quantity.Size(1000))
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: quantity.Size(1000)},
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(createCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateQuotaConflicts(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
		c.Check(name, check.Equals, "booze")
		c.Check(parentName, check.Equals, "foo")
		c.Check(snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(memoryLimit, check.DeepEquals, quantity.Size(1000))

		createCalled++
		switch createCalled {
		case 1:
			// return a quota conflict as if we were trying to create this quota in
			// another task
			return nil, &servicestate.QuotaChangeConflictError{Quota: "booze", ChangeKind: "quota-control"}
		case 2:
			// return a snap conflict as if we were trying to disable the
			// some-snap in the quota group to be created
			return nil, &snapstate.ChangeConflictError{Snap: "some-snap", ChangeKind: "disable"}
		default:
			c.Errorf("test broken")
			return nil, fmt.Errorf("test broken")
		}
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: 1000},
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "booze" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "booze",
	})

	req, err = http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)

	rspe = s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `snap "some-snap" has "disable" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "disable",
		"snap-name":   "some-snap",
	})

	c.Assert(createCalled, check.Equals, 2)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateHappy(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	err := servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, 5000)
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.QuotaGroupUpdate) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.QuotaGroupUpdate{
			AddSnaps:       []string{"some-snap"},
			NewMemoryLimit: 9000,
		})
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "ginger-ale",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: quantity.Size(9000)},
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(updateCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateConflicts(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	err := servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, 5000)
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, parentName string, snaps []string, memoryLimit quantity.Size) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.QuotaGroupUpdate) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.QuotaGroupUpdate{
			AddSnaps:       []string{"some-snap"},
			NewMemoryLimit: 9000,
		})
		switch updateCalled {
		case 1:
			// return a quota conflict as if we were trying to update this quota
			// in another task
			return nil, &servicestate.QuotaChangeConflictError{Quota: "ginger-ale", ChangeKind: "quota-control"}
		case 2:
			// return a snap conflict as if we were trying to disable the
			// some-snap in the quota group to be added to the group
			return nil, &snapstate.ChangeConflictError{Snap: "some-snap", ChangeKind: "disable"}
		default:
			c.Errorf("test broken")
			return nil, fmt.Errorf("test broken")
		}
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "ginger-ale",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: 9000},
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "ginger-ale" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "ginger-ale",
	})

	req, err = http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)

	rspe = s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `snap "some-snap" has "disable" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "disable",
		"snap-name":   "some-snap",
	})

	c.Assert(updateCalled, check.Equals, 2)
}

func (s *apiQuotaSuite) TestPostRemoveQuotaHappy(c *check.C) {
	var removeCalled int
	r := daemon.MockServicestateRemoveQuota(func(st *state.State, name string) (*state.TaskSet, error) {
		removeCalled++
		c.Check(name, check.Equals, "booze")
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

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
	c.Assert(rec.Code, check.Equals, 202)
	c.Assert(removeCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostRemoveQuotaConflict(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	err := servicestatetest.MockQuotaInState(st, "ginger-ale", "", []string{"some-snap"}, 5000)
	st.Unlock()
	c.Assert(err, check.IsNil)

	var removeCalled int
	r := daemon.MockServicestateRemoveQuota(func(st *state.State, name string) (*state.TaskSet, error) {
		removeCalled++
		c.Check(name, check.Equals, "booze")
		switch removeCalled {
		case 1:
			// return a quota conflict as if we were trying to update this quota
			// in another task
			return nil, &servicestate.QuotaChangeConflictError{Quota: "booze", ChangeKind: "quota-control"}
		case 2:
			// return a snap conflict as if we were trying to disable the
			// some-snap in the quota group to be added to the group
			return nil, &snapstate.ChangeConflictError{Snap: "some-snap", ChangeKind: "disable"}
		default:
			c.Errorf("test broken")
			return nil, fmt.Errorf("test broken")
		}
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "booze" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "booze",
	})

	req, err = http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)

	rspe = s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `snap "some-snap" has "disable" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "disable",
		"snap-name":   "some-snap",
	})

	c.Assert(removeCalled, check.Equals, 2)
}

func (s *apiQuotaSuite) TestPostRemoveQuotaUnhappy(c *check.C) {
	r := daemon.MockServicestateRemoveQuota(func(st *state.State, name string) (*state.TaskSet, error) {
		c.Check(name, check.Equals, "booze")
		return nil, fmt.Errorf("boom")
	})
	defer r()

	data, err := json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	})
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `cannot remove quota group: boom`)
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestPostQuotaRequiresRoot(c *check.C) {
	r := daemon.MockServicestateRemoveQuota(func(st *state.State, name string) (*state.TaskSet, error) {
		c.Fatalf("remove quota should not get called")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

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
	c.Check(s.ensureSoonCalled, check.Equals, 0)
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
			GroupName:   "bar",
			Parent:      "foo",
			Constraints: &client.QuotaValues{Memory: quantity.Size(6000)},
			Current:     &client.QuotaValues{Memory: quantity.Size(500)},
		},
		{
			GroupName:   "baz",
			Parent:      "foo",
			Constraints: &client.QuotaValues{Memory: quantity.Size(5000)},
			Current:     &client.QuotaValues{Memory: quantity.Size(1000)},
		},
		{
			GroupName:   "foo",
			Subgroups:   []string{"bar", "baz"},
			Constraints: &client.QuotaValues{Memory: quantity.Size(11000)},
			Current:     &client.QuotaValues{Memory: quantity.Size(5000)},
		},
	})
	c.Check(s.ensureSoonCalled, check.Equals, 0)
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
		GroupName:   "bar",
		Parent:      "foo",
		Constraints: &client.QuotaValues{Memory: quantity.Size(6000)},
		Current:     &client.QuotaValues{Memory: quantity.Size(500)},
	})

	c.Check(s.ensureSoonCalled, check.Equals, 0)
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
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestGetQuotaNotFound(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/quotas/unknown", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Matches, `cannot find quota group "unknown"`)
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}
