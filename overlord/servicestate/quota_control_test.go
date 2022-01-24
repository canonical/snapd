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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
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

	// we enable quota-groups by default
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	// mock that we have a new enough version of systemd by default
	r := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(r)
	servicestate.CheckSystemdVersion()
}

type quotaGroupState struct {
	MemoryLimit quantity.Size
	SubGroups   []string
	ParentGroup string
	Snaps       []string
}

func checkQuotaState(c *C, st *state.State, exp map[string]quotaGroupState) {
	m, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, len(exp))
	for name, grp := range m {
		expGrp, ok := exp[name]
		c.Assert(ok, Equals, true, Commentf("unexpected group %q in state", name))
		c.Assert(grp.MemoryLimit, Equals, expGrp.MemoryLimit)
		c.Assert(grp.ParentGroup, Equals, expGrp.ParentGroup)

		c.Assert(grp.Snaps, HasLen, len(expGrp.Snaps))
		if len(expGrp.Snaps) != 0 {
			c.Assert(grp.Snaps, DeepEquals, expGrp.Snaps)

			// also check on the service file states
			for _, sn := range expGrp.Snaps {
				// meh assume all services are named svc1
				slicePath := name
				if grp.ParentGroup != "" {
					slicePath = grp.ParentGroup + "/" + name
				}
				checkSvcAndSliceState(c, sn+".svc1", slicePath, grp.MemoryLimit)
			}
		}

		c.Assert(grp.SubGroups, HasLen, len(expGrp.SubGroups))
		if len(expGrp.SubGroups) != 0 {
			c.Assert(grp.SubGroups, DeepEquals, expGrp.SubGroups)
		}
	}
}

func checkSvcAndSliceState(c *C, snapSvc string, slicePath string, sliceMem quantity.Size) {
	slicePath = systemd.EscapeUnitNamePath(slicePath)
	// make sure the service file exists
	svcFileName := filepath.Join(dirs.SnapServicesDir, "snap."+snapSvc+".service")
	c.Assert(svcFileName, testutil.FilePresent)

	if sliceMem != 0 {
		// the service file should mention this slice
		c.Assert(svcFileName, testutil.FileContains, fmt.Sprintf("\nSlice=snap.%s.slice\n", slicePath))
	} else {
		c.Assert(svcFileName, Not(testutil.FileContains), fmt.Sprintf("Slice=snap.%s.slice", slicePath))
	}
	checkSliceState(c, slicePath, sliceMem)
}

