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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	_ "github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	_ "github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
}

func (s *quotaControlSuite) TestCreateQuotaNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	// try to create an empty quota group
	err := servicestate.CreateQuota(s.state, "foo", "", nil, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.quota-groups' to true`)
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
				sliceName := name
				if grp.ParentGroup != "" {
					sliceName = grp.ParentGroup + "-" + name
				}
				checkSvcAndSliceState(c, sn+".svc1", sliceName, grp.MemoryLimit)
			}
		}

		c.Assert(grp.SubGroups, HasLen, len(expGrp.SubGroups))
		if len(expGrp.SubGroups) != 0 {
			c.Assert(grp.SubGroups, DeepEquals, expGrp.SubGroups)
		}
	}
}

func checkSvcAndSliceState(c *C, snapSvc string, sliceName string, sliceMem quantity.Size) {
	// make sure the service file exists
	svcFileName := filepath.Join(dirs.SnapServicesDir, "snap."+snapSvc+".service")
	c.Assert(svcFileName, testutil.FilePresent)

	if sliceMem != 0 {
		// the service file should mention this slice
		c.Assert(svcFileName, testutil.FileContains, fmt.Sprintf("\nSlice=snap.%s.slice\n", sliceName))
	} else {
		c.Assert(svcFileName, Not(testutil.FileContains), fmt.Sprintf("Slice=snap.%s.slice", sliceName))
	}
	checkSliceState(c, sliceName, sliceMem)
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

func systemctlCallsForSliceRestart(name string) []expectedSystemctl {
	slice := "snap." + name + ".slice"
	return []expectedSystemctl{
		{expArgs: []string{"stop", slice}},
		{
			expArgs: []string{"show", "--property=ActiveState", slice},
			output:  "ActiveState=inactive",
		},
		{expArgs: []string{"start", slice}},
	}
}

func systemctlCallsForSliceStop(name string) []expectedSystemctl {
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
			expArgs: []string{"is-enabled", svc},
			output:  "enabled",
		},
		{expArgs: []string{"stop", svc}},
		{
			expArgs: []string{"show", "--property=ActiveState", svc},
			output:  "ActiveState=inactive",
		},
		{expArgs: []string{"start", svc}},
	}
}

func systemctlCallsForCreateQuota(groupName, snapName string) []expectedSystemctl {
	return join(
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceRestart(groupName),
		systemctlCallsForServiceRestart(snapName),
	)
}

func join(calls ...[]expectedSystemctl) []expectedSystemctl {
	fullCall := []expectedSystemctl{}
	for _, call := range calls {
		fullCall = append(fullCall, call...)
	}

	return fullCall
}

func (s *quotaControlSuite) TestCreateQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// trying to create a quota with a snap that doesn't exist fails
	err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot use snap "test-snap" in group "foo": snap "test-snap" is not installed`)

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// now we can create the quota group
	err = servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// we can't add the same snap to a different group though
	err = servicestate.CreateQuota(s.state, "foo2", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot add snap "test-snap" to group "foo2": snap already in quota group "foo"`)

	// creating the same group again will fail
	err = servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `group "foo" already exists`)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaControlSuite) TestCreateSubGroupQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo2 - we don't write anything for the first quota
		// since there are no snaps in the quota to track
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceRestart("foo"),
		systemctlCallsForSliceRestart("foo-foo2"),
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
	err := servicestate.CreateQuota(s.state, "foo", "", nil, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// trying to create a quota group with a non-existent parent group fails
	err = servicestate.CreateQuota(s.state, "foo2", "foo-non-real", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot create group under non-existent parent group "foo-non-real"`)

	// trying to create a quota group with too big of a limit to fit inside the
	// parent fails
	err = servicestate.CreateQuota(s.state, "foo2", "foo", []string{"test-snap"}, 2*quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside remaining quota space 1 GiB for parent group foo`)

	// now we can create a sub-quota
	err = servicestate.CreateQuota(s.state, "foo2", "foo", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			SubGroups:   []string{"foo2"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			ParentGroup: "foo",
		},
	})

	// foo exists but is not in any service
	checkSliceState(c, "foo", quantity.SizeGiB)
}

