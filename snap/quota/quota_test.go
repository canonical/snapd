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
	"fmt"
	"math"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/systemd"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type quotaTestSuite struct{}

var _ = Suite(&quotaTestSuite{})

func (ts *quotaTestSuite) TestNewGroup(c *C) {

	tt := []struct {
		name          string
		sliceFileName string
		limits        quota.Resources
		err           string
		comment       string
	}{
		{
			name:    "group1",
			limits:  quota.NewResources(quantity.SizeMiB),
			comment: "basic happy",
		},
		{
			name:    "biglimit",
			limits:  quota.NewResources(quantity.Size(math.MaxUint64)),
			comment: "huge limit happy",
		},
		{
			name:    "zero",
			limits:  quota.NewResources(0),
			err:     `quota group must have a memory limit set`,
			comment: "group with zero memory limit",
		},
		{
			name:    "group1-unsupported chars",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "unsupported characters in group name",
		},
		{
			name:    "group%%%",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "more invalid characters in name",
		},
		{
			name:    "CAPITALIZED",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "capitalized letters",
		},
		{
			name:    "g1",
			limits:  quota.NewResources(quantity.SizeMiB),
			comment: "small group name",
		},
		{
			name:          "name-with-dashes",
			sliceFileName: `name\x2dwith\x2ddashes`,
			limits:        quota.NewResources(quantity.SizeMiB),
			comment:       "name with dashes",
		},
		{
			name:    "",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `invalid quota group name: must not be empty`,
			comment: "empty group name",
		},
		{
			name:    "g",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `invalid quota group name: must be between 2 and 40 characters long.*`,
			comment: "too small group name",
		},
		{
			name:    "root",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `group name "root" reserved`,
			comment: "reserved root name",
		},
		{
			name:    "snapd",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `group name "snapd" reserved`,
			comment: "reserved snapd name",
		},
		{
			name:    "system",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `group name "system" reserved`,
			comment: "reserved system name",
		},
		{
			name:    "user",
			limits:  quota.NewResources(quantity.SizeMiB),
			err:     `group name "user" reserved`,
			comment: "reserved user name",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		grp, err := quota.NewGroup(t.name, t.limits)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil, comment)

		if t.sliceFileName != "" {
			c.Assert(grp.SliceFileName(), Equals, "snap."+t.sliceFileName+".slice", comment)
		} else {
			c.Assert(grp.SliceFileName(), Equals, "snap."+t.name+".slice", comment)
		}
	}
}

func (ts *quotaTestSuite) TestSimpleSubGroupVerification(c *C) {
	tt := []struct {
		rootname      string
		rootlimits    quota.Resources
		subname       string
		sliceFileName string
		sublimits     quota.Resources
		err           string
		comment       string
	}{
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "sub",
			sublimits:  quota.NewResources(quantity.SizeMiB),
			comment:    "basic sub group with same quota as parent happy",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "sub",
			sublimits:  quota.NewResources(quantity.SizeMiB / 2),
			comment:    "basic sub group with smaller quota than parent happy",
		},
		{
			rootlimits:    quota.NewResources(quantity.SizeMiB),
			subname:       "sub-with-dashes",
			sliceFileName: `myroot-sub\x2dwith\x2ddashes`,
			sublimits:     quota.NewResources(quantity.SizeMiB / 2),
			comment:       "basic sub group with dashes in the name",
		},
		{
			rootname:      "my-root",
			rootlimits:    quota.NewResources(quantity.SizeMiB),
			subname:       "sub-with-dashes",
			sliceFileName: `my\x2droot-sub\x2dwith\x2ddashes`,
			sublimits:     quota.NewResources(quantity.SizeMiB / 2),
			comment:       "parent and sub group have dashes in name",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "sub",
			sublimits:  quota.NewResources(quantity.SizeMiB * 2),
			err:        "sub-group memory limit of 2 MiB is too large to fit inside remaining quota space 1 MiB for parent group myroot",
			comment:    "sub group with larger quota than parent unhappy",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "sub invalid chars",
			sublimits:  quota.NewResources(quantity.SizeMiB),
			err:        `invalid quota group name: contains invalid characters.*`,
			comment:    "sub group with invalid name",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "myroot",
			sublimits:  quota.NewResources(quantity.SizeMiB),
			err:        `cannot use same name "myroot" for sub group as parent group`,
			comment:    "sub group with same name as parent group",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "snapd",
			sublimits:  quota.NewResources(quantity.SizeMiB),
			err:        `group name "snapd" reserved`,
			comment:    "sub group with reserved name",
		},
		{
			rootlimits: quota.NewResources(quantity.SizeMiB),
			subname:    "zero",
			sublimits:  quota.NewResources(0),
			err:        `quota group must have a memory limit set`,
			comment:    "sub group with zero memory limit",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		// make a root group
		rootname := t.rootname
		if rootname == "" {
			rootname = "myroot"
		}
		rootGrp, err := quota.NewGroup(rootname, t.rootlimits)
		c.Assert(err, IsNil, comment)

		// make a sub-group under the root group
		subGrp, err := rootGrp.NewSubGroup(t.subname, t.sublimits)
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
			continue
		}
		c.Assert(err, IsNil, comment)

		if t.sliceFileName != "" {
			c.Assert(subGrp.SliceFileName(), Equals, "snap."+t.sliceFileName+".slice")
		} else {
			c.Assert(subGrp.SliceFileName(), Equals, "snap.myroot-"+t.subname+".slice")
		}
	}
}

