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
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/snap/quota"
)

type servicestateQuotasSuite struct {
	baseServiceMgrTestSuite
}

var _ = Suite(&servicestateQuotasSuite{})

func (s *servicestateQuotasSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)

	// we don't need the EnsureSnapServices ensure loop to run by default
	servicestate.MockEnsuredSnapServices(s.mgr, true)
}

func (s *servicestateQuotasSuite) TestQuotas(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// with nothing in state we don't get any quotas
	quotaMap, err := servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, HasLen, 0)

	// we can add some basic quotas to state
	grp := &quota.Group{
		Name:        "foogroup",
		MemoryLimit: quantity.SizeGiB,
	}
	err = servicestate.UpdateQuotas(st, grp)
	c.Assert(err, IsNil)

	// now we get back the same quota
	quotaMap, err = servicestate.AllQuotas(st)
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
	err = servicestate.UpdateQuotas(st, grp2)
	c.Assert(err, ErrorMatches, `cannot update quota "group-2": group "foogroup" does not reference necessary child group "group-2"`)

	// we also can't add a sub-group to the parent without adding the sub-group
	// itself
	grp.SubGroups = append(grp.SubGroups, "group-2")
	err = servicestate.UpdateQuotas(st, grp)
	c.Assert(err, ErrorMatches, `cannot update quota "foogroup": missing group "group-2" referenced as the sub-group of group "foogroup"`)

	// foogroup didn't get updated in the state to mention the sub-group
	foogrp, err := servicestate.GetQuota(st, "foogroup")
	c.Assert(err, IsNil)
	c.Assert(foogrp.SubGroups, HasLen, 0)

	// but if we update them both at the same time we succeed
	err = servicestate.UpdateQuotas(st, grp, grp2)
	c.Assert(err, IsNil)

	// and now we see both in the state
	quotaMap, err = servicestate.AllQuotas(st)
	c.Assert(err, IsNil)
	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
		"group-2":  grp2,
	})

	// and we can get individual quotas too
	res, err := servicestate.GetQuota(st, "foogroup")
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, grp)

	res2, err := servicestate.GetQuota(st, "group-2")
	c.Assert(err, IsNil)
	c.Assert(res2, DeepEquals, grp2)

	// adding multiple quotas that are invalid produces a nice error message
	otherGrp := &quota.Group{
		Name: "other-group",
		// invalid memory limit
	}

	otherGrp2 := &quota.Group{
		Name: "other-group2",
		// invalid memory limit
	}

	err = servicestate.UpdateQuotas(st, otherGrp2, otherGrp)
	c.Assert(err, ErrorMatches, `cannot update quotas "other-group", "other-group2": group "other-group" is invalid: group memory limit must be non-zero`)
}
