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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"
	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type quotaHandlersSuite struct {
	baseServiceMgrTestSuite
}

var _ = Suite(&quotaHandlersSuite{})

func (s *quotaHandlersSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)

	// we don't need the EnsureSnapServices ensure loop to run by default
	servicestate.MockEnsuredSnapServices(s.mgr, true)

	s.state.Lock()
	defer s.state.Unlock()

	// mock that we have a new enough version of systemd by default
	systemdRestore := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(systemdRestore)

	usabilityErrRestore := servicestate.EnsureQuotaUsability()
	s.AddCleanup(usabilityErrRestore)
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

	qcs := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) getTasksOfKind(chg *state.Change, kind string) []*state.Task {
	var tasks []*state.Task
	for _, t := range chg.Tasks() {
		if t.Kind() == kind {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

func (s *quotaHandlersSuite) runRestartTasks(tasks []*state.Task) error {
	for _, t := range tasks {
		if err := s.o.ServiceManager().DoServiceControl(t, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreateRestartOK(c *C) {
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// doQuotaControl handler to create the group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),
		// after task restart
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// make a fake change with a single task
	chg := st.NewChange("test", "")
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:         "create",
			QuotaName:      "foo-group",
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			AddSnaps:       []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	expectedQuotaState := map[string]quotaGroupState{
		"foo-group": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	}
	st.Unlock()

	err := s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	t.SetStatus(state.DoingStatus)
	restartTasks := s.getTasksOfKind(chg, "service-control")
	st.Unlock()

	err = s.runRestartTasks(restartTasks)
	c.Assert(err, IsNil)

	st.Lock()

	checkQuotaState(c, st, expectedQuotaState)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)
	st.Unlock()

	err = s.runRestartTasks(restartTasks)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)
	checkQuotaState(c, st, expectedQuotaState)
}

func (s *quotaHandlersSuite) TestQuotaStateAlreadyUpdatedBehavior(c *C) {
	// test a situation where because of restart the task is reentered
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

	// make a fake change with a task
	chg := st.NewChange("test", "")
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:         "create",
			QuotaName:      "foo-group",
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			AddSnaps:       []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)
	st.Unlock()

	err := s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)
	c.Assert(len(chg.Tasks()), Equals, 2)

	t.SetStatus(state.DoingStatus)
	restartTasks := s.getTasksOfKind(chg, "service-control")
	st.Unlock()

	err = s.runRestartTasks(restartTasks)

	st.Lock()
	c.Assert(err, IsNil)

	updated, appsToRestart, _, err := servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Assert(appsToRestart, HasLen, 1)
	for info, apps := range appsToRestart {
		c.Check(info.InstanceName(), Equals, "test-snap")
		c.Assert(apps, HasLen, 1)
		c.Check(apps[0], Equals, info.Apps["svc1"])
	}

	// rebooted
	r = servicestate.MockOsutilBootID("other-boot")
	defer r()

	updated, appsToRestart, _, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Check(appsToRestart, HasLen, 0)
	r()

	// restored
	_, appsToRestart, _, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(appsToRestart, HasLen, 1)

	// snap went missing
	snapstate.Set(s.state, "test-snap", nil)
	updated, appsToRestart, _, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Check(appsToRestart, HasLen, 0)
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

	qcs := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	// update the memory limit to be double
	qcs = servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	}

	err = s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdateRestartOK(c *C) {
	// test a situation where because of restart the task is reentered
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

	qcs := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	// update the memory limit to be double
	qcs = servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	}

	expectedQuotaState := map[string]quotaGroupState{
		"foo-group": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
			Snaps:          []string{"test-snap"},
		},
	}

	err = s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, expectedQuotaState)

	err = s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, expectedQuotaState)
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

	qcs := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	// remove quota group
	qcs = servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo-group",
	}

	err = s.callDoQuotaControl(&qcs)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, nil)
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemoveRestartOK(c *C) {
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which removes the group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStop("foo-group"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),
		// after task restart
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
	chg := st.NewChange("remove-quota", "...")
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:         "create",
			QuotaName:      "foo-group",
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			AddSnaps:       []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)
	st.Unlock()

	err := s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	restartTasks := s.getTasksOfKind(chg, "service-control")
	st.Unlock()

	err = s.runRestartTasks(restartTasks)
	c.Assert(err, IsNil)

	st.Lock()
	// create a change and a task for removing the quota group
	chg = st.NewChange("remove-quota", "...")
	t = st.NewTask("remove-quota", "...")

	// remove quota group
	qcs = []servicestate.QuotaControlAction{
		{
			Action:    "remove",
			QuotaName: "foo-group",
		},
	}

	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)
	st.Unlock()

	err = s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	t.SetStatus(state.DoingStatus)
	restartTasks = s.getTasksOfKind(chg, "service-control")
	st.Unlock()

	err = s.runRestartTasks(restartTasks)
	c.Assert(err, IsNil)

	st.Lock()

	checkQuotaState(c, st, nil)

	st.Unlock()

	err = s.o.ServiceManager().DoQuotaControl(t, nil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)
	st.Unlock()

	err = s.runRestartTasks(restartTasks)
	c.Assert(err, IsNil)

	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, nil)
}

