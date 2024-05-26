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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			comment: "basic happy",
		},
		{
			name:    "biglimit",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.Size(math.MaxUint64)).Build(),
			comment: "huge limit happy",
		},
		{
			name:    "zero",
			limits:  quota.NewResourcesBuilder().Build(),
			err:     `quota group must have at least one resource limit set`,
			comment: "group with no limits",
		},
		{
			name:    "group1-unsupported chars",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "unsupported characters in group name",
		},
		{
			name:    "group%%%",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "more invalid characters in name",
		},
		{
			name:    "CAPITALIZED",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `invalid quota group name: contains invalid characters.*`,
			comment: "capitalized letters",
		},
		{
			name:    "g1",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			comment: "small group name",
		},
		{
			name:          "name-with-dashes",
			sliceFileName: `name\x2dwith\x2ddashes`,
			limits:        quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			comment:       "name with dashes",
		},
		{
			name:    "",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `invalid quota group name: must not be empty`,
			comment: "empty group name",
		},
		{
			name:    "g",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `invalid quota group name: must be between 2 and 40 characters long.*`,
			comment: "too small group name",
		},
		{
			name:    "root",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `group name "root" reserved`,
			comment: "reserved root name",
		},
		{
			name:    "snapd",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `group name "snapd" reserved`,
			comment: "reserved snapd name",
		},
		{
			name:    "system",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `group name "system" reserved`,
			comment: "reserved system name",
		},
		{
			name:    "user",
			limits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			err:     `group name "user" reserved`,
			comment: "reserved user name",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		grp := mylog.Check2(quota.NewGroup(t.name, t.limits))
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
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			comment:    "basic sub group with same quota as parent happy",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(2 * quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(50).WithCPUSet([]int{0}).WithThreadLimit(16).Build(),
			comment:    "basic sub group with smaller quota than parent happy",
		},
		{
			rootlimits:    quota.NewResourcesBuilder().WithMemoryLimit(2 * quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:       "sub-with-dashes",
			sliceFileName: `myroot-sub\x2dwith\x2ddashes`,
			sublimits:     quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(50).WithCPUSet([]int{0}).WithThreadLimit(16).Build(),
			comment:       "basic sub group with dashes in the name",
		},
		{
			rootname:      "my-root",
			rootlimits:    quota.NewResourcesBuilder().WithMemoryLimit(2 * quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:       "sub-with-dashes",
			sliceFileName: `my\x2droot-sub\x2dwith\x2ddashes`,
			sublimits:     quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(50).WithCPUSet([]int{0}).WithThreadLimit(16).Build(),
			comment:       "parent and sub group have dashes in name",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB * 2).Build(),
			err:        "sub-group memory limit of 2 MiB is too large to fit inside group \"myroot\" remaining quota space 1 MiB",
			comment:    "sub group with larger memory quota than parent unhappy",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(100).Build(),
			err:        "sub-group cpu limit of 200% is too large to fit inside group \"myroot\" remaining quota space 100%",
			comment:    "sub group with larger cpu count quota than parent unhappy",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithCPUSet([]int{1}).Build(),
			err:        "sub-group cpu-set \\[1\\] is not a subset of group \"myroot\" cpu-set \\[0\\]",
			comment:    "sub group with different cpu allowance quota than parent unhappy",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub",
			sublimits:  quota.NewResourcesBuilder().WithThreadLimit(64).Build(),
			err:        "sub-group thread limit of 64 is too large to fit inside group \"myroot\" remaining quota space 32",
			comment:    "sub group with larger task allowance quota than parent unhappy",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "sub invalid chars",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			err:        `invalid quota group name: contains invalid characters.*`,
			comment:    "sub group with invalid name",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "myroot",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			err:        `cannot use same name "myroot" for sub group as parent group`,
			comment:    "sub group with same name as parent group",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "snapd",
			sublimits:  quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			err:        `group name "snapd" reserved`,
			comment:    "sub group with reserved name",
		},
		{
			rootlimits: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(1).WithCPUPercentage(100).WithCPUSet([]int{0}).WithThreadLimit(32).Build(),
			subname:    "zero",
			sublimits:  quota.NewResourcesBuilder().Build(),
			err:        `quota group must have at least one resource limit set`,
			comment:    "sub group with no limits",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		// make a root group
		rootname := t.rootname
		if rootname == "" {
			rootname = "myroot"
		}
		rootGrp := mylog.Check2(quota.NewGroup(rootname, t.rootlimits))
		c.Assert(err, IsNil, comment)

		// make a sub-group under the root group
		subGrp := mylog.Check2(rootGrp.NewSubGroup(t.subname, t.sublimits))
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
	rootGrp := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(2*quantity.SizeMiB).Build()))


	// try adding 2 sub-groups with total quota split exactly equally
	sub1 := mylog.Check2(rootGrp.NewSubGroup("sub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Assert(sub1.SliceFileName(), Equals, "snap.myroot-sub1.slice")

	sub2 := mylog.Check2(rootGrp.NewSubGroup("sub2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Assert(sub2.SliceFileName(), Equals, "snap.myroot-sub2.slice")

	// adding another sub-group to this group fails
	_ = mylog.Check2(rootGrp.NewSubGroup("sub3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))
	c.Assert(err, ErrorMatches, "sub-group memory limit of 1 MiB is too large to fit inside group \"myroot\" remaining quota space 0 B")

	// we can however add a sub-group to one of the sub-groups with the exact
	// size of the parent sub-group
	subsub1 := mylog.Check2(sub1.NewSubGroup("subsub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Assert(subsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1.slice")

	// and we can even add a sub-sub-sub-group to the sub-group
	subsubsub1 := mylog.Check2(subsub1.NewSubGroup("subsubsub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Assert(subsubsub1.SliceFileName(), Equals, "snap.myroot-sub1-subsub1-subsubsub1.slice")
}

func (ts *quotaTestSuite) TestGroupIsMixableSnapsSubgroups(c *C) {
	parent := mylog.Check2(quota.NewGroup("parent", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))


	// now we add a snap to the parent group
	parent.Snaps = []string{"test-snap"}

	// add a subgroup to the parent group
	_ = mylog.Check2(parent.NewSubGroup("sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

}

func (ts *quotaTestSuite) TestGroupUnmixableServicesSubgroups(c *C) {
	parent := mylog.Check2(quota.NewGroup("parent", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))


	// now we add a snap to the parent group
	parent.Services = []string{"my-service"}

	// add a subgroup to the parent group
	_ = mylog.Check2(parent.NewSubGroup("sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))
	c.Assert(err, ErrorMatches, "cannot mix sub groups with services in the same group")
}

func (ts *quotaTestSuite) TestJournalNamespaceName(c *C) {
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalNamespaceName(), Equals, "snap-foo")
}

func (ts *quotaTestSuite) TestJournalQuotaSet(c *C) {
	// If no services are in the sub-group, then it's not a service group.
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithJournalNamespace().Build()))

	sub := mylog.Check2(grp.NewSubGroup("bar", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalQuotaSet(), Equals, true)
	c.Check(sub.JournalQuotaSet(), Equals, false)
}

func (ts *quotaTestSuite) TestJournalQuotaSetReflectsParent(c *C) {
	// If services are in the sub-group, then it's a service group.
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithJournalNamespace().Build()))

	sub := mylog.Check2(grp.NewSubGroup("bar", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	sub.Services = []string{"snap.svc"}
	c.Check(grp.JournalQuotaSet(), Equals, true)

	// now that sub is a service group, we should see that this reflects
	// the parent group
	c.Check(sub.JournalQuotaSet(), Equals, true)
}

func (ts *quotaTestSuite) TestJournalNamespaceNameSubgroupNotInherit(c *C) {
	// If no services are in the sub-group, then it's not a service group.
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	sub := mylog.Check2(grp.NewSubGroup("bar", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalNamespaceName(), Equals, "snap-foo")
	c.Check(sub.JournalNamespaceName(), Equals, "snap-bar")
}

func (ts *quotaTestSuite) TestJournalNamespaceNameSubgroupInherit(c *C) {
	// If services are in the sub-group, then it's a service group.
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	sub := mylog.Check2(grp.NewSubGroup("bar", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	sub.Services = []string{"snap.svc"}
	c.Check(grp.JournalNamespaceName(), Equals, "snap-foo")
	// now the journal namespace is set to the parent
	c.Check(sub.JournalNamespaceName(), Equals, "snap-foo")
}

func (ts *quotaTestSuite) TestJournalConfFileName(c *C) {
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalConfFileName(), Equals, "journald@snap-foo.conf")
}

func (ts *quotaTestSuite) TestJournalServiceName(c *C) {
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalServiceName(), Equals, "systemd-journald@snap-foo.service")
}

func (ts *quotaTestSuite) TestJournalServiceDropInDir(c *C) {
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalServiceDropInDir(), Equals, "/etc/systemd/system/systemd-journald@snap-foo.service.d")
}

func (ts *quotaTestSuite) TestJournalServiceDropInFile(c *C) {
	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))

	c.Check(grp.JournalServiceDropInFile(), Equals, "/etc/systemd/system/systemd-journald@snap-foo.service.d/00-snap.conf")
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
			err:     `group "foogroup" is invalid: quota group must have at least one resource limit set`,
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
					MemoryLimit: 2 * quantity.SizeMiB,
					SubGroups:   []string{"subgroup1", "subgroup2"},
				},
				"subgroup1": {
					Name:        "subgroup1",
					MemoryLimit: quantity.SizeMiB,
					ParentGroup: "foogroup",
				},
				"subgroup2": {
					Name:        "subgroup2",
					MemoryLimit: quantity.SizeMiB,
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
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:         "foogroup",
					JournalLimit: &quota.GroupQuotaJournal{},
					Services:     []string{"snap.svc"},
				},
			},
			err:     `group "foogroup" is invalid: journal quota is not supported for individual services`,
			comment: "setting a journal quota for a group with services is not allowed",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		mylog.Check(quota.ResolveCrossReferences(t.grps))
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
	}
}

func (ts *quotaTestSuite) TestVerifyNestingAndMixingIsAllowed(c *C) {
	tt := []struct {
		grps    map[string]*quota.Group
		check   string
		err     string
		comment string
	}{
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
				},
			},
			check:   "foogroup",
			comment: "single group with a snap",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
					SubGroups:   []string{"foo-sub"},
				},
				"foo-sub": {
					Name:        "foo-sub",
					MemoryLimit: quantity.SizeMiB * 8,
					ParentGroup: "foogroup",
				},
			},
			check:   "foogroup",
			comment: "mixed group, with a single sub-group",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
					SubGroups:   []string{"foo-sub"},
				},
				"foo-sub": {
					Name:        "foo-sub",
					MemoryLimit: quantity.SizeMiB * 8,
					ParentGroup: "foogroup",
					SubGroups:   []string{"foo-sub-sub"},
				},
				"foo-sub-sub": {
					Name:        "foo-sub-sub",
					MemoryLimit: quantity.SizeMiB * 4,
					ParentGroup: "foo-sub",
				},
			},
			check:   "foogroup",
			err:     `group "foo-sub-sub" is invalid: only one level of sub-groups are allowed for groups with snaps`,
			comment: "mixed parent group with more than 1 level of sub-grouping",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
					SubGroups:   []string{"foo-sub"},
				},
				"foo-sub": {
					Name:        "foo-sub",
					MemoryLimit: quantity.SizeMiB * 8,
					ParentGroup: "foogroup",
					SubGroups:   []string{"foo-sub-sub"},
				},
				"foo-sub-sub": {
					Name:        "foo-sub-sub",
					MemoryLimit: quantity.SizeMiB * 4,
					ParentGroup: "foo-sub",
				},
			},
			check:   "foo-sub",
			err:     `group "foo-sub" is invalid: only one level of sub-groups are allowed for groups with snaps`,
			comment: "mixed parent group with more than 1 level of sub-grouping, verifying foo-sub",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
					SubGroups:   []string{"foo-sub"},
				},
				"foo-sub": {
					Name:        "foo-sub",
					MemoryLimit: quantity.SizeMiB * 8,
					ParentGroup: "foogroup",
					SubGroups:   []string{"foo-sub-sub"},
				},
				"foo-sub-sub": {
					Name:        "foo-sub-sub",
					MemoryLimit: quantity.SizeMiB * 4,
					ParentGroup: "foo-sub",
				},
			},
			check:   "foo-sub-sub",
			err:     `group "foo-sub-sub" is invalid: only one level of sub-groups are allowed for groups with snaps`,
			comment: "mixed parent group with more than 1 level of sub-grouping, verifying foo-sub-sub",
		},
		{
			grps: map[string]*quota.Group{
				"foogroup": {
					Name:        "foogroup",
					MemoryLimit: quantity.SizeMiB * 16,
					Snaps:       []string{"test-snap"},
					SubGroups:   []string{"foo-sub"},
				},
				"foo-sub": {
					Name:        "foo-sub",
					MemoryLimit: quantity.SizeMiB * 8,
					ParentGroup: "foogroup",
					Snaps:       []string{"test-snap"},
				},
			},
			check:   "foogroup",
			err:     `group "foogroup" is invalid: nesting of groups with snaps is not supported`,
			comment: "mixed parent group with nested snap, verifying foogroup",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		mylog.
			// resolve cross references as we need group pointers to be updated
			Check(quota.ResolveCrossReferences(t.grps))
		c.Assert(err, IsNil, comment)
		grpToCheck := t.grps[t.check]
		c.Assert(grpToCheck, NotNil, comment)
		mylog.Check(grpToCheck.ValidateNestingAndSnaps())
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
	}
}

