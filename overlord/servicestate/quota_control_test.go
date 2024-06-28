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
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	tomb "gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type quotaControlSuite struct {
	baseServiceMgrTestSuite
}

var _ = Suite(&quotaControlSuite{})

func (s *quotaControlSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)

	// we don't need the EnsureSnapServices ensure loop to run by default
	servicestate.MockEnsuredSnapServices(s.mgr, true)

	s.state.Lock()
	defer s.state.Unlock()

	// mock that we have a new enough version of systemd by default
	r := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(r)
	servicestate.CheckSystemdVersion()

	r = servicestate.MockResourcesCheckFeatureRequirements(func(res *quota.Resources) error {
		return nil
	})
	s.AddCleanup(r)

	// Add fake handlers for tasks handled by interfaces manager
	fakeHandler := func(task *state.Task, _ *tomb.Tomb) error {
		task.State().Lock()
		_, err := snapstate.TaskSnapSetup(task)
		task.State().Unlock()
		return err
	}
	s.o.TaskRunner().AddHandler("setup-profiles", fakeHandler, fakeHandler)
}

type quotaGroupState struct {
	ResourceLimits quota.Resources
	SubGroups      []string
	ParentGroup    string
	Snaps          []string
	Services       []string
}

func checkQuotaState(c *C, st *state.State, exp map[string]quotaGroupState) {
	m, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, len(exp))
	for name, grp := range m {
		expGrp, ok := exp[name]
		c.Assert(ok, Equals, true, Commentf("unexpected group %q in state", name))
		c.Assert(grp.ParentGroup, Equals, expGrp.ParentGroup)
		groupResources := grp.GetQuotaResources()
		c.Assert(groupResources, DeepEquals, expGrp.ResourceLimits)

		c.Assert(grp.Snaps, HasLen, len(expGrp.Snaps))
		if len(expGrp.Snaps) != 0 {
			c.Assert(grp.Snaps, DeepEquals, expGrp.Snaps)

			// also check on the service file states, but take into account
			// that the services might be in separate sub-groups here. If it has
			// sub-groups, assume those contain the services
			if len(expGrp.SubGroups) == 0 {
				for _, sn := range expGrp.Snaps {
					// meh assume all services are named svc1
					slicePath := name
					if grp.ParentGroup != "" {
						slicePath = grp.ParentGroup + "/" + name
					}
					checkSvcAndSliceState(c, sn+".svc1", slicePath, groupResources)
				}
			}
		}

		c.Assert(grp.Services, HasLen, len(expGrp.Services))
		if len(expGrp.Services) != 0 {
			c.Assert(grp.Services, DeepEquals, expGrp.Services)
			for _, svc := range expGrp.Services {
				slicePath := name
				parentName := expGrp.ParentGroup
				for parentName != "" {
					slicePath = parentName + "/" + slicePath
					parentName = exp[parentName].ParentGroup
				}
				checkSvcAndSliceState(c, svc, slicePath, groupResources)
			}
		}
		c.Assert(grp.SubGroups, HasLen, len(expGrp.SubGroups))
		if len(expGrp.SubGroups) != 0 {
			c.Assert(grp.SubGroups, DeepEquals, expGrp.SubGroups)
		}
	}
}

// shouldMentionSlice returns whether or not a slice file
// should be mentioned in the service unit file. It does in the case
// when a quota is set.
func shouldMentionSlice(resources quota.Resources) bool {
	if resources.Memory == nil && resources.CPU == nil &&
		resources.CPUSet == nil && resources.Threads == nil &&
		resources.Journal == nil {
		return false
	}
	return true
}

func checkSvcAndSliceState(c *C, snapSvc string, slicePath string, resources quota.Resources) {
	slicePath = systemd.EscapeUnitNamePath(slicePath)
	// make sure the service file exists
	svcFileName := filepath.Join(dirs.SnapServicesDir, "snap."+snapSvc+".service")
	c.Assert(svcFileName, testutil.FilePresent)

	if shouldMentionSlice(resources) {
		// the service file should mention this slice
		c.Assert(svcFileName, testutil.FileContains, fmt.Sprintf("\nSlice=snap.%s.slice\n", slicePath))
	} else {
		c.Assert(svcFileName, Not(testutil.FileContains), fmt.Sprintf("Slice=snap.%s.slice", slicePath))
	}
	checkSliceState(c, slicePath, resources)
}