func (s *quotaHandlersSuite) callDoQuotaControl(action *servicestate.QuotaControlAction) error {
	st := s.state
	qcs := []*servicestate.QuotaControlAction{action}
	chg := st.NewChange("quota-control", "...")
	t := st.NewTask("quota-task", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer st.Lock()

	if err := s.o.ServiceManager().DoQuotaControl(t, nil); err != nil {
		return err
	}
	st.Lock()
	restartTasks := s.getTasksOfKind(chg, "service-control")
	st.Unlock()
	return s.runRestartTasks(restartTasks)
}

func (s *quotaHandlersSuite) TestQuotaCreatePreseeding(c *C) {
	// should be no systemctl calls since we are preseeding
	r := snapdenv.MockPreseeding(true)
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// now we can create the quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaCreate(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for non-installed snap - fails

		// CreateQuota for foo - success
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2 with overlapping snap already in foo

		// CreateQuota for foo again - fails
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// trying to create a quota with a snap that doesn't exist fails
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `cannot use snap "test-snap" in group "foo": snap "test-snap" is not installed`)

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(4 * quantity.SizeKiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	// trying to create a quota with too low of a memory limit fails
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, ErrorMatches, `memory limit 4096 is too small: size must be larger than 640 KiB`)

	// but with an adequately sized memory limit, and a snap that exists, we can
	// create it
	qc3 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(640*quantity.SizeKiB + 1).Build(),
		AddSnaps:       []string{"test-snap"},
	}
	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(640*quantity.SizeKiB + 1).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoCreateSubGroupQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo - no systemctl calls since no snaps in it

		// CreateQuota for foo2 - fails thus no systemctl calls

		// CreateQuota for foo2 - we don't write anything for the first quota
		// since there are no snaps in the quota to track
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo-group"),
		systemctlCallsForSliceStart("foo-group/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group with no snaps to be the parent
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-group",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// trying to create a quota group with a non-existent parent group fails
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		ParentName:     "foo-non-real",
		AddSnaps:       []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, ErrorMatches, `cannot create group under non-existent parent group "foo-non-real"`)

	// trying to create a quota group with too big of a limit to fit inside the
	// parent fails
	qc3 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
		ParentName:     "foo-group",
		AddSnaps:       []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside group \"foo-group\" remaining quota space 1 GiB`)

	// now we can create a sub-quota
	qc4 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		ParentName:     "foo-group",
		AddSnaps:       []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			ParentGroup:    "foo-group",
		},
	})

	// foo-group exists as a slice too, but has no snap services in the slice
	checkSliceState(c, systemd.EscapeUnitNamePath("foo-group"),
		quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
}

func (s *quotaHandlersSuite) TestQuotaRemove(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo - no systemctl calls since there are no snaps
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),

		// for CreateQuota foo2
		systemctlCallsForSliceStart("foo/foo2"),

		// for CreateQuota foo3 - no systemctl calls since there are no snaps

		// for RemoveQuota foo3
		systemctlCallsForServiceRestart("test-snap"),
		systemctlCallsForSliceStop("foo/foo3"),

		// RemoveQuota for foo2 - expect daemon reloads due to snap being in group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStop("foo/foo2"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// RemoveQuota for foo
		systemctlCallsForServiceRestart("test-snap"),
		systemctlCallsForSliceStop("foo"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// trying to remove a group that does not exist fails
	qc := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "not-exists",
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `cannot remove non-existent quota group "not-exists"`)

	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// create 2 quota sub-groups too
	qc3 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
		AddSnaps:       []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	qc4 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2", "foo3"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			Snaps:          []string{"test-snap"},
			ParentGroup:    "foo",
		},
		"foo3": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})

	// try removing the parent and it fails since it still has a sub-group
	// under it
	qc5 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}

	err = s.callDoQuotaControl(&qc5)
	c.Assert(err, ErrorMatches, "cannot remove quota group with sub-groups, remove the sub-groups first")

	// but we can remove the sub-group successfully first
	qc6 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo3",
	}

	err = s.callDoQuotaControl(&qc6)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			Snaps:          []string{"test-snap"},
			ParentGroup:    "foo",
		},
	})

	// and we can remove the other sub-group
	qc7 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo2",
	}

	err = s.callDoQuotaControl(&qc7)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		},
	})

	// now we can remove the quota from the state
	qc8 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}

	err = s.callDoQuotaControl(&qc8)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, nil)

	// foo is not mentioned in the service and doesn't exist
	checkSvcAndSliceState(c, "test-snap.svc1", "foo", quota.NewResourcesBuilder().Build())
}

