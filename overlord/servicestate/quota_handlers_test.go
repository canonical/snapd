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

package servicestate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/snaptest"
)

type quotaHandlersSuite struct {
	baseServiceMgrTestSuite
}

var _ = Suite(&quotaHandlersSuite{})

func (s *quotaHandlersSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)

	// we don't need the EnsureSnapServices ensure loop to run by default
	servicestate.MockEnsuredSnapServices(s.mgr, true)

	// we enable quota-groups by default
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	// mock that we have a new enough version of systemd by default
	r := servicestate.MockSystemdVersion(248)
	s.AddCleanup(r)
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreate(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// doQuotaControl handler to create the group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// make a fake task
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdate(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which updates the group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	err := servicestate.CreateQuota(st, "foo-group", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// create a task for updating the quota group
	t := st.NewTask("update-quota", "...")

	// update the memory limit to be double
	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "update",
			QuotaName:   "foo-group",
			MemoryLimit: 2 * quantity.SizeGiB,
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemove(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which removes the group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStop("foo-group"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	err := servicestate.CreateQuota(st, "foo-group", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// create a task for removing the quota group
	t := st.NewTask("remove-quota", "...")

	// update the memory limit to be double
	qcs := []servicestate.QuotaControlAction{
		{
			Action:    "remove",
			QuotaName: "foo-group",
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, nil)
}
