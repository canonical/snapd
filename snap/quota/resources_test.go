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

	. "gopkg.in/check.v1"

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
		{quota.Resources{}, `quota group must have at least one resource limit set`},
		{quota.Resources{Memory: &quota.ResourceMemory{}}, `memory quota must have a limit set`},
		{quota.Resources{CPU: &quota.ResourceCPU{}}, `invalid cpu quota with a cpu quota of 0`},
		{quota.Resources{CPUSet: &quota.ResourceCPUSet{}}, `cpu-set quota must not be empty`},
		{quota.Resources{Threads: &quota.ResourceThreads{}}, `invalid thread quota with a thread count of 0`},
		{quota.NewResourcesBuilder().Build(), `quota group must have at least one resource limit set`},
		{quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeKiB).Build(), `memory limit 1024 is too small: size must be larger than 4KB`},
		{quota.NewResourcesBuilder().WithCPUCount(1).Build(), `invalid cpu quota with count of >0 and percentage of 0`},
		{quota.NewResourcesBuilder().WithCPUCount(2).WithCPUPercentage(100).WithAllowedCPUs([]int{0}).Build(), `cpu usage 200% is larger than the maximum allowed for provided set \[0\] of 100%`},
	}

	for _, t := range tests {
		err := t.limits.Validate()
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
	bad := quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1}).Build()
	c.Check(bad.CheckFeatureRequirements(), ErrorMatches, "cannot use CPU set with cgroup version 1")
}

func (s *resourcesTestSuite) TestResourceCheckFeatureRequirementsCgroupv1Err(c *C) {
	r := quota.MockCgroupVerErr(fmt.Errorf("some cgroup detection error"))
	defer r()

	// normal cpu resource is fine
	good := quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()
	c.Check(good.Validate(), IsNil)

	// cpu set without cgroup detection is not supported
	bad := quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1}).Build()
	c.Check(bad.CheckFeatureRequirements(), ErrorMatches, "some cgroup detection error")
}

func (s *resourcesTestSuite) TestQuotaValidationPasses(c *C) {
	tests := []struct {
		limits quota.Resources
	}{
		{quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()},
		{quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build()},
		{quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1}).Build()},
		{quota.NewResourcesBuilder().WithThreadLimit(16).Build()},
	}

	for _, t := range tests {
		err := t.limits.Validate()
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
			quota.NewResourcesBuilder().WithThreadLimit(16).WithAllowedCPUs([]int{0, 1}).Build(),
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{}).Build(),
			`cannot remove all allowed cpus from quota group`,
		},
		// ensure that changes will call "Validate" too
		{
			quota.NewResourcesBuilder().WithCPUCount(1).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(1).Build(),
			`memory limit 1 is too small: size must be larger than 4KB`,
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
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(8).WithCPUPercentage(100).Build(),
			`cpu usage 800% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(100).WithAllowedCPUs([]int{0, 1}).Build(),
			`cpu usage 400% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(6).WithCPUPercentage(100).Build(),
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1}).Build(),
			`cpu usage 600% is larger than the maximum allowed for provided set \[0 1\] of 200%`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),

			quota.NewResourcesBuilder().WithAllowedCPUs([]int{}).Build(),
			`cpu-set quota must not be empty`,
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build(),

			quota.NewResourcesBuilder().WithThreadLimit(-1).Build(),
			`invalid thread quota with a thread count of -1`,
		},
	}

	for _, t := range tests {
		err := t.limits.Change(t.updateLimits)
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
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{0}).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0}).Build(),
			quota.NewResourcesBuilder().WithThreadLimit(128).Build(),
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0}).WithThreadLimit(128).Build(),
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
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0}).Build(),
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0, 1, 2}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithAllowedCPUs([]int{0}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0}).Build(),
		},
		{
			quota.NewResourcesBuilder().WithCPUCount(4).WithCPUPercentage(25).WithAllowedCPUs([]int{0, 1, 2}).Build(),
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).Build(),
			quota.NewResourcesBuilder().WithCPUCount(1).WithCPUPercentage(50).WithAllowedCPUs([]int{0, 1, 2}).Build(),
		},
	}

	for _, t := range tests {
		err := t.limits.Change(t.updateLimits)
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