func (s *quotaHandlersSuite) TestQuotaRemoveWithLimitSetFails(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil,
		quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
	c.Assert(err, IsNil)

	// but we can remove the sub-group successfully first
	qc := servicestate.QuotaControlAction{
		Action:         "remove",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithThreadLimit(16).Build(),
	}

	err = s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `internal error, quota limit options cannot be used with remove action`)
}

func (s *quotaHandlersSuite) TestQuotaSnapMixSubgroupWithSnapsHappy(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// try to create a subgroup in a group that already has snaps
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapMixSnapsWithSubgroupsHappy(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// CreateQuota for foo2
		systemctlCallsForSliceStart("foo/foo2"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	// create a subgroup for the foo group, which has neither snaps or subgroups
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// now we try to add snaps to the foo group which already has subgroups
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2"},
			Snaps:          []string{"test-snap"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToMixSubgroupWithServices(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),
		// CreateQuota for foo2 - foo has changed
		systemctlCallsForServiceRestart("test-snap"),

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create a subgroup in a group that already has snaps
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// then, we try to put another subgroup into the new sub-group, before we add services
	// this should fail as only one level of sub-grouping is allowed with mixed parents
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
		ParentName:     "foo2",
	})
	c.Assert(err, ErrorMatches, `cannot update quota "foo2": group "foo2" is invalid: only one level of sub-groups are allowed for groups with snaps`)

	// and at last, we add services from test-snap into the sub-group, which itself already
	// has a subgroup.
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	// now try to trigger the unmixable error by trying to create a new
	// sub-group in the group we just created services in.
	qc5 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
		ParentName:     "foo2",
	}

	err = s.callDoQuotaControl(&qc5)
	c.Assert(err, ErrorMatches, `cannot mix sub groups with services in the same group`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
			Services:       []string{"test-snap.svc1"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToMixServicesWithSubgroups(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),
		// CreateQuota for foo2 - foo has changed
		systemctlCallsForServiceRestart("test-snap"),

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create a subgroup in a group that already has snaps
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// add services to the new sub group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	// now we try to create a sub-group in foo2, this should fail
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
		ParentName:     "foo2",
	})
	c.Assert(err, ErrorMatches, `cannot mix sub groups with services in the same group`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
			Services:       []string{"test-snap.svc1"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToAddServicesToNewTopGroup(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddServices:    []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "svc1": the snap "test-snap" must be in a direct parent group of group "foo"`)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToAddServicesToExistingTopGroup(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	// Try to add services to that root group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "svc1": the snap "test-snap" must be in a direct parent group of group "foo"`)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToAddServicesToGroupWithSubgroups(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	// Create a sub-group for foo
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// Try to add services to that root group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot mix services and sub groups in the group "foo"`)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToAddServicesToGroupWithJournalQuota(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	// Create a sub-group for foo with a journal limit set
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// Try to add services to that sub-group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot put services into group "foo2": journal quotas are not supported for individual services`)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToAddServicesAndJournalQuotaToGroup(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// Create a sub-group for foo with both a journal limit set and services
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
		AddServices:    []string{"test-snap.svc1"},
		ParentName:     "foo",
	})
	c.Assert(err, ErrorMatches, `cannot update quotas "foo", "foo2": group "foo2" is invalid: journal quota is not supported for individual services`)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToUpdateServicesAndJournalQuotaToGroup(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// Create root group
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// Create empty sub group with no journal limit
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ParentName:     "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
	})
	c.Assert(err, IsNil)

	// Update foo2 with both a journal limit set and services
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
		AddServices:    []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot update limits for group "foo2": journal quotas are not supported for individual services`)
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapServices(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),
		// CreateQuota for foo2 - foo has changed
		systemctlCallsForServiceRestart("test-snap"),

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// UpdateQuota for foo - just the slice changes
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create a subgroup in a group that already has snaps
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// and at last, we add services from test-snap into the sub-group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
			Services:       []string{"test-snap.svc1"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapNestedFails(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// create a subgroup in a group that already has snaps
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// and at last, we add a new snap into that sub-group, this is not allowed
	qc3 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo2",
		AddSnaps:  []string{"test-snap2"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `cannot add snaps to group "foo2": only services are allowed in this sub-group`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapAndServicesFail(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
		AddServices:    []string{"test-snap.svc1"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `cannot mix services and snaps in the same quota group`)
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapAndServicesFailExistingSnaps(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot mix services and snaps in the same quota group`)
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapAndServicesFailExistingServices(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),
		// CreateQuota for foo2 - foo has changed
		systemctlCallsForServiceRestart("test-snap"),

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo-sub
		systemctlCallsForSliceStart("foo/foo-sub"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo-sub",
		ParentName:     "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		AddServices:    []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo-sub",
		AddSnaps:  []string{"test-snap"},
	})
	c.Assert(err, ErrorMatches, `cannot mix services and snaps in the same quota group`)
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapServicesFailOnInvalidSnapService(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create a subgroup in a group that already has snaps
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// now we test both invalid service name, and an invalid snap name
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc-none"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "foo2": invalid service "svc-none"`)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"no-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "foo2": snap "no-snap" is not installed`)

	// also test adding a valid snap, but the snap is not relevant for this quota group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap2.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "svc1": the snap "test-snap2" must be in a direct parent group of group "foo2"`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapAddSnapServicesFailOnServiceTwice(c *C) {
	r := s.mockSystemctlCalls(c, join(

		// CreateQuota for foo, and we put 'test-snap' into foo immediately
		// so expect service restarts for that
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
		systemctlCallsForServiceRestart("test-snap"),

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		// CreateQuota for foo2 and foo3
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForSliceStart("foo/foo3"),

		// UpdateQuota for foo2, we put test-snap.svc1 into foo2
		// so we expect the service to be restarted at this point
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create two subgroups so we can test adding service twice
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	// add the service from test-snap, and then we try to re-add it to the second group
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo3",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap service "svc1": the service is already in group "foo2"`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
			SubGroups:      []string{"foo2", "foo3"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
			Services:       []string{"test-snap.svc1"},
		},
		"foo3": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapCorrectlyDetectErrorsIfCreatingSubgroupsFirst(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// CreateQuota for foo2
		systemctlCallsForSliceStart("foo/foo2"),

		// CreateQuota for foo3
		systemctlCallsForSliceStart("foo/foo2/foo3"),
		systemctlCallsForServiceRestart("test-snap"),

		// UpdateQuota for foo - just the slice changes
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
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// Create 3 levels of quota groups
	//         foo
	//         /  \
	//        X   foo2
	//              \
	//             foo3
	// The theory is, that if we just create the groups before inserting snaps
	// and services, maybe we can end up in a state we don't want. If we insert
	// a snap in foo, then we end up with a group foo3 which will be unusable. This
	// is the intended behavior, it's important to test that we won't be allowed to use it, but
	// that we can indeed remove it
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	})
	c.Assert(err, IsNil)

	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo3",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
		ParentName:     "foo2",
	})
	c.Assert(err, IsNil)

	// Should fail: insert snap into foo. When snaps are mixed with sub-groups
	// then only one level of sub-groups are allowed.
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap"},
	})
	c.Assert(err, ErrorMatches, `cannot update quota "foo": group "foo3" is invalid: only one level of sub-groups are allowed for groups with snaps`)

	// Should work: insert snap into foo2
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo2",
		AddSnaps:  []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// Should fail: insert snap into foo3
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo3",
		AddSnaps:  []string{"test-snap2"},
	})
	c.Assert(err, ErrorMatches, `cannot add snaps to group "foo3": only services are allowed in this sub-group`)

	// Must work: insert services from foo2 into foo3
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo3",
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			Snaps:          []string{"test-snap"},
			ParentGroup:    "foo",
			SubGroups:      []string{"foo3"},
		},
		"foo3": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
			ParentGroup:    "foo2",
			Services:       []string{"test-snap.svc1"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateGroupNotExist(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// non-existent quota group
	qc := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "non-existing",
	}

	err := s.callDoQuotaControl(&qc)
	c.Check(err, ErrorMatches, `group "non-existing" does not exist`)
}