func checkSliceState(c *C, sliceName string, resources quota.Resources) {
	sliceFileName := filepath.Join(dirs.SnapServicesDir, "snap."+sliceName+".slice")
	if !shouldMentionSlice(resources) {
		c.Assert(sliceFileName, Not(testutil.FilePresent))
		return
	}

	c.Assert(sliceFileName, testutil.FilePresent)
	if resources.Memory != nil {
		c.Assert(sliceFileName, testutil.FileContains, fmt.Sprintf("\nMemoryMax=%s\n", resources.Memory.Limit.String()))
	}
	if resources.CPU != nil {
		c.Assert(sliceFileName, testutil.FileContains, fmt.Sprintf("\nCPUQuota=%d%%\n", resources.CPU.Count*resources.CPU.Percentage))
	}
	if resources.CPUSet != nil {
		allowedCpusValue := strutil.IntsToCommaSeparated(resources.CPUSet.CPUs)
		c.Assert(sliceFileName, testutil.FileContains, fmt.Sprintf("\nAllowedCPUs=%s\n", allowedCpusValue))
	}
	if resources.Threads != nil {
		c.Assert(sliceFileName, testutil.FileContains, fmt.Sprintf("\nThreadsMax=%d\n", resources.Threads.Limit))
	}
}

func systemctlCallsForSliceStart(name string) []expectedSystemctl {
	name = systemd.EscapeUnitNamePath(name)
	slice := "snap." + name + ".slice"
	return []expectedSystemctl{
		{expArgs: []string{"start", slice}},
	}
}

func systemctlCallsForSliceStop(name string) []expectedSystemctl {
	name = systemd.EscapeUnitNamePath(name)
	slice := "snap." + name + ".slice"
	return []expectedSystemctl{
		{expArgs: []string{"stop", slice}},
		{
			expArgs: []string{"show", "--property=ActiveState", slice},
			output:  "ActiveState=inactive",
		},
	}
}

func systemctlCallsForServiceRestart(name string) []expectedSystemctl {
	svc := "snap." + name + ".svc1.service"
	return []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", svc},
			output:  fmt.Sprintf("Id=%s\nNames=%[1]s\nActiveState=active\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n", svc),
		},
		{expArgs: []string{"stop", svc}},
		{
			expArgs: []string{"show", "--property=ActiveState", svc},
			output:  "ActiveState=inactive",
		},
		{expArgs: []string{"start", svc}},
	}
}

func systemctlCallsForMultipleServiceRestart(name string, svcs []string) []expectedSystemctl {
	var svcNames []string
	var statusOutputs []string
	for _, svc := range svcs {
		svcName := "snap." + name + "." + svc + ".service"
		svcNames = append(svcNames, svcName)
		statusOutputs = append(statusOutputs, fmt.Sprintf("Id=%s\nNames=%[1]s\nActiveState=active\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n", svcName))
	}

	var expCalls []expectedSystemctl
	expCalls = append(expCalls, expectedSystemctl{
		expArgs: append([]string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload"}, svcNames...),
		output:  strings.Join(statusOutputs, "\n"),
	})
	for _, svc := range svcNames {
		expCalls = append(expCalls, []expectedSystemctl{
			{expArgs: []string{"stop", svc}},
			{
				expArgs: []string{"show", "--property=ActiveState", svc},
				output:  "ActiveState=inactive",
			},
			{expArgs: []string{"start", svc}},
		}...,
		)
	}
	return expCalls
}

func systemctlCallsForCreateQuota(groupName string, snapNames ...string) []expectedSystemctl {
	calls := join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStart(groupName),
	)
	for _, snapName := range snapNames {
		calls = join(calls, systemctlCallsForServiceRestart(snapName))
	}

	return calls
}

func join(calls ...[]expectedSystemctl) []expectedSystemctl {
	fullCall := []expectedSystemctl{}
	for _, call := range calls {
		fullCall = append(fullCall, call...)
	}

	return fullCall
}

