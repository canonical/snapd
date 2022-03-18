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

package internal_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

func TestInternal(t *testing.T) { TestingT(t) }

type servicestateQuotasSuite struct {
	state *state.State
}

var _ = Suite(&servicestateQuotasSuite{})

func (s *servicestateQuotasSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
}

func (s *servicestateQuotasSuite) TestQuotas(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// with nothing in state we don't get any quotas
	quotaMap, err := internal.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, HasLen, 0)

	// we can add some basic quotas to state
	grp := &quota.Group{
		Name:        "foogroup",
		MemoryLimit: quantity.SizeGiB,
	}
	newGrps, err := internal.PatchQuotas(st, grp)
	c.Assert(err, IsNil)
	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// now we get back the same quota
	quotaMap, err = internal.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// adding a sub-group quota only works when we update the parent group to
	// reference the sub-group at the same time
	grp2 := &quota.Group{
		Name:        "group-2",
		MemoryLimit: quantity.SizeGiB,
		ParentGroup: "foogroup",
	}
	_, err = internal.PatchQuotas(st, grp2)
	c.Assert(err, ErrorMatches, `cannot update quota "group-2": group "foogroup" does not reference necessary child group "group-2"`)

	// we also can't add a sub-group to the parent without adding the sub-group
	// itself
	grp.SubGroups = append(grp.SubGroups, "group-2")
	_, err = internal.PatchQuotas(st, grp)
	c.Assert(err, ErrorMatches, `cannot update quota "foogroup": missing group "group-2" referenced as the sub-group of group "foogroup"`)

	// foogroup didn't get updated in the state to mention the sub-group
	quotaMap, err = internal.AllQuotas(st)
	c.Assert(err, IsNil)
	foogrp, ok := quotaMap["foogroup"]
	c.Assert(ok, Equals, true)
	c.Assert(foogrp.SubGroups, HasLen, 0)

	// but if we update them both at the same time we succeed
	newGrps, err = internal.PatchQuotas(st, grp, grp2)
	c.Assert(err, IsNil)
	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
		"group-2":  grp2,
	})

	// and now we see both in the state
	quotaMap, err = internal.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
		"group-2":  grp2,
	})

	// adding multiple quotas that are invalid produces a nice error message
	otherGrp := &quota.Group{
		Name: "other-group",
		// invalid memory limit
	}

	otherGrp2 := &quota.Group{
		Name: "other-group2",
		// invalid memory limit
	}

	_, err = internal.PatchQuotas(st, otherGrp2, otherGrp)
	// either group can get checked first
	c.Assert(err, ErrorMatches, `cannot update quotas "other-group", "other-group2": group "other-group2?" is invalid: quota group must have at least one resource limit set`)
}

func (s *servicestateQuotasSuite) TestCreateQuotaInState(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// we can create a basic quota in state
	grp := &quota.Group{
		Name:        "foogroup",
		MemoryLimit: quantity.SizeGiB,
	}
	grp1, newGrps, err := internal.CreateQuotaInState(st, "foogroup", nil, nil, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(), nil)
	c.Assert(err, IsNil)
	c.Check(grp1, DeepEquals, grp)
	c.Check(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// now quota is really in state
	quotaMap, err := internal.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// create a sub-group quota
	grp2 := &quota.Group{
		Name:        "group-2",
		MemoryLimit: quantity.SizeGiB,
		ParentGroup: "foogroup",
		Snaps:       []string{"snap1", "snap2"},
	}
	grp3, newGrps, err := internal.CreateQuotaInState(st, "group-2", grp1, []string{"snap1", "snap2"}, quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(), nil)
	c.Assert(err, IsNil)
	c.Check(grp3.Name, Equals, grp2.Name)
	c.Check(grp3.MemoryLimit, Equals, grp2.MemoryLimit)
	c.Check(grp3.ParentGroup, Equals, grp2.ParentGroup)
	c.Check(grp3.Snaps, DeepEquals, grp2.Snaps)
	c.Check(newGrps, HasLen, 2)
	c.Check(newGrps["foogroup"].SubGroups, DeepEquals, []string{"group-2"})

	// and now we see both in the state
	quotaMap, err = internal.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": newGrps["foogroup"],
		"group-2":  grp3,
	})
}