func (s *quotaHandlersSuite) TestQuotaUpdateSubGroupTooBig(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo
		systemctlCallsForSliceStart("foo"),

		// CreateQuota for foo2
		systemctlCallsForSliceStart("foo/foo2"),

		// UpdateQuota for foo2 - just the slice changes
		systemctlCallsForServiceRestart("test-snap"),
		systemctlCallsForServiceRestart("test-snap2"),

		// UpdateQuota for foo2 which fails - no systemctl calls

		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	expFooGroupState := quotaGroupState{
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": expFooGroupState,
	})

	// create a sub-group with 0.5 GiB
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		AddSnaps:       []string{"test-snap", "test-snap2"},
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	expFooGroupState.SubGroups = []string{"foo2"}

	expFoo2GroupState := quotaGroupState{
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		Snaps:          []string{"test-snap", "test-snap2"},
		ParentGroup:    "foo",
	}

	// verify it was set in state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it to the max size
	qc3 := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	expFoo2GroupState.ResourceLimits = quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it above the parent limit
	qc4 := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside group \"foo\" remaining quota space 1 GiB`)

	// and make sure that the existing memory limit is still in place
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateChangeMemLimit(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota for foo - an existing slice was changed, so all we need
		// to is daemon-reload
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
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// modify to 2 GB
	qc2 := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	}
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// trying to decrease the memory limit is not yet supported
	qc3 := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}
	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, "cannot update limits for group \"foo\": cannot decrease memory limit, remove and re-create it to decrease the limit")
}