func (ts *quotaTestSuite) TestComplexSubGroups(c *C) {
	rootGrp, err := quota.NewGroup("myroot", quota.NewResources(quantity.SizeMiB))
	c.Assert(err, IsNil)

	// try adding 2 sub-groups with total quota split exactly equally
	sub1, err := rootGrp.NewSubGroup("sub1", quota.NewResources(quantity.SizeMiB/2))
	c.Assert(err, IsNil)
	c.Assert(sub1.SliceFileName(), Equals, "snap.myroot-sub1.slice")

	sub2, err := rootGrp.NewSubGroup("sub2", quota.NewResources(quantity.SizeMiB/2))
	c.Assert(err, IsNil)
	c.Assert(sub2.SliceFileName(), Equals, "snap.myroot-sub2.slice")

	// adding another sub-group to this group fails
	_, err = rootGrp.NewSubGroup("sub3", quota.NewResources(5*quantity.SizeKiB))
	c.Assert(err, ErrorMatches, "sub-group memory limit of 5 KiB is too large to fit inside remaining quota space 0 B for parent group myroot")

	// we can however add a sub-group to one of the sub-groups with the exact
	// size of the parent sub-group
	subsub1, err := sub1.NewSubGroup("subsub1", quota.NewResources(quantity.SizeMiB/2))
	c.Assert(err, IsNil)
	c.Assert(subsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1.slice")

	// and we can even add a smaller sub-sub-sub-group to the sub-group
	subsubsub1, err := subsub1.NewSubGroup("subsubsub1", quota.NewResources(quantity.SizeMiB/4))
	c.Assert(err, IsNil)
	c.Assert(subsubsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1-subsubsub1.slice")
}

func (ts *quotaTestSuite) TestGroupUnmixableSnapsSubgroups(c *C) {
	parent, err := quota.NewGroup("parent", quota.NewResources(quantity.SizeMiB))
	c.Assert(err, IsNil)

	// now we add a snap to the parent group
	parent.Snaps = []string{"test-snap"}

	// add a subgroup to the parent group, this should fail as the group now has snaps
	_, err = parent.NewSubGroup("sub", quota.NewResources(quantity.SizeMiB/2))
	c.Assert(err, ErrorMatches, "cannot mix sub groups with snaps in the same group")
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
			err:     `group "foogroup" is invalid: quota group must have a memory limit set`,
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

func (ts *quotaTestSuite) TestAddAllNecessaryGroupsAvoidsInfiniteRecursion(c *C) {
	grp, err := quota.NewGroup("infinite-group", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	grp2, err := grp.NewSubGroup("infinite-group2", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	// create a cycle artificially to the same group
	grp2.SetInternalSubGroups([]*quota.Group{grp2})

	// now we fail to add this to a quota set
	qs := &quota.QuotaGroupSet{}
	err = qs.AddAllNecessaryGroups(grp)
	c.Assert(err, ErrorMatches, "internal error: circular reference found")

	// create a more difficult to detect cycle going from the child to the
	// parent
	grp2.SetInternalSubGroups([]*quota.Group{grp})
	err = qs.AddAllNecessaryGroups(grp)
	c.Assert(err, ErrorMatches, "internal error: circular reference found")

	// make a real sub-group and try one more level of indirection going back
	// to the parent
	grp2.SetInternalSubGroups(nil)
	grp3, err := grp2.NewSubGroup("infinite-group3", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)
	grp3.SetInternalSubGroups([]*quota.Group{grp})

	err = qs.AddAllNecessaryGroups(grp)
	c.Assert(err, ErrorMatches, "internal error: circular reference found")
}

func (ts *quotaTestSuite) TestAddAllNecessaryGroups(c *C) {
	qs := &quota.QuotaGroupSet{}

	// it should initially be empty
	c.Assert(qs.AllQuotaGroups(), HasLen, 0)

	grp1, err := quota.NewGroup("myroot", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	// add the group and make sure it is in the set
	err = qs.AddAllNecessaryGroups(grp1)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1})

	// adding multiple times doesn't change the set
	err = qs.AddAllNecessaryGroups(grp1)
	c.Assert(err, IsNil)
	err = qs.AddAllNecessaryGroups(grp1)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1})

	// add a new group and make sure it is in the set now
	grp2, err := quota.NewGroup("myroot2", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)
	err = qs.AddAllNecessaryGroups(grp2)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2})

	// start again
	qs = &quota.QuotaGroupSet{}

	// make a sub-group and add the root group - it will automatically add
	// the sub-group without us needing to explicitly add the sub-group
	subgrp1, err := grp1.NewSubGroup("mysub1", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)
	// add grp2 as well
	err = qs.AddAllNecessaryGroups(grp2)
	c.Assert(err, IsNil)

	err = qs.AddAllNecessaryGroups(grp1)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, subgrp1})

	// we can explicitly add the sub-group and still have the same set too
	err = qs.AddAllNecessaryGroups(subgrp1)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, subgrp1})

	// create a new set of group and sub-groups to add the deepest child group
	// and add that, and notice that the root groups are also added
	grp3, err := quota.NewGroup("myroot3", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp3, err := grp3.NewSubGroup("mysub3", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subsubgrp3, err := subgrp3.NewSubGroup("mysubsub3", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	err = qs.AddAllNecessaryGroups(subsubgrp3)
	c.Assert(err, IsNil)
	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, grp3, subgrp1, subgrp3, subsubgrp3})

	// finally create a tree with multiple branches and ensure that adding just
	// a single deepest child will add all the other deepest children from other
	// branches
	grp4, err := quota.NewGroup("myroot4", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp4, err := grp4.NewSubGroup("mysub4", quota.NewResources(quantity.SizeGiB/2))
	c.Assert(err, IsNil)

	subgrp5, err := grp4.NewSubGroup("mysub5", quota.NewResources(quantity.SizeGiB/2))
	c.Assert(err, IsNil)

	// adding just subgrp5 to a quota set will automatically add the other sub
	// group, subgrp4
	qs2 := &quota.QuotaGroupSet{}
	err = qs2.AddAllNecessaryGroups(subgrp4)
	c.Assert(err, IsNil)
	c.Assert(qs2.AllQuotaGroups(), DeepEquals, []*quota.Group{grp4, subgrp4, subgrp5})
}

func (ts *quotaTestSuite) TestResolveCrossReferencesLimitCheckSkipsSelf(c *C) {
	grp1, err := quota.NewGroup("myroot", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp1, err := grp1.NewSubGroup("mysub1", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp2, err := subgrp1.NewSubGroup("mysub2", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	all := map[string]*quota.Group{
		"myroot": grp1,
		"mysub1": subgrp1,
		"mysub2": subgrp2,
	}
	err = quota.ResolveCrossReferences(all)
	c.Assert(err, IsNil)
}

func (ts *quotaTestSuite) TestResolveCrossReferencesCircular(c *C) {
	grp1, err := quota.NewGroup("myroot", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp1, err := grp1.NewSubGroup("mysub1", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	subgrp2, err := subgrp1.NewSubGroup("mysub2", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	all := map[string]*quota.Group{
		"myroot": grp1,
		"mysub1": subgrp1,
		"mysub2": subgrp2,
	}
	// try to set up circular ref
	subgrp2.SubGroups = append(subgrp2.SubGroups, "mysub1")
	err = quota.ResolveCrossReferences(all)
	c.Assert(err, ErrorMatches, `.*reference necessary parent.*`)
}

type systemctlInactiveServiceError struct{}

func (s systemctlInactiveServiceError) Msg() []byte   { return []byte("inactive") }
func (s systemctlInactiveServiceError) ExitCode() int { return 0 }
func (s systemctlInactiveServiceError) Error() string { return "inactive" }

func (ts *quotaTestSuite) TestCurrentMemoryUsage(c *C) {
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		systemctlCalls++
		switch systemctlCalls {

		// inactive case, memory is 0
		case 1:
			// first time pretend the service is inactive
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("inactive"), systemctlInactiveServiceError{}

		// active but no tasks, but we still return the memory usage because it
		// can be valid on some systems to have non-zero memory usage for a
		// group without any tasks in it, such as on hirsute, arch, fedora 33+,
		// and debian sid
		case 2:
			// now pretend it is active
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("active"), nil
		case 3:
			// and the memory count can be non-zero like
			c.Assert(args, DeepEquals, []string{"show", "--property", "MemoryCurrent", "snap.group.slice"})
			return []byte("MemoryCurrent=4096"), nil

		case 4:
			// now pretend it is active
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("active"), nil
		case 5:
			// and the memory count can be zero too
			c.Assert(args, DeepEquals, []string{"show", "--property", "MemoryCurrent", "snap.group.slice"})
			return []byte("MemoryCurrent=0"), nil

		// bug case where 16 exb is erroneous - this is left in for posterity,
		// but we don't handle this differently, previously we had a workaround
		// for this sort of case, but it ended up not being tenable but still
		// test that a huge value just gets reported as-is
		case 6:
			// the cgroup is active, has no tasks and has 16 exb usage
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("active"), nil
		case 7:
			// since it is active, we will query the current memory usage,
			// this time return an obviously wrong number
			c.Assert(args, DeepEquals, []string{"show", "--property", "MemoryCurrent", "snap.group.slice"})
			return []byte("MemoryCurrent=18446744073709551615"), nil

		default:
			c.Errorf("too many systemctl calls (%d) (current call is %+v)", systemctlCalls, args)
			return []byte("broken test"), fmt.Errorf("broken test")
		}
	})
	defer r()

	grp1, err := quota.NewGroup("group", quota.NewResources(quantity.SizeGiB))
	c.Assert(err, IsNil)

	// group initially is inactive, so it has no current memory usage
	currentMem, err := grp1.CurrentMemoryUsage()
	c.Assert(err, IsNil)
	c.Assert(currentMem, Equals, quantity.Size(0))

	// now with the slice mocked as active it has real usage
	currentMem, err = grp1.CurrentMemoryUsage()
	c.Assert(err, IsNil)
	c.Assert(currentMem, Equals, 4*quantity.SizeKiB)

	// but it can also have 0 usage
	currentMem, err = grp1.CurrentMemoryUsage()
	c.Assert(err, IsNil)
	c.Assert(currentMem, Equals, quantity.Size(0))

	// and it can also be an incredibly huge value too
	currentMem, err = grp1.CurrentMemoryUsage()
	c.Assert(err, IsNil)
	const sixteenExb = quantity.Size(1<<64 - 1)
	c.Assert(currentMem, Equals, sixteenExb)
}
