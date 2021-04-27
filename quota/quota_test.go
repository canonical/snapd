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

package quota_test

import (
	"math"
	"testing"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/quota"
	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type quotaTestSuite struct{}

var _ = Suite(&quotaTestSuite{})

func (ts *quotaTestSuite) TestNewGroup(c *C) {

	tt := []struct {
		name    string
		limit   quantity.Size
		err     string
		comment string
	}{
		{
			name:    "group1",
			limit:   quantity.SizeMiB,
			comment: "basic happy",
		},
		{
			name:    "biglimit",
			limit:   quantity.Size(math.MaxUint64),
			comment: "huge limit happy",
		},
		{
			name:    "zero",
			limit:   0,
			err:     `group memory limit must be non-zero`,
			comment: "group with zero memory limit",
		},
		{
			name:    "group1-unsupported chars",
			limit:   quantity.SizeMiB,
			err:     `group name "group1-unsupported chars" contains invalid characters.*`,
			comment: "unsupported characters in group name",
		},
		{
			name:    "1group",
			limit:   quantity.SizeMiB,
			err:     `group name "1group" contains invalid characters.*`,
			comment: "size negative",
		},
		{
			name:    "CAPITALIZED",
			limit:   quantity.SizeMiB,
			err:     `group name "CAPITALIZED" contains invalid characters.*`,
			comment: "capitalized letters",
		},
		{
			name:    "g1",
			limit:   quantity.SizeMiB,
			comment: "small group name",
		},
		{
			name:    "",
			limit:   quantity.SizeMiB,
			err:     `group name must not be empty`,
			comment: "empty group name",
		},
		{
			name:    "g",
			limit:   quantity.SizeMiB,
			err:     `group name "g" contains invalid character.*`,
			comment: "too small group name",
		},
		{
			name:    "root",
			limit:   quantity.SizeMiB,
			err:     `group name "root" reserved`,
			comment: "reserved root name",
		},
		{
			name:    "snapd",
			limit:   quantity.SizeMiB,
			err:     `group name "snapd" reserved`,
			comment: "reserved snapd name",
		},
		{
			name:    "system",
			limit:   quantity.SizeMiB,
			err:     `group name "system" reserved`,
			comment: "reserved system name",
		},
		{
			name:    "user",
			limit:   quantity.SizeMiB,
			err:     `group name "user" reserved`,
			comment: "reserved user name",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		grp, err := quota.NewGroup(t.name, t.limit)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil, comment)

		c.Assert(grp.SliceFileName(), Equals, "snap."+t.name+".slice", comment)
	}
}

func (ts *quotaTestSuite) TestSimpleSubGroupVerification(c *C) {
	tt := []struct {
		rootlimit quantity.Size
		subname   string
		sublimit  quantity.Size
		err       string
		comment   string
	}{
		{
			rootlimit: quantity.SizeMiB,
			subname:   "sub",
			sublimit:  quantity.SizeMiB,
			comment:   "basic sub group with same quota as parent happy",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "sub",
			sublimit:  quantity.SizeMiB / 2,
			comment:   "basic sub group with smaller quota than parent happy",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "sub",
			sublimit:  quantity.SizeMiB * 2,
			err:       "sub-group memory limit of 2 MiB is too large to fit inside remaining quota space 1 MiB for parent group myroot",
			comment:   "sub group with larger quota than parent unhappy",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "sub-invalid-chars",
			sublimit:  quantity.SizeMiB,
			err:       `group name "sub-invalid-chars" contains invalid characters.*`,
			comment:   "sub group with invalid name",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "myroot",
			sublimit:  quantity.SizeMiB,
			err:       `cannot use same name "myroot" for sub group as parent group`,
			comment:   "sub group with same name as parent group",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "snapd",
			sublimit:  quantity.SizeMiB,
			err:       `group name "snapd" reserved`,
			comment:   "sub group with reserved name",
		},
		{
			rootlimit: quantity.SizeMiB,
			subname:   "zero",
			sublimit:  0,
			err:       `group memory limit must be non-zero`,
			comment:   "sub group with zero memory limit",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		// make a root group
		rootGrp, err := quota.NewGroup("myroot", t.rootlimit)
		c.Assert(err, IsNil, comment)

		// make a sub-group under the root group
		subGrp, err := rootGrp.NewSubGroup(t.subname, t.sublimit)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil, comment)

		c.Assert(subGrp.SliceFileName(), Equals, "snap.myroot-"+t.subname+".slice")
	}
}

func (ts *quotaTestSuite) TestComplexSubGroups(c *C) {
	rootGrp, err := quota.NewGroup("myroot", quantity.SizeMiB)
	c.Assert(err, IsNil)

	// try adding 2 sub-groups with total quota split exactly equally
	sub1, err := rootGrp.NewSubGroup("sub1", quantity.SizeMiB/2)
	c.Assert(err, IsNil)
	c.Assert(sub1.SliceFileName(), Equals, "snap.myroot-sub1.slice")

	sub2, err := rootGrp.NewSubGroup("sub2", quantity.SizeMiB/2)
	c.Assert(err, IsNil)
	c.Assert(sub2.SliceFileName(), Equals, "snap.myroot-sub2.slice")

	// adding another sub-group to this group fails
	_, err = rootGrp.NewSubGroup("sub3", 1)
	c.Assert(err, ErrorMatches, "sub-group memory limit of 1 B is too large to fit inside remaining quota space 0 B for parent group myroot")

	// we can however add a sub-group to one of the sub-groups with the exact
	// size of the parent sub-group
	subsub1, err := sub1.NewSubGroup("subsub1", quantity.SizeMiB/2)
	c.Assert(err, IsNil)
	c.Assert(subsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1.slice")

	// and we can even add a smaller sub-sub-sub-group to the sub-group
	subsubsub1, err := subsub1.NewSubGroup("subsubsub1", quantity.SizeMiB/4)
	c.Assert(err, IsNil)
	c.Assert(subsubsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1-subsubsub1.slice")
}

func (ts *quotaTestSuite) TestResolveCrossReferences(c *C) {
	tt := []struct {
		grps    map[string]*quota.Group
		err     string
		comment string
	}{
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
				},
			},
			comment: "single group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
				},
			},
			err:     `group "foogroup" is invalid: group has circular parent reference to itself`,
			comment: "parent group self-reference group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"foogroup"},
				},
			},
			err:     `group "foogroup" is invalid: group has circular sub-group reference to itself`,
			comment: "parent group self-reference group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: 0,
				},
			},
			err:     `group "foogroup" is invalid: group memory limit must be non-zero`,
			comment: "invalid group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
				},
				"foogroup2": {
					Name:        "foogroup2",
					MemoryLimit: quantity.SizeMiB,
				},
			},
			comment: "multiple root groups",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
				},
				"subgroup": {
					Name:        "subgroup",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
				},
			},
			err:     `group "foogroup" does not reference necessary child group "subgroup"`,
			comment: "incomplete references in parent group to child group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"subgroup"},
				},
				"subgroup": {
					Name:        "subgroup",
					MemoryLimit: quantity.SizeMiB,
				},
			},
			err:     `group "subgroup" does not reference necessary parent group "foogroup"`,
			comment: "incomplete references in sub-group to parent group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"subgroup"},
				},
				"subgroup": {
					Name:        "subgroup",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
				},
			},
			comment: "valid fully specified sub-group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"subgroup1", "subgroup2"},
				},
				"subgroup1": {
					Name:        "subgroup1",
					MemoryLimit: quantity.SizeMiB / 2,
					ParentGroup: "foogroup",
				},
				"subgroup2": {
					Name:        "subgroup2",
					MemoryLimit: quantity.SizeMiB / 2,
					ParentGroup: "foogroup",
				},
			},
			comment: "multiple valid fully specified sub-groups",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"subgroup1"},
				},
				"subgroup1": {
					Name:        "subgroup1",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
					SubGroups:   []string{"subgroup2"},
				},
				"subgroup2": {
					Name:        "subgroup2",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "subgroup1",
				},
			},
			comment: "deeply nested valid fully specified sub-groups",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"subgroup1"},
				},
				"subgroup1": {
					Name:        "subgroup1",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
					SubGroups:   []string{"subgroup2"},
				},
				"subgroup2": {
					Name:        "subgroup2",
					MemoryLimit: quantity.SizeMiB,
					// missing parent reference
				},
			},
			err:     `group "subgroup2" does not reference necessary parent group "subgroup1"`,
			comment: "deeply nested invalid fully specified sub-groups",
		},
		{
			grps: map[string]*quota.Group{
				"not-foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
				},
			},
			err:     `group has name "foogroup", but is referenced as "not-foogroup"`,
			comment: "group misname",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					SubGroups:   []string{"other-missing"},
				},
			},
			err:     `missing group "other-missing" referenced as the sub-group of group "foogroup"`,
			comment: "missing sub-group name",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "other-missing",
				},
			},
			err:     `missing group "other-missing" referenced as the parent of group "foogroup"`,
			comment: "missing sub-group name",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		err := quota.ResolveCrossReferences(t.grps)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
	}
}

func (ts *quotaTestSuite) TestAddAllNecessaryGroups(c *C) {

}