func (s *quotaHandlersSuite) TestQuotaUpdateJournalQuotaNotAllowedForServices(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// CreateQuota for foo2
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group with the test snap
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	})
	c.Assert(err, IsNil)

	// create the sub-group which contain just the service
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ParentName:     "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		AddServices:    []string{"test-snap.svc1"},
	})
	c.Assert(err, IsNil)

	// try to impose the journal quota on the sub-group that contains services
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
	})
	c.Assert(err, ErrorMatches, `cannot update limits for group "foo2": journal quotas are not supported for individual services`)
}

func (s *quotaHandlersSuite) TestCreateJournalQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	// Add fake handlers for the setup-profiles task which should be invoked
	// when creating the journal quota.
	var setupProfilesCalled int
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		setupProfilesCalled++
		task.State().Lock()
		snapInfo, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		c.Assert(err, IsNil)
		c.Check(snapInfo.InstanceName(), Equals, "test-snap")
		c.Check(snapInfo.SideInfo.Revision, Equals, s.testSnapSideInfo.Revision)
		return err
	}
	s.o.TaskRunner().AddHandler("setup-profiles", fakeHandler, fakeHandler)

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB * 64).Build(),
		AddSnaps:       []string{"test-snap"},
	}
	qcs := []*servicestate.QuotaControlAction{&qc}

	chg := st.NewChange("quota-control-tasks", "...")
	t := st.NewTask("quota-control", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	err := s.o.Settle(5 * time.Second)
	st.Lock()
	c.Check(err, IsNil)
	c.Check(setupProfilesCalled, Equals, 1)
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB * 64).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestAddJournalQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	// Add fake handlers for the setup-profiles task which should be invoked
	// when creating the journal quota.
	var setupProfilesCalled int
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		setupProfilesCalled++
		task.State().Lock()
		snapInfo, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		c.Assert(err, IsNil)
		c.Check(snapInfo.InstanceName(), Equals, "test-snap")
		c.Check(snapInfo.SideInfo.Revision, Equals, s.testSnapSideInfo.Revision)
		return err
	}
	s.o.TaskRunner().AddHandler("setup-profiles", fakeHandler, fakeHandler)

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}
	qcs := []*servicestate.QuotaControlAction{&qc}

	chg := st.NewChange("quota-control-tasks", "...")
	t := st.NewTask("quota-control", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	err := s.o.Settle(5 * time.Second)
	st.Lock()
	c.Check(err, IsNil)
	c.Check(setupProfilesCalled, Equals, 0)
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	qc = servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB * 64).Build(),
	}
	qcs = []*servicestate.QuotaControlAction{&qc}

	chg = st.NewChange("quota-control-tasks", "...")
	t = st.NewTask("quota-control", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Check(err, IsNil)
	c.Check(setupProfilesCalled, Equals, 1)
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).WithJournalSize(quantity.SizeMiB * 64).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestUpdateJournalQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
		[]expectedSystemctl{
			{expArgs: []string{"stop", "systemd-journald@snap-foo"}},
			{
				expArgs: []string{"show", "--property=ActiveState", "systemd-journald@snap-foo"},
				output:  "ActiveState=inactive",
			},
			{expArgs: []string{"start", "systemd-journald@snap-foo"}},
		},
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	// Add fake handlers for the setup-profiles task which should be invoked
	// when creating the journal quota.
	var setupProfilesCalled int
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		setupProfilesCalled++
		task.State().Lock()
		snapInfo, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		c.Assert(err, IsNil)
		c.Check(snapInfo.InstanceName(), Equals, "test-snap")
		c.Check(snapInfo.SideInfo.Revision, Equals, s.testSnapSideInfo.Revision)
		return err
	}
	s.o.TaskRunner().AddHandler("setup-profiles", fakeHandler, fakeHandler)

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// setup an existing quota group we can update it
	err := servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil, quota.NewResourcesBuilder().WithJournalSize(16*quantity.SizeMiB).Build())
	c.Assert(err, check.IsNil)

	// Create the journald config file in /etc/systemd/journald@snap-foo.conf
	// this needs to be done to trigger the restart of the journald service for
	// that specific group. This is not done in the test as we only setup the
	// group as a mock, so manually do this here.
	fooConfPath := filepath.Join(dirs.SnapSystemdDir, "journald@snap-foo.conf")
	c.Assert(os.MkdirAll(filepath.Dir(fooConfPath), 0755), IsNil)
	err = os.WriteFile(fooConfPath, []byte(`[Journal]
SystemMaxUse=16M
`), 0644)
	c.Assert(err, IsNil)

	qc := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalRate(150, time.Millisecond*10).Build(),
	}
	qcs := []*servicestate.QuotaControlAction{&qc}

	chg := st.NewChange("quota-control-tasks", "...")
	t := st.NewTask("quota-control", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Check(err, IsNil)
	c.Check(setupProfilesCalled, Equals, 0)
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(16*quantity.SizeMiB).WithJournalRate(150, time.Millisecond*10).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestRemoveJournalQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// RemoveQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStop("foo"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	// Add fake handlers for the setup-profiles task which should be invoked
	// when creating the journal quota.
	var setupProfilesCalled int
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		setupProfilesCalled++
		task.State().Lock()
		snapInfo, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		c.Assert(err, IsNil)
		c.Check(snapInfo.InstanceName(), Equals, "test-snap")
		c.Check(snapInfo.SideInfo.Revision, Equals, s.testSnapSideInfo.Revision)
		return err
	}
	s.o.TaskRunner().AddHandler("setup-profiles", fakeHandler, fakeHandler)

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// setup an existing quota group we can remove
	err := servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil, quota.NewResourcesBuilder().WithJournalSize(16*quantity.SizeMiB).Build())
	c.Assert(err, check.IsNil)

	qc := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}
	qcs := []*servicestate.QuotaControlAction{&qc}

	chg := st.NewChange("quota-control-tasks", "...")
	t := st.NewTask("quota-control", "...")
	t.Set("quota-control-actions", &qcs)
	chg.AddTask(t)

	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Check(err, IsNil)
	c.Check(setupProfilesCalled, Equals, 1)
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnap(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota with just test-snap2 restarted since the group already
		// exists
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap2"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// add a snap
	qc2 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap2"},
	}
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap", "test-snap2"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnapAlreadyInOtherGroup(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo2", "test-snap2"),

		// UpdateQuota for foo which fails - no systemctl calls
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// create another quota group with the second snap
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap2"},
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap2"},
		},
	})

	// try to add test-snap2 to foo
	qc3 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap2"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `cannot add snap "test-snap2" to group "foo": snap already in quota group "foo2"`)

	// nothing changed in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap2"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaAddSnap(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       nil,
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		},
	})

	// The snap exists and the quota group exists, so we're able to test the
	// DoQuotaAddSnap
	task := s.state.NewTask("add-snap-to-quota", "test")

	// now set the snap-setup parameter and try again
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "test-snap",
			Revision: snap.R(1),
			SnapID:   "test-snap-id",
		},
	}
	task.Set("snap-setup", snapsup)

	st.Unlock()
	err = s.mgr.DoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err.Error(), Equals, "internal error: cannot get quota-name: no state entry for key \"quota-name\"")

	// and finally set the quota name as well, so it should succeed
	task.Set("quota-name", "foo")
	st.Unlock()
	err = s.mgr.DoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaAddSnapQuotaConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(st, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// setup an existing quota group we can update it
	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(1*quantity.SizeGiB).Build())
	c.Assert(err, check.IsNil)

	// Create a change that has a quota-control task in it for quota group foo
	chg := st.NewChange("quota-update", "update foo quota group")
	tsk := st.NewTask("quota-control", "update limits")
	tsk.Set("quota-control-actions", []servicestate.QuotaControlAction{
		{
			Action:    "update",
			QuotaName: "foo",
		},
	})
	chg.AddTask(tsk)
	chg.SetStatus(state.DoingStatus)

	// Now we create a task for QuotaAddSnap
	_, err = servicestate.AddSnapToQuotaGroup(st, "test-snap", "foo")
	c.Assert(err.Error(), Equals, "quota group \"foo\" has \"quota-update\" change in progress")
}