func (s *quotaControlSuite) TestRemoveQuota(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// RemoveQuota for foo2 - no daemon reload initially because
		// we didn't modify anything, as there are no snaps in foo2 so we don't
		// create that group on disk
		// TODO: is this bit correct in practice? we are in effect calling
		// systemctl stop <non-existing-slice> ?
		systemctlCallsForSliceStop("foo-foo3"),

		systemctlCallsForSliceStop("foo-foo2"),

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

	// trying to remove a group that does not exist fails
	err := servicestate.RemoveQuota(s.state, "not-exists")
	c.Assert(err, ErrorMatches, `cannot remove non-existent quota group "not-exists"`)

	// create a quota
	err = servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// create 2 quota sub-groups too
	err = servicestate.CreateQuota(s.state, "foo2", "foo", nil, quantity.SizeGiB/2)
	c.Assert(err, IsNil)

	err = servicestate.CreateQuota(s.state, "foo3", "foo", nil, quantity.SizeGiB/2)
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			SubGroups:   []string{"foo2", "foo3"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
		"foo3": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
	})

	// try removing the parent and it fails since it still has a sub-group
	// under it
	err = servicestate.RemoveQuota(s.state, "foo")
	c.Assert(err, ErrorMatches, "cannot remove quota group with sub-groups, remove the sub-groups first")

	// but we can remove the sub-group successfully first
	err = servicestate.RemoveQuota(s.state, "foo3")
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			SubGroups:   []string{"foo2"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
	})

	// and we can remove the other sub-group
	err = servicestate.RemoveQuota(s.state, "foo2")
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// now we can remove the quota from the state
	err = servicestate.RemoveQuota(s.state, "foo")
	c.Assert(err, IsNil)

	checkQuotaState(c, st, nil)

	// foo is not mentioned in the service and doesn't exist
	checkSvcAndSliceState(c, "test-snap.svc1", "foo", 0)
}

func (s *quotaControlSuite) TestUpdateQuotaGroupNotExist(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	opts := servicestate.QuotaGroupUpdate{}
	err := servicestate.UpdateQuota(s.state, "non-existing", opts)
	c.Check(err, ErrorMatches, `group "non-existing" does not exist`)
}

func (s *quotaControlSuite) TestUpdateQuotaSubGroupTooBig(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo-foo2", "test-snap2"),

		// UpdateQuota for foo2 - just the slice changes
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceRestart("foo-foo2"),
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
	err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	expFooGroupState := quotaGroupState{
		MemoryLimit: quantity.SizeGiB,
		Snaps:       []string{"test-snap"},
	}
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": expFooGroupState,
	})

	// create a sub-group with 0.5 GiB
	err = servicestate.CreateQuota(s.state, "foo2", "foo", []string{"test-snap2"}, quantity.SizeGiB/2)
	c.Assert(err, IsNil)

	expFooGroupState.SubGroups = []string{"foo2"}

	expFoo2GroupState := quotaGroupState{
		MemoryLimit: quantity.SizeGiB / 2,
		Snaps:       []string{"test-snap2"},
		ParentGroup: "foo",
	}

	// verify it was set in state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it to the max size
	err = servicestate.UpdateQuota(s.state, "foo2", servicestate.QuotaGroupUpdate{
		NewMemoryLimit: quantity.SizeGiB,
	})
	c.Assert(err, IsNil)

	expFoo2GroupState.MemoryLimit = quantity.SizeGiB
	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it above the parent limit
	err = servicestate.UpdateQuota(s.state, "foo2", servicestate.QuotaGroupUpdate{
		NewMemoryLimit: 2 * quantity.SizeGiB,
	})
	c.Assert(err, ErrorMatches, `cannot update quota "foo2": group "foo2" is invalid: sub-group memory limit of 2 GiB is too large to fit inside remaining quota space 1 GiB for parent group foo`)

	// and make sure that the existing memory limit is still in place
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})
}

func (s *quotaControlSuite) TestUpdateQuotaGroupNotEnabled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", false)
	tr.Commit()

	opts := servicestate.QuotaGroupUpdate{}
	err := servicestate.UpdateQuota(s.state, "foo", opts)
	c.Assert(err, ErrorMatches, `experimental feature disabled - test it by setting 'experimental.quota-groups' to true`)
}

func (s *quotaControlSuite) TestUpdateQuotaChangeMemLimit(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota for foo - just the slice changes
		[]expectedSystemctl{{expArgs: []string{"daemon-reload"}}},
		systemctlCallsForSliceRestart("foo"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// modify to 2 GB
	opts := servicestate.QuotaGroupUpdate{NewMemoryLimit: 2 * quantity.SizeGiB}
	err = servicestate.UpdateQuota(s.state, "foo", opts)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaControlSuite) TestUpdateQuotaAddSnap(c *C) {
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
	err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// add a snap
	opts := servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}}
	err = servicestate.UpdateQuota(s.state, "foo", opts)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap", "test-snap2"},
		},
	})
}

func (s *quotaControlSuite) TestUpdateQuotaAddSnapAlreadyInOtherGroup(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo2", "test-snap2"),
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
	err := servicestate.CreateQuota(s.state, "foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// create another quota group with the second snap
	err = servicestate.CreateQuota(s.state, "foo2", "", []string{"test-snap2"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap2"},
		},
	})

	// try to add test-snap2 to foo
	err = servicestate.UpdateQuota(st, "foo", servicestate.QuotaGroupUpdate{
		AddSnaps: []string{"test-snap2"},
	})
	c.Assert(err, ErrorMatches, `cannot add snap "test-snap2" to group "foo": snap already in quota group "foo2"`)

	// nothing changed in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap2"},
		},
	})
}