func (ts *quotaTestSuite) TestChangingRequirementsDoesNotBreakExistingGroups(c *C) {
	tt := []struct {
		grp     *quota.Group
		err     string
		comment string
	}{
		// Test that an existing group with lower than 640kB limit
		// does not break .validate(), since the requirement was increased
		{
			grp: &quota.Group{
				Name:        "foogroup",
				MemoryLimit: quantity.SizeKiB * 12,
			},
			comment: "group with a lower memory limit than 640kB",
		},
	}

	for _, t := range tt {
		comment := Commentf(t.comment)
		mylog.Check(t.grp.ValidateGroup())
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err, comment)
		} else {
			c.Assert(err, IsNil, comment)
		}
	}
}

func (ts *quotaTestSuite) TestAddAllNecessaryGroupsAvoidsInfiniteRecursion(c *C) {
	grp := mylog.Check2(quota.NewGroup("infinite-group", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	grp2 := mylog.Check2(grp.NewSubGroup("infinite-group2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// create a cycle artificially to the same group
	grp2.SetInternalSubGroups([]*quota.Group{grp2})

	// now we fail to add this to a quota set
	qs := &quota.QuotaGroupSet{}
	mylog.Check(qs.AddAllNecessaryGroups(grp))
	c.Assert(err, ErrorMatches, "internal error: circular reference found")

	// create a more difficult to detect cycle going from the child to the
	// parent
	grp2.SetInternalSubGroups([]*quota.Group{grp})
	mylog.Check(qs.AddAllNecessaryGroups(grp))
	c.Assert(err, ErrorMatches, "internal error: circular reference found")

	// make a real sub-group and try one more level of indirection going back
	// to the parent
	grp2.SetInternalSubGroups(nil)
	grp3 := mylog.Check2(grp2.NewSubGroup("infinite-group3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	grp3.SetInternalSubGroups([]*quota.Group{grp})
	mylog.Check(qs.AddAllNecessaryGroups(grp))
	c.Assert(err, ErrorMatches, "internal error: circular reference found")
}

func (ts *quotaTestSuite) TestAddAllNecessaryGroups(c *C) {
	qs := &quota.QuotaGroupSet{}

	// it should initially be empty
	c.Assert(qs.AllQuotaGroups(), HasLen, 0)

	grp1 := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.

		// add the group and make sure it is in the set
		Check(qs.AddAllNecessaryGroups(grp1))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1})
	mylog.

		// adding multiple times doesn't change the set
		Check(qs.AddAllNecessaryGroups(grp1))

	mylog.Check(qs.AddAllNecessaryGroups(grp1))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1})

	// add a new group and make sure it is in the set now
	grp2 := mylog.Check2(quota.NewGroup("myroot2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.Check(qs.AddAllNecessaryGroups(grp2))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2})

	// start again
	qs = &quota.QuotaGroupSet{}

	// make a sub-group and add the root group - it will automatically add
	// the sub-group without us needing to explicitly add the sub-group
	subgrp1 := mylog.Check2(grp1.NewSubGroup("mysub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.
		// add grp2 as well
		Check(qs.AddAllNecessaryGroups(grp2))

	mylog.Check(qs.AddAllNecessaryGroups(grp1))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, subgrp1})
	mylog.

		// we can explicitly add the sub-group and still have the same set too
		Check(qs.AddAllNecessaryGroups(subgrp1))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, subgrp1})

	// create a new set of group and sub-groups to add the deepest child group
	// and add that, and notice that the root groups are also added
	grp3 := mylog.Check2(quota.NewGroup("myroot3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp3 := mylog.Check2(grp3.NewSubGroup("mysub3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subsubgrp3 := mylog.Check2(subgrp3.NewSubGroup("mysubsub3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.Check(qs.AddAllNecessaryGroups(subsubgrp3))

	c.Assert(qs.AllQuotaGroups(), DeepEquals, []*quota.Group{grp1, grp2, grp3, subgrp1, subgrp3, subsubgrp3})

	// finally create a tree with multiple branches and ensure that adding just
	// a single deepest child will add all the other deepest children from other
	// branches
	grp4 := mylog.Check2(quota.NewGroup("myroot4", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp4 := mylog.Check2(grp4.NewSubGroup("mysub4", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build()))


	subgrp5 := mylog.Check2(grp4.NewSubGroup("mysub5", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build()))


	// adding just subgrp5 to a quota set will automatically add the other sub
	// group, subgrp4
	qs2 := &quota.QuotaGroupSet{}
	mylog.Check(qs2.AddAllNecessaryGroups(subgrp4))

	c.Assert(qs2.AllQuotaGroups(), DeepEquals, []*quota.Group{grp4, subgrp4, subgrp5})
}

func (ts *quotaTestSuite) TestResolveCrossReferencesLimitCheckSkipsSelf(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("mysub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("mysub2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	all := map[string]*quota.Group{
		"myroot": grp1,
		"mysub1": subgrp1,
		"mysub2": subgrp2,
	}
	mylog.Check(quota.ResolveCrossReferences(all))

}

func (ts *quotaTestSuite) TestResolveCrossReferencesCircular(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("mysub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("mysub2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	all := map[string]*quota.Group{
		"myroot": grp1,
		"mysub1": subgrp1,
		"mysub2": subgrp2,
	}
	// try to set up circular ref
	subgrp2.SubGroups = append(subgrp2.SubGroups, "mysub1")
	mylog.Check(quota.ResolveCrossReferences(all))
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

	grp1 := mylog.Check2(quota.NewGroup("group", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// group initially is inactive, so it has no current memory usage
	currentMem := mylog.Check2(grp1.CurrentMemoryUsage())

	c.Assert(currentMem, Equals, quantity.Size(0))

	// now with the slice mocked as active it has real usage
	currentMem = mylog.Check2(grp1.CurrentMemoryUsage())

	c.Assert(currentMem, Equals, 4*quantity.SizeKiB)

	// but it can also have 0 usage
	currentMem = mylog.Check2(grp1.CurrentMemoryUsage())

	c.Assert(currentMem, Equals, quantity.Size(0))

	// and it can also be an incredibly huge value too
	currentMem = mylog.Check2(grp1.CurrentMemoryUsage())

	const sixteenExb = quantity.Size(1<<64 - 1)
	c.Assert(currentMem, Equals, sixteenExb)
}

func (ts *quotaTestSuite) TestCurrentTaskUsage(c *C) {
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		systemctlCalls++
		switch systemctlCalls {

		// inactive case, number of tasks must be 0
		case 1:
			// first time pretend the service is inactive
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("inactive"), systemctlInactiveServiceError{}

		// active cases
		case 2:
			// now pretend it is active
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("active"), nil
		case 3:
			// and the task count can be non-zero like
			c.Assert(args, DeepEquals, []string{"show", "--property", "TasksCurrent", "snap.group.slice"})
			return []byte("TasksCurrent=32"), nil

		case 4:
			// now pretend it is active
			c.Assert(args, DeepEquals, []string{"is-active", "snap.group.slice"})
			return []byte("active"), nil
		case 5:
			// and no tasks are active
			c.Assert(args, DeepEquals, []string{"show", "--property", "TasksCurrent", "snap.group.slice"})
			return []byte("TasksCurrent=0"), nil

		default:
			c.Errorf("unexpected number of systemctl calls (%d) (current call is %+v)", systemctlCalls, args)
			return []byte("broken test"), fmt.Errorf("broken test")
		}
	})
	defer r()

	grp1 := mylog.Check2(quota.NewGroup("group", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))


	// group initially is inactive, so it has no current task usage
	currentTasks := mylog.Check2(grp1.CurrentTaskUsage())
	c.Check(err, IsNil)
	c.Check(currentTasks, Equals, 0)
	c.Check(systemctlCalls, Equals, 1)

	// now with the slice mocked as active it has real usage
	currentTasks = mylog.Check2(grp1.CurrentTaskUsage())
	c.Check(err, IsNil)
	c.Check(currentTasks, Equals, 32)
	c.Check(systemctlCalls, Equals, 3)

	// but it can also have 0 usage
	currentTasks = mylog.Check2(grp1.CurrentTaskUsage())
	c.Check(err, IsNil)
	c.Check(currentTasks, Equals, 0)
	c.Check(systemctlCalls, Equals, 5)
}

func (ts *quotaTestSuite) TestGetGroupQuotaAllocations(c *C) {
	// Verify we get the correct allocations for a group with a more complex tree-structure
	// and different quotas split out into different sub-groups.
	// The tree we will be verifying will be like this
	//                   <groot>                     (root group, 1GB Memory)
	// 				  /    |      \
	// 	     <cpu-q0>      |       \                 (subgroup, 2x50% Cpu Quota)
	// 		 /        <thread-q0>   \                (subgroup, 32 threads)
	//      /	           |     <cpus-q0>           (subgroup, cpu-set quota with cpus 0,1)
	// <mem-q1>        <mem-q2>       \              (2 subgroups, 256MB Memory each)
	//    |                |       <cpus-q1>         (subgroup, cpu-set quota with cpus 0)
	// <cpu-q1>        <thread-q1>                   (subgroups, cpu quota of 50%, thread quota of 16)
	//                     |
	//                 <mem-q3>                      (subgroup, 128MB Memory)
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	cpuq0 := mylog.Check2(grp1.NewSubGroup("cpu-q0", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	thrq0 := mylog.Check2(grp1.NewSubGroup("thread-q0", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))


	cpusq0 := mylog.Check2(grp1.NewSubGroup("cpus-q0", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))


	memq1 := mylog.Check2(cpuq0.NewSubGroup("mem-q1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB*256).Build()))


	memq2 := mylog.Check2(thrq0.NewSubGroup("mem-q2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB*256).Build()))


	_ = mylog.Check2(cpusq0.NewSubGroup("cpus-q1", quota.NewResourcesBuilder().WithCPUSet([]int{0}).Build()))
	c.Check(err, IsNil)

	_ = mylog.Check2(memq1.NewSubGroup("cpu-q1", quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()))
	c.Check(err, IsNil)

	thrq1 := mylog.Check2(memq2.NewSubGroup("thread-q1", quota.NewResourcesBuilder().WithThreadLimit(16).Build()))


	_ = mylog.Check2(thrq1.NewSubGroup("mem-q3", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB*128).Build()))
	c.Check(err, IsNil)

	// Now we verify that the reservations made for the relevant groups are correct. The upper parent group will
	// contained a combined overview of reserveations made.
	allReservations := grp1.InspectInternalQuotaAllocations()

	// Verify the root group
	c.Check(allReservations["groot"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryLimit:               quantity.SizeGiB,
		MemoryReservedByChildren:  quantity.SizeMiB * 512,
		CPUReservedByChildren:     100,
		ThreadsReservedByChildren: 32,
		CPUSetLimit:               []int{},
		CPUSetReservedByChildren:  []int{0, 1},
	})

	// Verify the subgroup cpu-q0
	c.Check(allReservations["cpu-q0"], DeepEquals, &quota.GroupQuotaAllocations{
		CPULimit:                 100,
		CPUReservedByChildren:    50,
		MemoryReservedByChildren: quantity.SizeMiB * 256,
		CPUSetLimit:              []int{},
	})

	// Verify the subgroup thread-q0
	c.Check(allReservations["thread-q0"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryReservedByChildren:  quantity.SizeMiB * 256,
		ThreadsLimit:              32,
		ThreadsReservedByChildren: 16,
		CPUSetLimit:               []int{},
	})

	// Verify the subgroup cpus-q0
	c.Check(allReservations["cpus-q0"], DeepEquals, &quota.GroupQuotaAllocations{
		CPUSetLimit:              []int{0, 1},
		CPUSetReservedByChildren: []int{0},
	})

	// Verify the subgroup cpus-q1
	c.Check(allReservations["cpus-q1"], DeepEquals, &quota.GroupQuotaAllocations{
		CPUSetLimit: []int{0},
	})

	// Verify the subgroup mem-q1
	c.Check(allReservations["mem-q1"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryLimit:           quantity.SizeMiB * 256,
		CPUReservedByChildren: 50,
		CPUSetLimit:           []int{},
	})

	// Verify the subgroup mem-q2
	c.Check(allReservations["mem-q2"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryLimit:               quantity.SizeMiB * 256,
		MemoryReservedByChildren:  quantity.SizeMiB * 128,
		ThreadsReservedByChildren: 16,
		CPUSetLimit:               []int{},
	})

	// Verify the subgroup cpu-q1
	c.Check(allReservations["cpu-q1"], DeepEquals, &quota.GroupQuotaAllocations{
		CPULimit:    50,
		CPUSetLimit: []int{},
	})

	// Verify the subgroup thread-q1
	c.Check(allReservations["thread-q1"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryReservedByChildren: quantity.SizeMiB * 128,
		ThreadsLimit:             16,
		CPUSetLimit:              []int{},
	})

	// Verify the subgroup mem-q3
	c.Check(allReservations["mem-q3"], DeepEquals, &quota.GroupQuotaAllocations{
		MemoryLimit: quantity.SizeMiB * 128,
		CPUSetLimit: []int{},
	})
}

func (ts *quotaTestSuite) TestNestingOfLimitsWithExceedingParent(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	_ = mylog.Check2(grp1.NewSubGroup("thread-sub", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))
	c.Check(err, IsNil)

	_ = mylog.Check2(grp1.NewSubGroup("cpus-sub", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))
	c.Check(err, IsNil)

	// Now we have the root with a memory limit, and three subgroups with
	// each with one of the remaining limits. The point of this test is to make
	// sure nested cases of limits that don't fit are caught and reported. So in a
	// sub-sub group we create a limit higher than the upper parent
	_ = mylog.Check2(subgrp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB*2).Build()))
	c.Check(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside group \"groot\" remaining quota space 1 GiB`)
}

func (ts *quotaTestSuite) TestNestingOfLimitsWithExceedingSiblings(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	_ = mylog.Check2(grp1.NewSubGroup("thread-sub", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))
	c.Check(err, IsNil)

	subgrp2 := mylog.Check2(grp1.NewSubGroup("cpus-sub", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))
	c.Check(err, IsNil)

	// The point here is to catch if we, in a nested, scenario, together with our siblings
	// exceed one of the parent's limits.
	subgrp3 := mylog.Check2(subgrp1.NewSubGroup("mem-sub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	_ = mylog.Check2(subgrp3.NewSubGroup("mem-sub-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))
	c.Check(err, IsNil)

	// now we have consumed the entire memory quota set by the parent, so this should fail
	_ = mylog.Check2(subgrp2.NewSubGroup("mem-sub2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))
	c.Check(err, ErrorMatches, `sub-group memory limit of 1 GiB is too large to fit inside group \"groot\" remaining quota space 0 B`)
}

func (ts *quotaTestSuite) TestChangingSubgroupLimits(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a memory limit of only half, then we try to adjust the value to another
	// larger value. This must succeed.
	memgrp := mylog.Check2(subgrp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build()))

	mylog.

		// Now we change it to fill the entire quota of our upper parent
		Check(memgrp.UpdateQuotaLimits(quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))
	c.Check(err, IsNil)
	mylog.

		// Now we try to change the limits of the subgroup to a value that is too large to fit inside the parent,
		// the error message should also correctly report that the remaining space is 1GiB, as it should not consider
		// the current memory quota of the subgroup.
		Check(memgrp.UpdateQuotaLimits(quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build()))
	c.Check(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside group \"groot\" remaining quota space 1 GiB`)
}

func (ts *quotaTestSuite) TestChangingParentMemoryLimits(c *C) {
	// The purpose here is to make sure we can't change the limits of the parent group
	// that would otherwise conflict with the current usage of limits by children of the
	// parent.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a memory limit that takes up the entire quota of the parent
	_ = mylog.Check2(subgrp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.

		// Now the test is to change the upper most parent limit so that it would be less
		// than the current usage, which we should not be able to do
		Check(grp1.QuotaUpdateCheck(quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build()))
	c.Check(err, ErrorMatches, `group memory limit of 512 MiB is too small to fit current subgroup usage of 1 GiB`)
}

func (ts *quotaTestSuite) TestChangingParentCpuPercentageLimits(c *C) {
	// The purpose here is to make sure we can't change the limits of the parent group
	// that would otherwise conflict with the current usage of limits by children of the
	// parent.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// Create a nested subgroup with a cpu limit that takes up the entire quota of the parent
	_ = mylog.Check2(subgrp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))

	mylog.

		// Now the test is to change the upper most parent limit so that it would be less
		// than the current usage, which we should not be able to do
		Check(grp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()))
	c.Check(err, ErrorMatches, `group cpu limit of 50% is less than current subgroup usage of 100%`)
}

func (ts *quotaTestSuite) TestChangingParentCpuSetLimits(c *C) {
	// The purpose here is to make sure we can't change the limits of the parent group
	// that would otherwise conflict with the current usage of limits by children of the
	// parent.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a cpu limit that uses both of allowed cpus
	_ = mylog.Check2(subgrp1.NewSubGroup("cpuset-sub", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))

	mylog.

		// Now the test is to change the upper most parent limit so that it would be more
		// restrictive then the previous limit
		Check(grp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUSet([]int{0}).Build()))
	c.Check(err, ErrorMatches, `group cpu-set \[0\] is not a superset of current subgroup usage of \[0 1\]`)
}

func (ts *quotaTestSuite) TestChangingParentThreadLimits(c *C) {
	// The purpose here is to make sure we can't change the limits of the parent group
	// that would otherwise conflict with the current usage of limits by children of the
	// parent.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a thread limit that takes up the entire quota of the parent
	_ = mylog.Check2(subgrp1.NewSubGroup("thread-sub", quota.NewResourcesBuilder().WithThreadLimit(32).Build()))

	mylog.

		// Now the test is to change the upper most parent limit so that it would be less
		// than the current usage, which we should not be able to do
		Check(grp1.QuotaUpdateCheck(quota.NewResourcesBuilder().WithThreadLimit(16).Build()))
	c.Check(err, ErrorMatches, `group thread limit of 16 is too small to fit current subgroup usage of 32`)
}

func (ts *quotaTestSuite) TestChangingMiddleParentLimits(c *C) {
	// Catch any algorithmic mistakes made in regards to not catching parents
	// that are also children of other parents.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a memory limit that takes up the entire quota of the upper parent
	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// Create a nested subgroup with a cpu limit that takes up the entire quota of the middle parent
	_ = mylog.Check2(subgrp2.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))

	mylog.

		// Now the test is to change the middle parent limit so that it would be less
		// than the current usage, which we should not be able to do
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()))
	c.Check(err, ErrorMatches, `group cpu limit of 50% is less than current subgroup usage of 100%`)
}

func (ts *quotaTestSuite) TestAddingNewMiddleParentMemoryLimits(c *C) {
	// The purpose here is to make sure we catch any new limits inserted into
	// the tree, which would conflict with the current usage.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB*2).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a memory limit that takes half of the quota of the upper parent
	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("mem-sub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// Create a nested subgroup with a cpu limit that takes up the entire quota of the middle parent
	_ = mylog.Check2(subgrp2.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))

	mylog.

		// Now lets inject a memory quota that is less than currently used by children
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB * 512).Build()))
	c.Check(err, ErrorMatches, `group memory limit of 512 MiB is too small to fit current subgroup usage of 1 GiB`)
	mylog.

		// Now lets inject one that is larger, that should be possible
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB * 2).Build()))
	c.Check(err, IsNil)
}

func (ts *quotaTestSuite) TestAddingNewMiddleParentCpuLimits(c *C) {
	// The purpose here is to make sure we catch any new limits inserted into
	// the tree, which would conflict with the current usage.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("mem-sub1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// Create a nested subgroup with a cpu limit that takes half of the quota of the upper parent
	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("cpu-sub", quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a memory limit that takes up the entire quota of the middle parent
	_ = mylog.Check2(subgrp2.NewSubGroup("mem-sub2", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	mylog.

		// Now lets inject a cpu quota that is less than currently used by children
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(25).Build()))
	c.Check(err, ErrorMatches, `group cpu limit of 25% is less than current subgroup usage of 50%`)
	mylog.

		// Now lets inject one that is larger, that should be possible
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))
	c.Check(err, IsNil)
}

func (ts *quotaTestSuite) TestAddingNewMiddleParentCpuSetLimits(c *C) {
	// The purpose here is to make sure we catch any new limits inserted into
	// the tree, which would conflict with the current usage.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1, 2, 3}).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a more restrictive cpu-set of the upper parent
	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("cpuset-sub", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))


	// Create a nested subgroup with a cpu limit that takes up the entire quota of the middle parent
	_ = mylog.Check2(subgrp2.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))

	mylog.

		// Now lets inject a cpu-set that does not match whats currently used by children
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUSet([]int{2, 3}).Build()))
	c.Check(err, ErrorMatches, `group cpu-set \[2 3\] is not a superset of current subgroup usage of \[0 1\]`)
	mylog.

		// Now lets inject one that is larger, that should be possible
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithCPUSet([]int{0, 1, 2}).Build()))
	c.Check(err, IsNil)
}

