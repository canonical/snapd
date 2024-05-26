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
	"time"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/gadget/quantity"
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
	mylog.Check(servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil, quota.NewResourcesBuilder().WithMemoryLimit(16*quantity.SizeMiB).Build()))
	c.Assert(err, check.IsNil)
	mylog.Check(servicestatetest.MockQuotaInState(st, "bar", "foo", nil, []string{"test-snap.svc1"}, quota.NewResourcesBuilder().WithMemoryLimit(4*quantity.SizeMiB).Build()))
	c.Assert(err, check.IsNil)
	mylog.Check(servicestatetest.MockQuotaInState(st, "baz", "foo", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))
	c.Assert(err, check.IsNil)
}

func (s *apiQuotaSuite) TestCreateQuotaValues(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, nil,
		quota.NewResourcesBuilder().
			WithMemoryLimit(quantity.SizeMiB).
			WithCPUCount(1).
			WithCPUPercentage(100).
			WithThreadLimit(256).
			WithCPUSet([]int{0, 1}).
			WithJournalRate(150, time.Second).
			WithJournalSize(quantity.SizeMiB).
			Build()))
	allGroups, err2 := servicestate.AllQuotas(st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(err2, check.IsNil)

	c.Check(allGroups, check.HasLen, 1)

	grp := allGroups["ginger-ale"]
	c.Check(grp, check.NotNil)

	quotaValues := daemon.CreateQuotaValues(grp)
	c.Check(quotaValues.Memory, check.DeepEquals, quantity.SizeMiB)
	c.Check(quotaValues.Threads, check.DeepEquals, 256)
	c.Check(quotaValues.CPU, check.DeepEquals, &client.QuotaCPUValues{
		Count:      1,
		Percentage: 100,
	})
	c.Check(quotaValues.CPUSet, check.DeepEquals, &client.QuotaCPUSetValues{
		CPUs: []int{0, 1},
	})
	c.Check(quotaValues.Journal, check.DeepEquals, &client.QuotaJournalValues{
		Size: quantity.SizeMiB,
		QuotaJournalRate: &client.QuotaJournalRate{
			RateCount:  150,
			RatePeriod: time.Second,
		},
	})
}

func (s *apiQuotaSuite) TestPostQuotaUnknownAction(c *check.C) {
	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{Action: "foo", GroupName: "bar"}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `unknown quota action "foo"`)
}

func (s *apiQuotaSuite) TestPostQuotaInvalidGroupName(c *check.C) {
	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{Action: "ensure", GroupName: "$$$"}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `invalid quota group name: .*`)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUnhappy(c *check.C) {
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Check(name, check.Equals, "booze")
		c.Check(createOpts.ParentName, check.Equals, "foo")
		c.Check(createOpts.Snaps, check.DeepEquals, []string{"bar"})
		c.Check(createOpts.ResourceLimits, check.DeepEquals, quota.NewResourcesBuilder().WithMemoryLimit(quantity.Size(1000)).Build())
		return nil, fmt.Errorf("boom")
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"bar"},
		Constraints: client.QuotaValues{Memory: quantity.Size(1000)},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `cannot create quota group: boom`)
	c.Assert(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateHappy(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		createCalled++
		c.Check(name, check.Equals, "booze")
		c.Check(createOpts.ParentName, check.Equals, "foo")
		c.Check(createOpts.Snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(createOpts.ResourceLimits, check.DeepEquals, quota.NewResourcesBuilder().WithMemoryLimit(quantity.Size(1000)).Build())
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: quantity.Size(1000)},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(createCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateQuotaConflicts(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Check(name, check.Equals, "booze")
		c.Check(createOpts.ParentName, check.Equals, "foo")
		c.Check(createOpts.Snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(createOpts.ResourceLimits, check.DeepEquals, quota.NewResourcesBuilder().WithMemoryLimit(quantity.Size(1000)).Build())

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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "booze",
		Parent:      "foo",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: 1000},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "booze" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "booze",
	})

	req = mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateServicesHappy(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		createCalled++
		c.Check(name, check.Equals, "booze")
		c.Check(createOpts.ParentName, check.Equals, "foo")
		c.Check(createOpts.Snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(createOpts.Services, check.DeepEquals, []string{"some-snap.svc1"})
		c.Check(createOpts.ResourceLimits, check.DeepEquals, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "booze",
		Parent:    "foo",
		Snaps:     []string{"some-snap"},
		Services:  []string{"some-snap.svc1"},
		Constraints: client.QuotaValues{
			Memory: quantity.SizeGiB,
		},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(createCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaCreateJournalRateZeroHappy(c *check.C) {
	var createCalled int
	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		createCalled++
		c.Check(name, check.Equals, "booze")
		c.Check(createOpts.ParentName, check.Equals, "foo")
		c.Check(createOpts.Snaps, check.DeepEquals, []string{"some-snap"})
		c.Check(createOpts.ResourceLimits, check.DeepEquals, quota.NewResourcesBuilder().WithJournalRate(0, 0).Build())
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "booze",
		Parent:    "foo",
		Snaps:     []string{"some-snap"},
		Constraints: client.QuotaValues{
			Journal: &client.QuotaJournalValues{
				QuotaJournalRate: &client.QuotaJournalRate{
					RateCount:  0,
					RatePeriod: 0,
				},
			},
		},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(createCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateCpuHappy(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, nil,
		quota.NewResourcesBuilder().
			WithMemoryLimit(quantity.SizeMiB).
			WithCPUCount(1).
			WithCPUPercentage(100).
			WithThreadLimit(256).
			Build()))
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.UpdateQuotaOptions) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.UpdateQuotaOptions{
			AddSnaps:          []string{"some-snap"},
			NewResourceLimits: quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(100).WithThreadLimit(512).Build(),
		})
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "ginger-ale",
		Snaps:     []string{"some-snap"},
		Constraints: client.QuotaValues{
			CPU: &client.QuotaCPUValues{
				Count:      2,
				Percentage: 100,
			},
			Threads: 512,
		},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(updateCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateCpu2Happy(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, nil,
		quota.NewResourcesBuilder().
			WithMemoryLimit(quantity.SizeMiB).
			WithCPUCount(1).
			WithCPUPercentage(100).
			WithThreadLimit(256).
			Build()))
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.UpdateQuotaOptions) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.UpdateQuotaOptions{
			AddSnaps:          []string{"some-snap"},
			NewResourceLimits: quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0, 1}).Build(),
		})
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "ginger-ale",
		Snaps:     []string{"some-snap"},
		Constraints: client.QuotaValues{
			CPU: &client.QuotaCPUValues{
				Count:      1,
				Percentage: 100,
			},
			CPUSet: &client.QuotaCPUSetValues{
				CPUs: []int{0, 1},
			},
		},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(updateCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateMemoryHappy(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, nil,
		quota.NewResourcesBuilder().
			WithMemoryLimit(quantity.SizeMiB).
			WithCPUCount(1).
			WithCPUPercentage(100).
			WithThreadLimit(256).
			Build()))
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.UpdateQuotaOptions) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.UpdateQuotaOptions{
			AddSnaps:          []string{"some-snap"},
			NewResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(9000).Build(),
		})
		ts := state.NewTaskSet(st.NewTask("foo-quota", "..."))
		return ts, nil
	})
	defer r()

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "ensure",
		GroupName: "ginger-ale",
		Snaps:     []string{"some-snap"},
		Constraints: client.QuotaValues{
			Memory: quantity.Size(9000),
		},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rsp := s.asyncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Assert(updateCalled, check.Equals, 1)
	c.Assert(s.ensureSoonCalled, check.Equals, 1)
}