func checkSliceState(c *C, sliceName string, sliceMem quantity.Size) {
	sliceFileName := filepath.Join(dirs.SnapServicesDir, "snap."+sliceName+".slice")
	if sliceMem != 0 {
		c.Assert(sliceFileName, testutil.FilePresent)
		c.Assert(sliceFileName, testutil.FileContains, fmt.Sprintf("\nMemoryMax=%s\n", sliceMem.String()))
	} else {
		c.Assert(sliceFileName, testutil.FileAbsent)
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

func (s *quotaControlSuite) TestCreateQuotaNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	// try to create an empty quota group
	_, err := servicestate.CreateQuota(s.state, "foo", "", nil, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestCreateQuotaSystemdTooOld(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := systemd.MockSystemdVersion(229, nil)
	defer r()

	servicestate.CheckSystemdVersion()

	_, err := servicestate.CreateQuota(s.state, "foo", "", nil, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot use quotas with incompatible systemd: systemd version 229 is too old \(expected at least 230\)`)
}

func (s *quotaControlSuite) TestCreateQuotaPrecond(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, 2*quantity.SizeGiB)
	c.Assert(err, IsNil)

	tests := []struct {
		name  string
		mem   quantity.Size
		snaps []string
		err   string
	}{
		{"foo", 16 * quantity.SizeKiB, nil, `group "foo" already exists`},
		{"new", 0, nil, `cannot create quota group with no memory limit set`},
		{"new", quantity.SizeKiB, nil, `memory limit for group "new" is too small: size must be larger than 4KB`},
		{"new", 16 * quantity.SizeKiB, []string{"baz"}, `cannot use snap "baz" in group "new": snap "baz" is not installed`},
	}

	for _, t := range tests {
		_, err := servicestate.CreateQuota(st, t.name, "", t.snaps, t.mem)
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
	ts, err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp := &servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		AddSnaps:    []string{"test-snap"},
		MemoryLimit: quantity.SizeGiB,
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
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// but removing a quota doesn't work, since it just doesn't make sense to be
	// able to remove a quota group while preseeding, so we purposely fail
	_, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, ErrorMatches, `removing quota groups not supported while preseeding`)
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

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create the quota group
	ts, err := servicestate.CreateQuota(st, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp := &servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		AddSnaps:    []string{"test-snap"},
		MemoryLimit: quantity.SizeGiB,
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
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// increase the memory limit
	ts, err = servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{NewMemoryLimit: 2 * quantity.SizeGiB})
	c.Assert(err, IsNil)

	chg = st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp2 := &servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		MemoryLimit: 2 * quantity.SizeGiB,
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
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// remove the quota
	ts, err = servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)

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

		// EnsureSnapAbsentFromQuota with just test-snap restarted since it is
		// no longer in the group
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForServiceRestart("test-snap"),

		// another identical call to EnsureSnapAbsentFromQuota does nothing
		// since the function is idempotent

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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	ts, err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap", "test-snap2"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	chg := st.NewChange("quota-control", "...")
	chg.AddAll(ts)

	exp := &servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		AddSnaps:    []string{"test-snap", "test-snap2"},
		MemoryLimit: quantity.SizeGiB,
	}

	checkQuotaControlTasks(c, chg.Tasks(), exp)

	// run the change
	st.Unlock()
	defer s.se.Stop()
	err = s.o.Settle(5 * time.Second)
	st.Lock()
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap", "test-snap2"},
		},
	})

	// remove test-snap from the group
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap")
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap2"},
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
			MemoryLimit: quantity.SizeGiB,
		},
	})

	// it's not an error to call EnsureSnapAbsentFromQuotaGroup on a snap that
	// is not in any quota group
	err = servicestate.EnsureSnapAbsentFromQuota(s.state, "test-snap33333")
	c.Assert(err, IsNil)
}

func (s *quotaControlSuite) TestUpdateQuotaGroupNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	opts := servicestate.QuotaGroupUpdate{}
	_, err := servicestate.UpdateQuota(s.state, "foo", opts)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestUpdateQuotaPrecond(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, 2*quantity.SizeGiB)
	c.Assert(err, IsNil)

	tests := []struct {
		name string
		opts servicestate.QuotaGroupUpdate
		err  string
	}{
		{"what", servicestate.QuotaGroupUpdate{}, `group "what" does not exist`},
		{"foo", servicestate.QuotaGroupUpdate{NewMemoryLimit: quantity.SizeGiB}, `cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit`},
		{"foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"baz"}}, `cannot use snap "baz" in group "foo": snap "baz" is not installed`},
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

	err := servicestatetest.MockQuotaInState(st, "foo", "", nil, 2*quantity.SizeGiB)
	c.Assert(err, IsNil)
	err = servicestatetest.MockQuotaInState(st, "bar", "foo", nil, quantity.SizeGiB)
	c.Assert(err, IsNil)

	_, err = servicestate.RemoveQuota(st, "what")
	c.Check(err, ErrorMatches, `cannot remove non-existent quota group "what"`)

	_, err = servicestate.RemoveQuota(st, "foo")
	c.Check(err, ErrorMatches, `cannot remove quota group "foo" with sub-groups, remove the sub-groups first`)
}

func (s *quotaControlSuite) createQuota(c *C, name string, limit quantity.Size, snaps ...string) {
	ts, err := servicestate.CreateQuota(s.state, name, "", snaps, limit)
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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	defer s.se.Stop()
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

	ts, err := snapstate.Disable(st, "test-snap2")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("disable", "...")
	chg1.AddAll(ts)

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}})
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

	_, err = servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
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
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

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

	ts, err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	defer s.se.Stop()
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}})
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
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

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
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	defer s.se.Stop()
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}})
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{NewMemoryLimit: 2 * quantity.SizeGiB})
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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	defer s.se.Stop()
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

	ts, err := servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}})
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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	defer s.se.Stop()
	s.createQuota(c, "foo", quantity.SizeGiB, "test-snap")

	ts, err := servicestate.RemoveQuota(st, "foo")
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}})
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
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	ts, err := servicestate.CreateQuota(st, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)
	chg1 := s.state.NewChange("quota-control", "...")
	chg1.AddAll(ts)

	_, err = servicestate.CreateQuota(st, "foo", "", []string{"test-snap2"}, 2*quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `quota group "foo" has "quota-control" change in progress`)
}