func (ts *quotaTestSuite) TestAddingNewMiddleParentThreadLimits(c *C) {
	// The purpose here is to make sure we catch any new limits inserted into
	// the tree, which would conflict with the current usage.
	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithThreadLimit(1024).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))


	// Create a nested subgroup with a thread limit that takes half of the quota of the upper parent
	subgrp2 := mylog.Check2(subgrp1.NewSubGroup("thread-sub", quota.NewResourcesBuilder().WithThreadLimit(512).Build()))


	// Create a nested subgroup with a cpu limit that takes up the entire quota of the middle parent
	_ = mylog.Check2(subgrp2.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(50).Build()))

	mylog.

		// Now lets inject a thread quota that is less than currently used by children
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithThreadLimit(256).Build()))
	c.Check(err, ErrorMatches, `group thread limit of 256 is too small to fit current subgroup usage of 512`)
	mylog.

		// Now lets inject one that is larger, that should be possible
		Check(subgrp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithThreadLimit(1024).Build()))
	c.Check(err, IsNil)
}

func (ts *quotaTestSuite) TestCombinedCpuPercentageWithCpuSetLimits(c *C) {
	// mock the CPU count to be above 2
	restore := quota.MockRuntimeNumCPU(func() int { return 4 })
	defer restore()

	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))


	// Create a subgroup of the CPU set of 0,1 with 50% allowed CPU usage. This should result in a combined
	// allowance of 100%
	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUPercentage(50).Build()))

	c.Check(subgrp1.GetCPUQuotaPercentage(), Equals, 100)

	_ = mylog.Check2(grp1.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(8).WithCPUPercentage(50).Build()))
	c.Assert(err, ErrorMatches, `sub-group cpu limit of 400% is too large to fit inside group "groot" with allowed CPU set \[0 1\]`)
}

