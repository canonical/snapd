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

	_ "github.com/snapcore/snapd/overlord/devicestate"
	_ "github.com/snapcore/snapd/overlord/state"

	"github.com/snapcore/snapd/gadget/quantity"
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
}

func (s *quotaControlSuite) TestCreateQuota(c *C) {
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			// called for new slice unit written by CreateQuota after we create
			// the snap in state
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	// trying to create a quota with a snap that doesn't exist fails

	err := s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot use snap "test-snap" in group "foo": snap "test-snap" is not installed`)

	st := s.state
	st.Lock()
	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	st.Unlock()

	// now we can create the quota group
	err = s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// we can't add the same snap to a different group though
	err = s.mgr.CreateQuota("foo2", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `cannot use snap "test-snap" in group "foo2": snap already in quota group "foo"`)

	// creating the same group again will fail
	err = s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, ErrorMatches, `group "foo" already exists`)

	// check that the quota groups were created in the state
	st.Lock()
	defer st.Unlock()
	m, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, 1)
	for name, grp := range m {
		switch name {
		case "foo":
			c.Assert(grp.Snaps, DeepEquals, []string{"test-snap"})
			c.Assert(grp.SubGroups, HasLen, 0)
			c.Assert(grp.ParentGroup, Equals, "")
		default:
			c.Errorf("unexpected group %q in state", name)
		}
	}
}

func (s *quotaControlSuite) TestCreateSubGroupQuota(c *C) {
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			// called for new slice unit written by CreateQuota after we create
			// the snap in state
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	st := s.state
	st.Lock()
	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	st.Unlock()

	// create a quota group with no snaps to be the parent
	err := s.mgr.CreateQuota("foo", "", nil, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// now we can create a sub-quota
	err = s.mgr.CreateQuota("foo2", "foo", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	st.Lock()
	defer st.Unlock()
	m, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, 2)
	for name, grp := range m {
		switch name {
		case "foo":
			c.Assert(grp.Snaps, HasLen, 0)
			c.Assert(grp.SubGroups, DeepEquals, []string{"foo2"})
			c.Assert(grp.ParentGroup, Equals, "")
		case "foo2":
			c.Assert(grp.Snaps, DeepEquals, []string{"test-snap"})
			c.Assert(grp.SubGroups, HasLen, 0)
			c.Assert(grp.ParentGroup, Equals, "foo")
		default:
			c.Errorf("unexpected group %q in state", name)
		}
	}
}

func (s *quotaControlSuite) TestRemoveQuota(c *C) {
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			// called for new slice unit written by CreateQuota after we create
			// the snap in state
			expArgs: []string{"daemon-reload"},
		},
		{
			// called for the deleted slice unit from RemoveQuota
			expArgs: []string{"daemon-reload"},
		},
		{
			// called for the modified service unit files from EnsureSnapServices
			// TODO: this call should go away?
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	st := s.state
	st.Lock()
	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	st.Unlock()

	// trying to remove a group that does not exist fails
	err := s.mgr.RemoveQuota("not-exists")
	c.Assert(err, ErrorMatches, `cannot remove non-existent quota group "not-exists"`)

	// create a quota
	err = s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	st.Lock()
	defer st.Unlock()
	m, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, 1)
	for name, grp := range m {
		switch name {
		case "foo":
			c.Assert(grp.Snaps, DeepEquals, []string{"test-snap"})
			c.Assert(grp.SubGroups, HasLen, 0)
			c.Assert(grp.ParentGroup, Equals, "")
		default:
			c.Errorf("unexpected group %q in state", name)
		}
	}

	// remove the quota from the state
	st.Unlock()
	defer st.Lock()
	err = s.mgr.RemoveQuota("foo")
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	m, err = servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(m, HasLen, 0)
}

func (s *quotaControlSuite) TestUpdateQuotaGroupNotExist(c *C) {
	opts := servicestate.QuotaGroupUpdate{}
	err := s.mgr.UpdateQuota("non-existing", opts)
	c.Check(err, ErrorMatches, `group "non-existing" does not exist`)
}

func (s *quotaControlSuite) TestUpdateQuotaChangeMemLimit(c *C) {
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			// called for new slice unit written by CreateQuota after we create
			// the snap in state
			expArgs: []string{"daemon-reload"},
		},
		{
			// called by UpdateQuota
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	st := s.state
	st.Lock()
	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	st.Unlock()

	// create a quota group
	err := s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	st.Lock()
	m, err := servicestate.AllQuotas(st)
	st.Unlock()
	c.Check(err, IsNil)
	c.Check(m, HasLen, 1)
	c.Check(m["foo"].MemoryLimit, Equals, quantity.SizeGiB)

	// modify to 2 GB
	opts := servicestate.QuotaGroupUpdate{NewMemoryLimit: 2 * quantity.SizeGiB}
	err = s.mgr.UpdateQuota("foo", opts)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	st.Lock()
	m, err = servicestate.AllQuotas(st)
	st.Unlock()
	c.Check(err, IsNil)
	c.Check(m, HasLen, 1)
	c.Check(m["foo"].MemoryLimit, Equals, 2*quantity.SizeGiB)

	// XXX: should we look at the written snap services/slices here too?
}

func (s *quotaControlSuite) TestUpdateQuotaAddSnap(c *C) {
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			// called for new slice unit written by CreateQuota after we create
			// the snap in state
			expArgs: []string{"daemon-reload"},
		},
		{
			// called by UpdateQuota
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	st := s.state
	st.Lock()
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
	st.Unlock()

	// create a quota group
	err := s.mgr.CreateQuota("foo", "", []string{"test-snap"}, quantity.SizeGiB)
	c.Assert(err, IsNil)

	st.Lock()
	m, err := servicestate.AllQuotas(st)
	st.Unlock()
	c.Check(err, IsNil)
	c.Check(m, HasLen, 1)
	c.Check(m["foo"].Snaps, DeepEquals, []string{"test-snap"})

	// add a snap
	opts := servicestate.QuotaGroupUpdate{AddSnaps: []string{"test-snap2"}}
	err = s.mgr.UpdateQuota("foo", opts)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	st.Lock()
	m, err = servicestate.AllQuotas(st)
	st.Unlock()
	c.Check(err, IsNil)
	c.Check(m, HasLen, 1)
	c.Check(m["foo"].Snaps, DeepEquals, []string{"test-snap", "test-snap2"})

	// XXX: should we look at the written snap services/slices here too?
}
