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
		{quota.Resources{CPU: &quota.ResourceCPU{}}, `cannot validate quota limits with a cpu quota of 0 and allowed cpus of 0`},
		{quota.Resources{Threads: &quota.ResourceThreads{}}, `cannot create quota group with a thread count of 0`},
		{quota.NewResources(quantity.Size(0), 0, 0, nil, 0), `quota group must have at least one resource limit set`},
		{quota.NewResources(quantity.SizeKiB, 0, 0, nil, 0), `memory limit 1024 is too small: size must be larger than 4KB`},
		{quota.NewResources(0, 1, 0, nil, 0), `cannot validate quota limits with count of >0 and percentage of 0`},
	}

	for _, t := range tests {
		err := t.limits.Validate()
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *resourcesTestSuite) TestQuotaValidationPasses(c *C) {
	tests := []struct {
		limits quota.Resources
	}{
		{quota.NewResources(quantity.SizeMiB, 0, 0, nil, 0)},
		{quota.NewResources(0, 1, 50, nil, 0)},
		{quota.NewResources(0, 4, 25, nil, 0)},
		{quota.NewResources(0, 0, 0, []int{0, 1}, 0)},
		{quota.NewResources(0, 0, 0, nil, 16)},
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
		{quota.NewResources(quantity.SizeMiB, 0, 0, nil, 0), quota.Resources{&quota.ResourceMemory{0}, nil, nil}, `cannot remove memory limit from quota group`},
		{quota.NewResources(quantity.SizeMiB, 0, 0, nil, 0), quota.NewResources(5*quantity.SizeKiB, 0, 0, nil, 0), `cannot decrease memory limit, remove and re-create it to decrease the limit`},
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
		{quota.NewResources(quantity.SizeMiB, 0, 0, nil, 0), quota.NewResources(quantity.SizeGiB, 0, 0, nil, 0), quota.NewResources(quantity.SizeGiB, 0, 0, nil, 0)},
		{quota.NewResources(quantity.SizeMiB, 0, 0, nil, 0), quota.NewResources(0, 4, 25, nil, 0), quota.NewResources(quantity.SizeMiB, 4, 25, nil, 0)},
		{quota.NewResources(quantity.SizeMiB, 4, 25, nil, 0), quota.NewResources(0, 0, 0, []int{0}, 0), quota.NewResources(quantity.SizeMiB, 4, 25, []int{0}, 0)},
		{quota.NewResources(quantity.SizeMiB, 4, 25, []int{0}, 0), quota.NewResources(0, 0, 0, nil, 128), quota.NewResources(quantity.SizeMiB, 4, 25, []int{0}, 128)},
		{quota.NewResources(0, 1, 100, nil, 0), quota.NewResources(quantity.SizeGiB, 0, 0, nil, 32), quota.NewResources(quantity.SizeGiB, 1, 100, nil, 32)},
	}

	for _, t := range tests {
		err := t.limits.Change(t.updateLimits)
		c.Check(err, IsNil)
		c.Check(t.limits, DeepEquals, t.newLimits)
	}
}