func (s *quotaHandlersSuite) TestDoQuotaAddSnapSnapConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(st, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// setup an existing quota group we can update it
	err := servicestatetest.MockQuotaInState(st, "foo2", "", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(1*quantity.SizeGiB).Build())
	c.Assert(err, check.IsNil)

	// Create the initial change which will contain AddSnapToQuotaGroup
	chg1 := st.NewChange("snap-install", "installing test-snap")
	tsk1, err := servicestate.AddSnapToQuotaGroup(st, "test-snap", "foo")
	c.Assert(err, IsNil)
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "test-snap",
			Revision: snap.R(1),
			SnapID:   "test-snap-id",
		},
	}
	tsk1.Set("snap-setup", snapsup)
	chg1.AddTask(tsk1)
	chg1.SetStatus(state.DoingStatus)

	// Create a change that has a quota-control task in it for quota group foo
	_, err = servicestate.UpdateQuota(st, "foo2", servicestate.UpdateQuotaOptions{
		AddSnaps: []string{"test-snap"},
	})
	c.Assert(err.Error(), Equals, "snap \"test-snap\" has \"snap-install\" change in progress")
}

func (s *quotaHandlersSuite) TestDoAddSnapToJournalQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
		AddSnaps:       nil,
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
		},
	})

	// The snap exists and the quota group exists, so we're able to test the
	// DoQuotaAddSnap
	chg := s.state.NewChange("add-snap-to-quota", "test")
	task := s.state.NewTask("add-snap-to-quota", "test")
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "test-snap",
			Revision: snap.R(1),
			SnapID:   "test-snap-id",
		},
	}
	task.Set("snap-setup", snapsup)
	task.Set("quota-name", "foo")
	chg.AddTask(task)
	st.Unlock()
	err = s.mgr.DoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(len(chg.Tasks()), Equals, 2)
	c.Check(chg.Tasks()[1].Kind(), Equals, "setup-profiles")

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestUndoQuotaAddSnap(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
		systemctlCallsForServiceRestart("test-snap"),

		// System calls expected for the removal of the snap
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// The snap exists and the quota group exists, so we're able to test the
	// DoQuotaAddSnap
	task := s.state.NewTask("undo-add-snap-to-quota", "test")

	// Test that it correctly reports an error if parameters are missing
	st.Unlock()
	err = s.mgr.UndoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err.Error(), Equals, "no state entry for key \"snap-setup-task\"")

	// Set correct parameters so it can run while we have the lock
	// now set the snap-setup parameter and try again
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "test-snap",
			Revision: snap.R(1),
			SnapID:   "test-snap-id",
		},
	}
	task.Set("snap-setup", snapsup)

	st.Unlock()
	err = s.mgr.UndoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		},
	})
}