func (s *apiQuotaSuite) TestPostEnsureQuotaUpdateConflicts(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(650*quantity.SizeKiB).Build()))
	st.Unlock()
	c.Assert(err, check.IsNil)

	r := daemon.MockServicestateCreateQuota(func(st *state.State, name string, createOpts servicestate.CreateQuotaOptions) (*state.TaskSet, error) {
		c.Errorf("should not have called create quota")
		return nil, fmt.Errorf("broken test")
	})
	defer r()

	updateCalled := 0
	r = daemon.MockServicestateUpdateQuota(func(st *state.State, name string, opts servicestate.UpdateQuotaOptions) (*state.TaskSet, error) {
		updateCalled++
		c.Assert(name, check.Equals, "ginger-ale")
		c.Assert(opts, check.DeepEquals, servicestate.UpdateQuotaOptions{
			AddSnaps:          []string{"some-snap"},
			NewResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.Size(800 * quantity.SizeKiB)).Build(),
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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:      "ensure",
		GroupName:   "ginger-ale",
		Snaps:       []string{"some-snap"},
		Constraints: client.QuotaValues{Memory: 800 * quantity.SizeKiB},
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "ginger-ale" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "ginger-ale",
	})

	req = mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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
	mylog.Check(servicestatetest.MockQuotaInState(st, "ginger-ale", "", []string{"some-snap"}, nil, quota.NewResourcesBuilder().WithMemoryLimit(650*quantity.SizeKiB).Build()))
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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 409)
	c.Check(rspe.Message, check.Equals, `quota group "booze" has "quota-control" change in progress`)
	c.Check(rspe.Value, check.DeepEquals, map[string]interface{}{
		"change-kind": "quota-control",
		"quota-name":  "booze",
	})

	req = mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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

	data := mylog.Check2(json.Marshal(daemon.PostQuotaGroupData{
		Action:    "remove",
		GroupName: "booze",
	}))
	c.Assert(err, check.IsNil)

	req := mylog.Check2(http.NewRequest("POST", "/v2/quotas", bytes.NewBuffer(data)))
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
	r := daemon.MockGetQuotaUsage(func(grp *quota.Group) (*client.QuotaValues, error) {
		calls++
		switch grp.Name {
		case "bar":
			return &client.QuotaValues{
				Memory: quantity.Size(500),
			}, nil
		case "baz":
			return &client.QuotaValues{
				Memory: quantity.Size(1000),
			}, nil
		case "foo":
			return &client.QuotaValues{
				Memory: quantity.Size(5000),
			}, nil
		default:
			c.Errorf("unexpected call to get group memory usage for group %q", grp.Name)
			return nil, fmt.Errorf("broken test")
		}
	})
	defer r()
	defer func() {
		c.Assert(calls, check.Equals, 3)
	}()

	req := mylog.Check2(http.NewRequest("GET", "/v2/quotas", nil))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.QuotaGroupResult{})
	res := rsp.Result.([]client.QuotaGroupResult)
	c.Check(res, check.DeepEquals, []client.QuotaGroupResult{
		{
			GroupName:   "bar",
			Parent:      "foo",
			Services:    []string{"test-snap.svc1"},
			Constraints: &client.QuotaValues{Memory: 4 * quantity.SizeMiB},
			Current:     &client.QuotaValues{Memory: quantity.Size(500)},
		},
		{
			GroupName:   "baz",
			Parent:      "foo",
			Constraints: &client.QuotaValues{Memory: quantity.SizeMiB},
			Current:     &client.QuotaValues{Memory: quantity.Size(1000)},
		},
		{
			GroupName:   "foo",
			Subgroups:   []string{"bar", "baz"},
			Snaps:       []string{"test-snap"},
			Constraints: &client.QuotaValues{Memory: 16 * quantity.SizeMiB},
			Current:     &client.QuotaValues{Memory: quantity.Size(5000)},
		},
	})
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestListJournalQuotas(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mylog.Check(servicestatetest.MockQuotaInState(st, "foo", "", nil, nil, quota.NewResourcesBuilder().WithJournalSize(64*quantity.SizeMiB).Build()))
	c.Assert(err, check.IsNil)
	mylog.Check(servicestatetest.MockQuotaInState(st, "bar", "foo", nil, nil, quota.NewResourcesBuilder().WithJournalRate(100, time.Hour).Build()))
	c.Assert(err, check.IsNil)
	mylog.Check(servicestatetest.MockQuotaInState(st, "baz", "foo", nil, nil, quota.NewResourcesBuilder().WithJournalRate(0, 0).Build()))
	c.Assert(err, check.IsNil)
	st.Unlock()

	calls := 0
	r := daemon.MockGetQuotaUsage(func(grp *quota.Group) (*client.QuotaValues, error) {
		calls++
		return &client.QuotaValues{}, nil
	})
	defer r()
	defer func() {
		c.Assert(calls, check.Equals, 3)
	}()

	req := mylog.Check2(http.NewRequest("GET", "/v2/quotas", nil))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.QuotaGroupResult{})
	res := rsp.Result.([]client.QuotaGroupResult)
	c.Check(res, check.DeepEquals, []client.QuotaGroupResult{
		{
			GroupName: "bar",
			Parent:    "foo",
			Constraints: &client.QuotaValues{Journal: &client.QuotaJournalValues{
				QuotaJournalRate: &client.QuotaJournalRate{
					RateCount:  100,
					RatePeriod: time.Hour,
				},
			}},
			Current: &client.QuotaValues{},
		},
		{
			GroupName: "baz",
			Parent:    "foo",
			Constraints: &client.QuotaValues{Journal: &client.QuotaJournalValues{
				QuotaJournalRate: &client.QuotaJournalRate{
					RateCount:  0,
					RatePeriod: 0,
				},
			}},
			Current: &client.QuotaValues{},
		},
		{
			GroupName: "foo",
			Subgroups: []string{"bar", "baz"},
			Constraints: &client.QuotaValues{Journal: &client.QuotaJournalValues{
				Size: 64 * quantity.SizeMiB,
			}},
			Current: &client.QuotaValues{},
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
	r := daemon.MockGetQuotaUsage(func(grp *quota.Group) (*client.QuotaValues, error) {
		calls++
		c.Assert(grp.Name, check.Equals, "bar")
		return &client.QuotaValues{
			Memory: quantity.Size(500),
		}, nil
	})
	defer r()
	defer func() {
		c.Assert(calls, check.Equals, 1)
	}()

	req := mylog.Check2(http.NewRequest("GET", "/v2/quotas/bar", nil))
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, client.QuotaGroupResult{})
	res := rsp.Result.(client.QuotaGroupResult)
	c.Check(res, check.DeepEquals, client.QuotaGroupResult{
		GroupName:   "bar",
		Parent:      "foo",
		Services:    []string{"test-snap.svc1"},
		Constraints: &client.QuotaValues{Memory: quantity.Size(4194304)},
		Current:     &client.QuotaValues{Memory: quantity.Size(500)},
	})

	c.Check(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestGetQuotaInvalidName(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	mockQuotas(st, c)
	st.Unlock()

	req := mylog.Check2(http.NewRequest("GET", "/v2/quotas/000", nil))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `invalid quota group name: .*`)
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}

func (s *apiQuotaSuite) TestGetQuotaNotFound(c *check.C) {
	req := mylog.Check2(http.NewRequest("GET", "/v2/quotas/unknown", nil))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Matches, `cannot find quota group "unknown"`)
	c.Check(s.ensureSoonCalled, check.Equals, 0)
}