func (ts *quotaTestSuite) TestCombinedCpuPercentageWithLowCoreCount(c *C) {
	// mock the CPU count to be above 1
	restore := quota.MockRuntimeNumCPU(func() int { return 1 })
	defer restore()

	grp1 := mylog.Check2(quota.NewGroup("groot", quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()))


	subgrp1 := mylog.Check2(grp1.NewSubGroup("cpu-sub1", quota.NewResourcesBuilder().WithCPUPercentage(50).Build()))


	// Even though the CPU set is set to cores 0+1, which technically means that a CPUPercentage of 50 would
	// be half of this, the CPU percentage is capped at at total of 100% because the number of cores on the system
	// is 1.
	c.Check(subgrp1.GetCPUQuotaPercentage(), Equals, 50)

	subgrp2 := mylog.Check2(grp1.NewSubGroup("cpu-sub2", quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(50).Build()))


	// Verify that the number of cpus are now correctly reported as the one explicitly set
	// by the quota
	c.Check(subgrp2.GetCPUQuotaPercentage(), Equals, 200)
}

func (ts *quotaTestSuite) TestJournalQuotasSetCorrectly(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("groot1", quota.NewResourcesBuilder().WithJournalNamespace().Build()))

	c.Assert(grp1.JournalLimit, NotNil)

	grp2 := mylog.Check2(quota.NewGroup("groot2", quota.NewResourcesBuilder().WithJournalRate(15, time.Second).Build()))

	c.Assert(grp2.JournalLimit, NotNil)
	c.Check(grp2.JournalLimit.RateCount, Equals, 15)
	c.Check(grp2.JournalLimit.RatePeriod, Equals, time.Second)

	grp3 := mylog.Check2(quota.NewGroup("groot3", quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build()))

	c.Assert(grp3.JournalLimit, NotNil)
	c.Check(grp3.JournalLimit.Size, Equals, quantity.SizeMiB)
}

