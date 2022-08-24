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
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"
	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
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
	systemdRestore := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(systemdRestore)

	usabilityErrRestore := servicestate.EnsureQuotaUsability()
	s.AddCleanup(usabilityErrRestore)
}

// mockMixedQuotaGroup creates a new quota group mixed with the provided snaps and
// a single sub-group with the same name appended with 'sub'. The group is created with
// the memory limit of 1GB, and the subgroup has a limit of 512MB. We do this test as
// this type of mixed groups were supported when the feature was experimental.
func mockMixedQuotaGroup(st *state.State, name string, snaps []string) error {
	// create the quota group
	grp, err := quota.NewGroup(name, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
	if err != nil {
		return err
	}

	subGrpName := name + "-sub"
	subGrp, err := grp.NewSubGroup(subGrpName, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build())
	if err != nil {
		return err
	}

	grp.Snaps = snaps

	var quotas map[string]*quota.Group
	if err := st.Get("quotas", &quotas); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return err
		}
		quotas = make(map[string]*quota.Group)
	}
	quotas[name] = grp
	quotas[subGrpName] = subGrp
	st.Set("quotas", quotas)
	return nil
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

func (s *quotaHandlersSuite) TestQuotaSnapModifyExistingMixable(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := mockMixedQuotaGroup(st, "mixed-grp", []string{"test-snap"})
	c.Assert(err, IsNil)

	// try to update the memory limit for the mixed group
	qc := servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "mixed-grp",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	}
	err = s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `quota group "mixed-grp" has mixed snaps and sub-groups, which is no longer supported; removal and re-creation is necessary to modify it`)
}

func (s *quotaHandlersSuite) TestQuotaSnapCanRemoveMixed(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// handle the removal of the sub-group
		systemctlCallsForSliceStop("mixed-grp/mixed-grp-sub"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// handle removal of parent group with snap in
		systemctlCallsForSliceStop("mixed-grp"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	err := mockMixedQuotaGroup(st, "mixed-grp", []string{"test-snap"})
	c.Assert(err, IsNil)

	// first we remove the sub-group
	qc := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "mixed-grp-sub",
	}
	err = s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// then we remove the parent group
	qc2 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "mixed-grp",
	}
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToMixSubgroupWithSnaps(c *C) {
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

	// try to create a subgroup in a group that already has snaps, this call should fail
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, ErrorMatches, `cannot mix sub groups with snaps in the same group`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaSnapFailToMixSnapsWithSubgroups(c *C) {
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
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// create a subgroup for the foo group, which has neither snaps or subgroups
	qc2 := servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
		ParentName:     "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// now we try to add snaps to the foo group which already has subgroups, this should fail
	qc3 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `cannot mix snaps and sub groups in the group \"foo\"`)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			ParentGroup:    "foo",
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
		Sequence: []*snap.SideInfo{si2},
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
	err := servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, quota.NewResourcesBuilder().WithJournalSize(16*quantity.SizeMiB).Build())
	c.Assert(err, check.IsNil)

	// Create the journald config file in /etc/systemd/journald@snap-foo.conf
	// this needs to be done to trigger the restart of the journald service for
	// that specific group. This is not done in the test as we only setup the
	// group as a mock, so manually do this here.
	fooConfPath := filepath.Join(dirs.SnapSystemdDir, "journald@snap-foo.conf")
	c.Assert(os.MkdirAll(filepath.Dir(fooConfPath), 0755), IsNil)
	err = ioutil.WriteFile(fooConfPath, []byte(`[Journal]
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
	err := servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, quota.NewResourcesBuilder().WithJournalSize(16*quantity.SizeMiB).Build())
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
		Sequence: []*snap.SideInfo{si2},
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
		Sequence: []*snap.SideInfo{si2},
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

	st.Unlock()
	err = s.mgr.DoQuotaAddSnap(task, nil)
	st.Lock()
	c.Assert(err.Error(), Equals, "no state entry for key \"snap-setup-task\"")

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
	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, quota.NewResourcesBuilder().WithMemoryLimit(1*quantity.SizeGiB).Build())
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
	err := servicestatetest.MockQuotaInState(st, "foo2", "", nil, quota.NewResourcesBuilder().WithMemoryLimit(1*quantity.SizeGiB).Build())
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
	_, err = servicestate.UpdateQuota(st, "foo2", servicestate.QuotaGroupUpdate{
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