func (s *quotaHandlersSuite) TestValidateSnapServicesForAddingToGroupCantMixGroup(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// build test quota groups
	allQuotas := map[string]*quota.Group{
		"foo": {
			Name:      "foo",
			SubGroups: []string{"foo2"},
		},
		"foo2": {
			Name:        "foo2",
			ParentGroup: "foo",
		},
	}

	err := servicestate.ValidateSnapServicesForAddingToGroup(st, []string{"test-snap.svc1"}, "foo", nil, allQuotas)
	c.Assert(err, ErrorMatches, `cannot mix services and sub groups in the group "foo"`)
}

func (s *quotaHandlersSuite) TestValidateSnapServicesForAddingToGroupServiceSnapNotInParent(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// build test quota groups
	allQuotas := map[string]*quota.Group{
		"foo": {
			Name:      "foo",
			SubGroups: []string{"foo2"},
		},
		"foo2": {
			Name:        "foo2",
			ParentGroup: "foo",
		},
	}

	err := servicestate.ValidateSnapServicesForAddingToGroup(st, []string{"test-snap.svc1"}, "foo2", allQuotas["foo"], allQuotas)
	c.Assert(err, ErrorMatches, `cannot add snap service "svc1": the snap "test-snap" must be in a direct parent group of group "foo2"`)
}

func (s *quotaHandlersSuite) TestValidateSnapServicesForAddingToGroupInvalidService(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// build test quota groups
	allQuotas := map[string]*quota.Group{
		"foo": {
			Name:      "foo",
			SubGroups: []string{"foo2"},
		},
		"foo2": {
			Name:        "foo2",
			ParentGroup: "foo",
		},
	}

	err := servicestate.ValidateSnapServicesForAddingToGroup(st, []string{"test-snap.svc2"}, "foo2", allQuotas["foo"], allQuotas)
	c.Assert(err, ErrorMatches, `cannot add snap service "foo2": invalid service "svc2"`)
}

func (s *quotaHandlersSuite) TestValidateSnapServicesForAddingToGroupHappy(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// build test quota groups
	allQuotas := map[string]*quota.Group{
		"foo": {
			Name:      "foo",
			SubGroups: []string{"foo2"},
			Snaps:     []string{"test-snap"},
		},
		"foo2": {
			Name:        "foo2",
			ParentGroup: "foo",
		},
	}

	err := servicestate.ValidateSnapServicesForAddingToGroup(st, []string{"test-snap.svc1"}, "foo2", allQuotas["foo"], allQuotas)
	c.Assert(err, IsNil)
}

func (s *quotaHandlersSuite) TestAffectedSnapServices(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// Create the root group with snaps in them
	servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap", "test-snap2"}, nil,
		quota.NewResourcesBuilder().WithJournalNamespace().Build())

	// Create a sub-group containing just service from test-snap
	servicestatetest.MockQuotaInState(st, "foo-svc", "foo", nil, []string{"test-snap.svc1"},
		quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())

	// Get all quotas currently in state
	allGrps, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(allGrps["foo"], NotNil)
	c.Assert(allGrps["foo-svc"], NotNil)

	// Now, we get a list of services affected if we were to do changes
	// to the sub-group containing just services
	opts := servicestate.EnsureSnapServicesForGroupOptions(allGrps, nil)
	svcOpts, affectedServices, err := servicestate.AffectedSnapServices(st, allGrps["foo-svc"], opts)
	c.Assert(err, IsNil)
	c.Check(affectedServices, DeepEquals, []string{"test-snap.svc1"})
	c.Check(len(svcOpts), Equals, 1)

	// However, if we get affected services from the group containing snaps,
	// we should expect to see all services
	svcOpts, affectedServices, err = servicestate.AffectedSnapServices(st, allGrps["foo"], opts)
	c.Assert(err, IsNil)
	c.Check(affectedServices, DeepEquals, []string{"test-snap.svc1", "test-snap2.svc1"})
	c.Check(len(svcOpts), Equals, 2)
}