func (ts *quotaTestSuite) TestJournalQuotasUpdatesCorrectly(c *C) {
	grp1 := mylog.Check2(quota.NewGroup("groot1", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))

	c.Assert(grp1.JournalLimit, IsNil)

	grp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithJournalNamespace().Build())
	c.Assert(grp1.JournalLimit, NotNil)
	c.Check(grp1.JournalLimit.Size, Equals, quantity.Size(0))
	c.Check(grp1.JournalLimit.RateCount, Equals, 0)
	c.Check(grp1.JournalLimit.RatePeriod, Equals, time.Duration(0))

	grp1.UpdateQuotaLimits(quota.NewResourcesBuilder().WithJournalRate(15, time.Microsecond*5).WithJournalSize(quantity.SizeMiB).Build())
	c.Assert(grp1.JournalLimit, NotNil)
	c.Check(grp1.JournalLimit.Size, Equals, quantity.SizeMiB)
	c.Check(grp1.JournalLimit.RateCount, Equals, 15)
	c.Check(grp1.JournalLimit.RatePeriod, Equals, time.Microsecond*5)
}

func (ts *quotaTestSuite) TestServiceMapEmptyOnEmptyGroup(c *C) {
	rootGrp := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	// Check the root group now. No services exists yet, so this should yield an empty map
	serviceMap := rootGrp.ServiceMap()
	c.Check(serviceMap, DeepEquals, map[string]*quota.Group{})
}