func checkQuotaControlTasks(c *C, tasks []*state.Task, expAction *servicestate.QuotaControlAction) {
	c.Assert(tasks, HasLen, 1)
	t := tasks[0]

	c.Assert(t.Kind(), Equals, "quota-control")
	qcs := []*servicestate.QuotaControlAction{}

	err := t.Get("quota-control-actions", &qcs)
	c.Assert(err, IsNil)
	c.Assert(qcs, HasLen, 1)

	c.Assert(qcs[0], DeepEquals, expAction)
}

func (s *quotaControlSuite) TestCreateQuotaExperimentalNotEnabled(c *C) {
	// Test experimental quota group features that should not be enabled when
	// quota-groups feature is disabled
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	// Journal Quota is experimental, must give an error
	_, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		ResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
	})
	c.Assert(err, ErrorMatches, `journal quota options are experimental - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestCreateQuotaSystemdTooOld(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := systemd.MockSystemdVersion(229, nil)
	defer r()

	servicestate.CheckSystemdVersion()

	_, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, ErrorMatches, `cannot use quotas with incompatible systemd: systemd version 229 is too old \(expected at least 230\)`)
}

func (s *quotaControlSuite) TestCreateQuotaPerQuotaSystemdTooOld(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tests := []struct {
		resources      quota.Resources
		systemdVersion int
		expectedErr    string
	}{
		// We have no checks for these as we require a minimum systemd version of 230, and
		// the above unit test already verifies that minimum value. These are only listed
		// here for completeness.
		//{quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(), 211},
		//{quota.NewResourcesBuilder().WithCPUPercentage(25).Build(), 213},
		//{quota.NewResourcesBuilder().WithThreadLimit(64).Build(), 228},

		{quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build(), 243, `cannot use the cpu-set quota with incompatible systemd: systemd version 242 is too old \(expected at least 243\)`},
		{quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(), 245, `cannot use journal quota with incompatible systemd: systemd version 244 is too old \(expected at least 245\)`},
	}

	for _, t := range tests {
		r := systemd.MockSystemdVersion(t.systemdVersion-1, nil)
		defer r()

		_, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
			ResourceLimits: t.resources,
		})
		c.Assert(err, ErrorMatches, t.expectedErr)
	}
}

func (s *quotaControlSuite) TestCreateQuotaJournalNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	quotaConstraints := quota.NewResourcesBuilder().WithJournalNamespace().Build()
	_, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, ErrorMatches, `journal quota options are experimental - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestCreateQuotaJournalEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	quotaConstraints := quota.NewResourcesBuilder().WithJournalNamespace().Build()
	_, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)
}

func (s *quotaControlSuite) TestCreateQuotaPrecond(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB*2).Build())
	c.Assert(err, IsNil)

	tests := []struct {
		name     string
		mem      quantity.Size
		snaps    []string
		services []string
		err      string
	}{
		{"foo", quantity.SizeMiB, nil, nil, `group "foo" already exists`},
		{"new", 0, nil, nil, `cannot create quota group "new": memory quota must have a limit set`},
		{"new", quantity.SizeMiB, []string{"baz"}, nil, `cannot use snap "baz" in group "new": snap "baz" is not installed`},
		{"new", quantity.SizeMiB, []string{"baz"}, []string{"baz.foo"}, `cannot mix services and snaps in the same quota group`},
	}

	for _, t := range tests {
		_, err := servicestate.CreateQuota(st, t.name, servicestate.CreateQuotaOptions{
			Snaps:          t.snaps,
			Services:       t.services,
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(t.mem).Build(),
		})
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *quotaControlSuite) TestRemoveQuotaPreseeding(c *C) {
	r := snapdenv.MockPreseeding(true)
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	ts, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)

	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp := &servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		AddSnaps:       []string{"test-snap"},
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	}

	checkQuotaControlTasks(c, chg.Tasks(), exp)

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			Snaps:          []string{"test-snap"},
		},
	})

	// but removing a quota doesn't work, since it just doesn't make sense to be
	// able to remove a quota group while preseeding, so we purposely fail
	_, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, ErrorMatches, `removing quota groups not supported while preseeding`)
}

