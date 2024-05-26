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
	"reflect"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/snap/quota"
)

type resourcesTestSuite struct{}

var _ = Suite(&resourcesTestSuite{})

func (s *resourcesTestSuite) TestQuotaValidationFails(c *C) {
	tests := []struct {
		limits quota.Resources
		err    string
	}{
		{quota.NewResourcesBuilder().Build(), `quota group must have at least one resource limit set`},
		{quota.NewResourcesBuilder().WithMemoryLimit(0).Build(), `memory quota must have a limit set`},
		{quota.NewResourcesBuilder().WithCPUPercentage(0).Build(), `invalid cpu quota with a cpu quota of 0`},
		{quota.NewResourcesBuilder().WithCPUSet(nil).Build(), `cpu-set quota must not be empty`},
		{quota.NewResourcesBuilder().WithThreadLimit(0).Build(), `invalid thread quota with a thread count of 0`},
		{quota.NewResourcesBuilder().Build(), `quota group must have at least one resource limit set`},
		{quota.NewResourcesBuilder().WithCPUCount(1).Build(), `invalid cpu quota with count of >0 and percentage of 0`},
		{quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(100).WithCPUSet([]int{0}).Build(), `cpu usage 200% is larger than the maximum allowed for provided set \[0\] of 100%`},
		{quota.NewResourcesBuilder().WithJournalRate(0, 1).Build(), `journal quota must have a period of at least 1 microsecond \(minimum resolution\)`},
		{quota.NewResourcesBuilder().WithJournalRate(1, time.Nanosecond).Build(), `journal quota must have a period of at least 1 microsecond \(minimum resolution\)`},
		{quota.NewResourcesBuilder().WithJournalSize(0).Build(), `journal size quota must have a limit set`},
	}

	for _, t := range tests {
		mylog.Check(t.limits.Validate())
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *resourcesTestSuite) TestResourceCheckFeatureRequirementsCgroupv1(c *C) {
	r := quota.MockCgroupVer(1)
	defer r()

	// normal cpu resource is fine with cgroup v1
	good := quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()
	c.Check(good.Validate(), IsNil)

	// cpu set with cgroup v1 is not supported
	bad := quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()
	c.Check(bad.CheckFeatureRequirements(), ErrorMatches, "cannot use CPU set with cgroup version 1")
}

func (s *resourcesTestSuite) TestResourceCheckFeatureRequirementsCgroupv1Err(c *C) {
	r := quota.MockCgroupVerErr(fmt.Errorf("some cgroup detection error"))
	defer r()

	// normal cpu resource is fine
	good := quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()
	c.Check(good.Validate(), IsNil)

	// cpu set without cgroup detection is not supported
	bad := quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()
	c.Check(bad.CheckFeatureRequirements(), ErrorMatches, "some cgroup detection error")
}

func (s *resourcesTestSuite) TestQuotaValidationPasses(c *C) {
	tests := []struct {
		limits quota.Resources
	}{
		{quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()},
		{quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()},
		{quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build()},
		{quota.NewResourcesBuilder().WithThreadLimit(16).Build()},
		{quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build()},
		{quota.NewResourcesBuilder().WithJournalRate(1, time.Microsecond).Build()},
		{quota.NewResourcesBuilder().WithJournalNamespace().Build()},
	}

	for _, t := range tests {
		mylog.Check(t.limits.Validate())
		c.Check(err, IsNil)
	}
}

func (s *resourcesTestSuite) TestQuotaChangeValidationFails(c *C) {
	tests := []struct {
		limits       quota.Resources
		updateLimits quota.Resources
		err          string
	}{
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(0).Build(),
			`cannot remove memory limit from quota group`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(5 * quantity.SizeKiB).Build(),
			`memory limit 5120 is too small: size must be larger than 640 KiB`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(800 * quantity.SizeKiB).Build(),
			`cannot decrease memory limit, remove and re-create it to decrease the limit`,
		},
		{
			quota.NewResourcesBuilder().WithThreadLimit(64).Build(),
			quota.NewResourcesBuilder().WithThreadLimit(0).Build(),
			`cannot remove thread limit from quota group`,
		},
		{
			quota.NewResourcesBuilder().WithThreadLimit(64).Build(),
			quota.NewResourcesBuilder().WithThreadLimit(32).Build(),
			`cannot decrease thread limit, remove and re-create it to decrease the limit`,
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(75).Build(),
			quota.NewResourcesBuilder().WithCPUPercentage(0).Build(),
			`cannot remove cpu limit from quota group`,
		},
		{
			quota.NewResourcesBuilder().WithThreadLimit(16).WithCPUSet([]int{0, 1}).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{}).Build(),
			`cannot remove all allowed cpus from quota group`,
		},
		// ensure that changes will call "Validate" too
		{
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(1).Build(),
			`memory limit 1 is too small: size must be larger than 640 KiB`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(0).Build(),
			`invalid cpu quota with a cpu quota of 0`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(0).Build(),
			`invalid cpu quota with count of >0 and percentage of 0`,
		},
		{
			quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(8).WithCPUPercentage(100).Build(),
			`cpu usage 800% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(100).WithCPUSet([]int{0, 1}).Build(),
			`cpu usage 400% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(6).WithCPUPercentage(100).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{0, 1}).Build(),
			`cpu usage 600% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{}).Build(),
			`cpu-set quota must not be empty`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithThreadLimit(-1).Build(),
			`invalid thread quota with a thread count of -1`,
		},
		{
			quota.NewResourcesBuilder().WithJournalRate(1, 1).Build(),
			quota.NewResourcesBuilder().WithJournalRate(-2, 0).Build(),
			`journal quota must have a rate count equal to or larger than zero`,
		},
		{
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(0).Build(),
			`cannot remove journal size limit from quota group`,
		},
		{
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(5 * quantity.SizeKiB).Build(),
			`journal size limit 5120 is too small: size must be larger than 64 KiB`,
		},
		{
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(5 * quantity.SizeGiB).Build(),
			`journal size quota must be smaller than 4 GiB`,
		},
	}

	for _, t := range tests {
		mylog.Check(t.limits.Change(t.updateLimits))
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *resourcesTestSuite) TestQuotaChangeValidationPasses(c *C) {
	tests := []struct {
		limits       quota.Resources
		updateLimits quota.Resources

		// this is not strictly necessary as we only have one limit right now
		// but as we add additional limits, the updateLimits will not
		// equal limits or newLimits as it can contain partial updates.
		newLimits quota.Resources
	}{
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).Build(),
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{0}).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0}).Build(),
			quota.NewResourcesBuilder().WithThreadLimit(128).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0}).WithThreadLimit(128).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(100).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).WithThreadLimit(32).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).WithCPUCount(1).WithCPUPercentage(100).WithThreadLimit(32).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithCPUCount(0).WithCPUPercentage(25).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0}).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0, 1, 2}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithCPUSet([]int{0}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithCPUSet([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build(),
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).WithCPUSet([]int{0, 1, 2}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithJournalRate(15, 5*time.Second).Build(),
			quota.NewResourcesBuilder().WithJournalRate(5, 5*time.Second).Build(),
			quota.NewResourcesBuilder().WithJournalRate(5, 5*time.Second).Build(),
		},
		{
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeMiB).Build(),
		},
		{
			quota.NewResourcesBuilder().WithJournalRate(15, 5*time.Second).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).WithJournalRate(15, 5*time.Second).Build(),
		},
		{
			quota.NewResourcesBuilder().WithJournalRate(0, 0).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).WithJournalRate(0, 0).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithJournalSize(quantity.SizeGiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithJournalSize(quantity.SizeGiB).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithJournalRate(15, time.Second).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithJournalRate(15, time.Second).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).Build(),
			quota.NewResourcesBuilder().WithJournalNamespace().Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithJournalNamespace().Build(),
		},
	}

	for _, t := range tests {
		mylog.Check(t.limits.Change(t.updateLimits))
		c.Check(err, IsNil)
		c.Check(t.limits, DeepEquals, t.newLimits)
	}
}

func (s *resourcesTestSuite) TestResourceCloneComplete(c *C) {
	r := &quota.Resources{}
	rv := reflect.ValueOf(r).Elem()
	fieldPtrs := make([]uintptr, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		fv := rv.Field(i)
		ft := fv.Type()
		nv := reflect.New(ft.Elem())
		fv.Set(nv)
		fieldPtrs[i] = fv.Pointer()
	}

	// Clone the resource and ensure there are no un-initialized
	// fields in the resource after the clone. Also check that the
	// fields pointers changed too (ensure the sub-struct really
	// got copied not just assigned to the clone)
	r2 := quota.ResourcesClone(r)
	rv = reflect.ValueOf(r2)
	for i := 0; i < rv.NumField(); i++ {
		fv := rv.Field(i)
		c.Check(fv.IsNil(), Equals, false)
		c.Check(fv.Pointer(), Not(Equals), fieldPtrs[i])
	}
}

func (s *resourcesTestSuite) TestResourceBuilerWithJournalNamespaceOnly(c *C) {
	r := quota.NewResourcesBuilder().WithJournalNamespace().Build()
	c.Assert(r.Journal, NotNil)
	c.Check(r.Journal.Rate, IsNil)
	c.Check(r.Journal.Size, IsNil)
}
