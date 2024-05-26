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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
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
	quotaMap := mylog.Check2(servicestate.AllQuotas(st))

	c.Assert(quotaMap, HasLen, 0)

	// we can add some basic quotas to state
	grp := &quota.Group{
		Name:        "foogroup",
		MemoryLimit: quantity.SizeGiB,
	}
	newGrps := mylog.Check2(servicestatetest.PatchQuotas(st, grp))

	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
	})

	// now we get back the same quota
	quotaMap = mylog.Check2(servicestate.AllQuotas(st))

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
	_ = mylog.Check2(servicestatetest.PatchQuotas(st, grp2))
	c.Assert(err, ErrorMatches, `cannot update quota "group-2": group "foogroup" does not reference necessary child group "group-2"`)

	// we also can't add a sub-group to the parent without adding the sub-group
	// itself
	grp.SubGroups = append(grp.SubGroups, "group-2")
	_ = mylog.Check2(servicestatetest.PatchQuotas(st, grp))
	c.Assert(err, ErrorMatches, `cannot update quota "foogroup": missing group "group-2" referenced as the sub-group of group "foogroup"`)

	// but if we update them both at the same time we succeed
	newGrps = mylog.Check2(servicestatetest.PatchQuotas(st, grp, grp2))

	c.Assert(newGrps, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
		"group-2":  grp2,
	})

	// and now we see both in the state
	quotaMap = mylog.Check2(servicestate.AllQuotas(st))

	c.Assert(quotaMap, DeepEquals, map[string]*quota.Group{
		"foogroup": grp,
		"group-2":  grp2,
	})

	// and we can get individual quotas too
	res := mylog.Check2(servicestate.GetQuota(st, "foogroup"))

	c.Assert(res, DeepEquals, grp)

	res2 := mylog.Check2(servicestate.GetQuota(st, "group-2"))

	c.Assert(res2, DeepEquals, grp2)

	_ = mylog.Check2(servicestate.GetQuota(st, "unknown"))
	c.Assert(err, Equals, servicestate.ErrQuotaNotFound)
}