func (ts *quotaTestSuite) TestServiceMapEmptyOnGroupWithNoServices(c *C) {
	rootGrp := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	_ = mylog.Check2(rootGrp.NewSubGroup("mysub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build()))


	// Add a snap, this should yield no difference as services that are not
	// in service sub-groups are not included, and the fact that ServiceMap does
	// not look into snap.Info, but relies completely on local information in the group.
	rootGrp.Snaps = append(rootGrp.Snaps, "my-snap")

	// Let's also add a service, while not permitted, we can do this as we manually do
	// modifications. This service should not be included.
	rootGrp.Services = append(rootGrp.Services, "my-snap.uh-oh")

	// Check the root group now. No services exists yet, so this should yield an empty map
	serviceMap := rootGrp.ServiceMap()
	c.Check(serviceMap, DeepEquals, map[string]*quota.Group{})
}

func (ts *quotaTestSuite) TestServiceMapHappy(c *C) {
	rootGrp := mylog.Check2(quota.NewGroup("myroot", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	svcGrp := mylog.Check2(rootGrp.NewSubGroup("mysub", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB/2).Build()))


	// add a service to the service sub-group, this should now be included
	svcGrp.Services = []string{"my-snap.service"}
	serviceMap := rootGrp.ServiceMap()
	c.Check(serviceMap, DeepEquals, map[string]*quota.Group{
		"my-snap.service": svcGrp,
	})
}