func (s *quotaControlSuite) TestCreateUnhappyCheckFeatureReqs(c *C) {
	r := servicestate.MockResourcesCheckFeatureRequirements(func(res *quota.Resources) error {
		return fmt.Errorf("check feature requirements error")
	})
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create the quota constraints to use for the test
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()

	// create the quota group
	_, err := servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Check(err, ErrorMatches, `cannot create quota group "foo": check feature requirements error`)
}

func (s *quotaControlSuite) TestUpdateUnhappyCheckFeatureReqs(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo - success
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create the quota constraints to use for the test
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()

	// create the quota group
	ts, err := servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)
	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)
	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// simulate that update does something that is not supported
	r = servicestate.MockResourcesCheckFeatureRequirements(func(res *quota.Resources) error {
		return fmt.Errorf("check feature requirements error")
	})
	defer r()

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{NewResourceLimits: quotaConstraints})
	c.Check(err, ErrorMatches, `cannot update group "foo": check feature requirements error`)
}

func (s *quotaControlSuite) TestCreateUpdateRemoveQuotaHappy(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo - success
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},

		// RemoveQuota for foo
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceStop("foo"),
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()
	var resCheckFeatureRequirementsCalled int
	r = servicestate.MockResourcesCheckFeatureRequirements(func(res *quota.Resources) error {
		resCheckFeatureRequirementsCalled++
		return nil
	})
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create the quota constraints to use for the test
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()

	// create the quota group
	ts, err := servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)
	c.Check(resCheckFeatureRequirementsCalled, Equals, 1)

	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp := &servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		AddSnaps:       []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	}

	checkQuotaControlTasks(c, chg.Tasks(), exp)

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quotaConstraints,
			Snaps:          []string{"test-snap"},
		},
	})

	// increase the memory limit
	newConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build()
	ts, err = servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{NewResourceLimits: newConstraints})
	c.Assert(err, IsNil)
	c.Check(resCheckFeatureRequirementsCalled, Equals, 2)

	chg = st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp2 := &servicestate.QuotaControlAction{
		Action:         "update",
		QuotaName:      "foo",
		ResourceLimits: newConstraints,
	}

	checkQuotaControlTasks(c, chg.Tasks(), exp2)

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: newConstraints,
			Snaps:          []string{"test-snap"},
		},
	})

	// remove the quota
	ts, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)
	// removal is not checked for feature requirements
	c.Check(resCheckFeatureRequirementsCalled, Equals, 2)

	chg = st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp3 := &servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}

	checkQuotaControlTasks(c, chg.Tasks(), exp3)

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	checkQuotaState(c, st, nil)
}

func (s *quotaControlSuite) TestEnsureSnapAbsentFromQuotaGroup(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap", "test-snap2"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo/foo2"),
		systemctlCallsForServiceRestart("test-snap"),

		// EnsureSnapAbsentFromQuota with just test-snap restarted since it is
		// no longer in the group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),

		// EnsureSnapAbsentFromQuota with just test-snap2 restarted since it is no
		// longer in the group
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
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	ts1, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap", "test-snap2"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)

	chg1 := st.NewChange("quota-control", "...")
	chg1.AddAll(ts1)
	checkQuotaControlTasks(c, chg1.Tasks(), &servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo",
		AddSnaps:       []string{"test-snap", "test-snap2"},
		ResourceLimits: quotaConstraints,
	})

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	// create a sub-group containing services to ensure these are removed too
	subConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build()
	ts2, err := servicestate.CreateQuota(s.state, "foo2", servicestate.CreateQuotaOptions{
		ParentName:     "foo",
		Services:       []string{"test-snap.svc1"},
		ResourceLimits: subConstraints,
	})
	c.Assert(err, IsNil)
	ts2.WaitAll(ts1)

	chg2 := st.NewChange("quota-control-sub", "...")
	chg2.AddAll(ts2)
	checkQuotaControlTasks(c, chg2.Tasks(), &servicestate.QuotaControlAction{
		Action:         "create",
		QuotaName:      "foo2",
		ParentName:     "foo",
		AddServices:    []string{"test-snap.svc1"},
		ResourceLimits: subConstraints,
	})

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quotaConstraints,
			Snaps:          []string{"test-snap", "test-snap2"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: subConstraints,
			Services:       []string{"test-snap.svc1"},
			ParentGroup:    "foo",
		},
	})

	// remove test-snap from the group
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap")
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quotaConstraints,
			Snaps:          []string{"test-snap2"},
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: subConstraints,
			ParentGroup:    "foo",
		},
	})

	// removing the same snap twice works as well but does nothing
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap")
	c.Assert(err, IsNil)

	// now remove test-snap2 too
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap2")
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			ResourceLimits: quotaConstraints,
			SubGroups:      []string{"foo2"},
		},
		"foo2": {
			ResourceLimits: subConstraints,
			ParentGroup:    "foo",
		},
	})

	// it's not an error to call EnsureSnapAbsentFromQuotaGroup on a snap that
	// is not in any quota group
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap33333")
	c.Assert(err, IsNil)
}