func (s *quotaHandlersSuite) TestAffectedSnapServicesExtraSnaps(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// Create the root group with only test-snap in it
	servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil,
		quota.NewResourcesBuilder().WithJournalNamespace().Build())

	// Get all quotas currently in state
	allGrps, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(allGrps["foo"], NotNil)

	// Now, we get a list of services affected for foo, which should return only
	// test-snap.svc1, but add in test-snap2 using the ExtraSnaps property.
	opts := servicestate.EnsureSnapServicesForGroupOptions(allGrps, []string{"test-snap2"})
	svcOpts, affectedServices, err := servicestate.AffectedSnapServices(st, allGrps["foo"], opts)
	c.Assert(err, IsNil)
	c.Check(affectedServices, DeepEquals, []string{"test-snap.svc1", "test-snap2.svc1"})
	c.Check(len(svcOpts), Equals, 2)
}

const testYaml3 = `name: test-snap3
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
  svc2:
    command: bin.sh
    daemon: simple
`

// Do we have something like this anywhere?
func (s *quotaHandlersSuite) appendToFile(c *C, filePath string, text string) {
	f, err := os.OpenFile(filePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	defer f.Close()
	_, err = f.WriteString(text)
	c.Assert(err, IsNil)
}

func (s *quotaHandlersSuite) TestQuotaSnapServicesRestartOnlyRelevantServices(c *C) {
	// What makes services in a quota group restart? Moving snaps or services in or out
	// of quota groups. Here we test that when placing services into sub-groups only those
	// in the group are restarted.
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo"),
		systemctlCallsForServiceRestart("test-snap"),
		systemctlCallsForMultipleServiceRestart("test-snap3", []string{"svc1", "svc2"}),

		// CreateQuota for foo2 - we put test-snapd3.svc1 into this group
		// so we expect changes for that service only, and changes for the new quota group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap3"), // this only operates in svc1, which is what we need

		// UpdateQuota for foo2 - here we expect again to see just svc1 after our little hack
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap3"), // svc1
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// to prove we can affect only individual services with service groups, we will
	// need a couple of test snaps. We will include the default one for simplicity, and
	// we will setup another custom test snap that contains multiple services.
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snapInfo := snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap3
	si3 := &snap.SideInfo{RealName: "test-snap3", Revision: snap.R(42)}
	snapst3 := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  si3.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap3", snapst3)
	snap3Info := snaptest.MockSnapCurrent(c, testYaml3, si3)

	// Create the root quota group, and put let's fill it with our test snap.
	err := s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		AddSnaps:       []string{"test-snap", "test-snap3"},
	})
	c.Assert(err, IsNil)

	// Create our sub-group with a simple memory quota, and put one of the services
	// from test-snap3 into this.
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 4).Build(),
		ParentName:     "foo",
		AddServices:    []string{"test-snap3.svc1"},
	})
	c.Assert(err, IsNil)

	// To verify only the service we want actually changes (and see the restart we want)
	// we need to hack a bit. We need to modify the service files so the service layer actually
	// changes the files. Modifying the quota limits does not in itself trigger restarts, as any changes
	// that requires restart of services is not permitted as of writing this.
	// Instead, we will manually modify the service units, and add a comment. Then the service layer will
	// detect that it's making modifications.
	// Modify the service we expect will change only, and modify a service we expect not to change
	svc1AppInfo := snap3Info.Apps["svc1"]
	svc2AppInfo := snap3Info.Apps["svc2"]
	c.Assert(svc1AppInfo, NotNil)
	c.Assert(svc2AppInfo, NotNil)
	s.appendToFile(c, svc1AppInfo.ServiceFile(), "spaghetti\n")
	s.appendToFile(c, svc2AppInfo.ServiceFile(), "spaghetti\n")

	// Perform a change to the quota group, increase memory limit. If we ever decide to support
	// decreasing quota limits (where we need to restart the services), then we can do this instead
	// of the above hack.
	err = s.callDoQuotaControl(&servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
	})
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap", "test-snap3"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
			Services:       []string{"test-snap3.svc1"},
		},
	})

	// Verify service files agree on the correct slices
	allQuotas, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	expectedServiceUnits := []struct {
		snap       string
		service    string
		quotaGroup string
	}{
		{
			snap:       "test-snap3",
			service:    "svc1",
			quotaGroup: "foo2",
		},
		{
			snap:       "test-snap3",
			service:    "svc2",
			quotaGroup: "foo",
		},
	}
	for _, exp := range expectedServiceUnits {
		var info *snap.Info
		if exp.snap == "test-snap" {
			info = snapInfo
		} else if exp.snap == "test-snap3" {
			info = snap3Info
		}
		c.Assert(info, NotNil)
		svc := info.Apps[exp.service]
		grp := allQuotas[exp.quotaGroup]
		c.Assert(svc, NotNil)
		c.Assert(grp, NotNil)
		c.Check(svc.ServiceFile(), testutil.FileContains, fmt.Sprintf(`Slice=%s`, grp.SliceFileName()))
	}
}