func (s *quotaControlSuite) TestUpdateQuotaGroupExperimentalNotEnabled(c *C) {
	// Test experimental quota group features that should not be enabled when
	// quota-groups feature is disabled
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	// Journal Quotas is experimental, must give an error
	opts := servicestate.UpdateQuotaOptions{
		NewResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
	}
	_, err := servicestate.UpdateQuota(s.state, "foo", opts)
	c.Assert(err, ErrorMatches, `journal quota options are experimental - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestUpdateQuotaPrecond(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build()
	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, nil, quotaConstraints)
	c.Assert(err, IsNil)

	newConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	tests := []struct {
		name string
		opts servicestate.UpdateQuotaOptions
		err  string
	}{
		{"what", servicestate.UpdateQuotaOptions{}, `group "what" does not exist`},
		{"foo", servicestate.UpdateQuotaOptions{NewResourceLimits: newConstraints}, `cannot update group "foo": cannot decrease memory limit, remove and re-create it to decrease the limit`},
		{"foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"baz"}}, `cannot use snap "baz" in group "foo": snap "baz" is not installed`},
		{"foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"baz"}, AddServices: []string{"baz.svc"}}, `cannot mix services and snaps in the same quota group`},
		{"foo", servicestate.UpdateQuotaOptions{AddServices: []string{"baz"}}, `invalid snap service: baz`},
		{"foo", servicestate.UpdateQuotaOptions{AddServices: []string{"baz.svc"}}, `cannot add snap service "foo": snap "baz" is not installed`},
	}

	for _, t := range tests {
		_, err := servicestate.UpdateQuota(st, t.name, t.opts)
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *quotaControlSuite) TestRemoveQuotaPrecond(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	quotaConstraints2GB := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, nil, quotaConstraints2GB)
	c.Assert(err, IsNil)

	quotaConstraints1GB := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	err = servicestatetest.MockQuotaInState(st, "bar", "foo", nil, nil, quotaConstraints1GB)
	c.Assert(err, IsNil)

	_, err = servicestate.RemoveQuota(st, "what")
	c.Check(err, ErrorMatches, `cannot remove non-existent quota group "what"`)

	_, err = servicestate.RemoveQuota(st, "foo")
	c.Check(err, ErrorMatches, `cannot remove quota group "foo" with sub-groups, remove the sub-groups first`)
}

func (s *quotaControlSuite) createQuota(c *C, name string, limits quota.Resources, snaps ...string) {
	ts, err := servicestate.CreateQuota(s.state, name, servicestate.CreateQuotaOptions{
		Snaps:          snaps,
		ResourceLimits: limits,
	})
	c.Assert(err, IsNil)

	chg := s.state.NewChange("quota-control", "...")
	chg.AddAll(ts)

	// run the change
	s.state.Unlock()
	err = s.o.Settle(5 * time.Second)
	s.state.Lock()
	c.Assert(err, IsNil)
}

func (s *quotaControlSuite) TestSnapOpUpdateQuotaConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
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
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := snapstate.Disable(st, "test-snap2")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("disable", "...")
	chg1.AddAll(ts)

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, ErrorMatches, `snap "test-snap2" has "disable" change in progress`)
}

func (s *quotaControlSuite) TestSnapOpCreateQuotaConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	ts, err := snapstate.Disable(st, "test-snap")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("disable", "...")
	chg1.AddAll(ts)

	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	_, err = servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, ErrorMatches, `snap "test-snap" has "disable" change in progress`)
}

func (s *quotaControlSuite) TestSnapOpRemoveQuotaConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := snapstate.Disable(st, "test-snap")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("disable", "...")
	chg1.AddAll(ts)

	_, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, ErrorMatches, `snap "test-snap" has "disable" change in progress`)
}

func (s *quotaControlSuite) TestCreateQuotaSnapOpConflict(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	ts, err := servicestate.CreateQuota(s.state, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = snapstate.Disable(st, "test-snap")
	c.Assert(err, ErrorMatches, `snap "test-snap" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestUpdateQuotaSnapOpConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
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
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = snapstate.Disable(st, "test-snap2")
	c.Assert(err, ErrorMatches, `snap "test-snap2" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestRemoveQuotaSnapOpConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = snapstate.Disable(st, "test-snap")
	c.Assert(err, ErrorMatches, `snap "test-snap" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestRemoveQuotaLateSnapOpConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)
	c.Assert(ts.Tasks(), HasLen, 1)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	// the group is already gone, but the task is not finished
	s.state.Set("quotas", nil)
	task := ts.Tasks()[0]
	task.Set("state-updated", servicestate.QuotaStateUpdated{
		BootID: "boot-id",
		AppsToRestartBySnap: map[string][]string{
			"test-snap": {"svc1"},
		},
	})

	_, err = snapstate.Disable(st, "test-snap")
	c.Assert(err, ErrorMatches, `snap "test-snap" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestUpdateQuotaUpdateQuotaConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
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
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	newConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build()
	_, err = servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{NewResourceLimits: newConstraints})
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestUpdateQuotaRemoveQuotaConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
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
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestRemoveQuotaUpdateQuotaConflict(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
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
	defer s.se.Stop()
	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	s.createQuota(c, "foo", quotaConstraints, "test-snap")

	ts, err := servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.UpdateQuotaOptions{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestCreateQuotaCreateQuotaConflict(c *C) {
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

	quotaConstraints := quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()
	ts, err := servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quotaConstraints,
	})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap2"},
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build(),
	})
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestAddSnapToQuotaGroup(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	t, err := servicestate.AddSnapToQuotaGroup(st, "test-snap", "foo")
	c.Assert(err, IsNil)
	c.Assert(t.Kind(), Equals, "quota-add-snap")
	c.Assert(t.Summary(), Equals, "Add snap \"test-snap\" to quota group \"foo\"")
}

func (s *quotaControlSuite) TestAddSnapToQuotaGroupQuotaConflict(c *C) {
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

	ts, err := servicestate.CreateQuota(st, "foo", servicestate.CreateQuotaOptions{
		Snaps:          []string{"test-snap"},
		ResourceLimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
	})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.AddSnapToQuotaGroup(st, "test-snap2", "foo")
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}

func (s *quotaControlSuite) TestAddSnapServicesToQuotaJournalGroupQuotaFail(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.journal-quota", true)
	tr.Commit()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
	servicestatetest.MockQuotaInState(st, "foo2", "foo", nil, nil, quota.NewResourcesBuilder().WithJournalNamespace().Build())

	_, err := servicestate.UpdateQuota(st, "foo2", servicestate.UpdateQuotaOptions{
		AddServices: []string{"test-snap.svc1"},
	})
	c.Assert(err, ErrorMatches, `cannot put services into group "foo2": journal quotas are not supported for individual services`)
}

func (s *quotaControlSuite) TestAddJournalQuotaToGroupWithServicesFail(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	servicestatetest.MockQuotaInState(st, "foo", "", []string{"test-snap"}, nil, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build())
	servicestatetest.MockQuotaInState(st, "foo2", "foo", nil, []string{"test-snap.svc1"}, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build())

	_, err := servicestate.UpdateQuota(st, "foo2", servicestate.UpdateQuotaOptions{
		NewResourceLimits: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
	})
	c.Assert(err, ErrorMatches, `cannot update group "foo2": journal quotas are not supported for individual services`)
}
