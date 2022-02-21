// -*- Mode: Go; indent-tabs-mode: t -*-

/*
* Copyright (C) 2022 Canonical Ltd
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

package quota

import (
	"github.com/snapcore/snapd/gadget/quantity"
)

type ResourcesBuilder struct {
	MemoryLimit    quantity.Size
	MemoryLimitSet bool

	CPUCount    int
	CPUCountSet bool

	CPUPercentage    int
	CPUPercentageSet bool

	AllowedCPUs    []int
	AllowedCPUsSet bool

	ThreadLimit    int
	ThreadLimitSet bool
}

func (rb *ResourcesBuilder) WithMemoryLimit(limit quantity.Size) *ResourcesBuilder {
	rb.MemoryLimit = limit
	rb.MemoryLimitSet = true
	return rb
}

func (rb *ResourcesBuilder) WithCPUCount(count int) *ResourcesBuilder {
	rb.CPUCount = count
	rb.CPUCountSet = true
	return rb
}

func (rb *ResourcesBuilder) WithCPUPercentage(percentage int) *ResourcesBuilder {
	rb.CPUPercentage = percentage
	rb.CPUPercentageSet = true
	return rb
}

func (rb *ResourcesBuilder) WithAllowedCPUs(allowedCPUs []int) *ResourcesBuilder {
	rb.AllowedCPUs = allowedCPUs
	rb.AllowedCPUsSet = true
	return rb
}

func (rb *ResourcesBuilder) WithThreadLimit(limit int) *ResourcesBuilder {
	rb.ThreadLimit = limit
	rb.ThreadLimitSet = true
	return rb
}

func (rb *ResourcesBuilder) Build() Resources {
	var quotaResources Resources
	if rb.MemoryLimitSet {
		quotaResources.Memory = &ResourceMemory{
			Limit: rb.MemoryLimit,
		}
	}
	if rb.CPUCountSet || rb.CPUPercentageSet || rb.AllowedCPUsSet {
		quotaResources.CPU = &ResourceCPU{
			Count:       rb.CPUCount,
			Percentage:  rb.CPUPercentage,
			AllowedCPUs: rb.AllowedCPUs,
		}
	}
	if rb.ThreadLimitSet {
		quotaResources.Threads = &ResourceThreads{
			Limit: rb.ThreadLimit,
		}
	}
	return quotaResources
}

func NewResourcesBuilder() *ResourcesBuilder {
	return &ResourcesBuilder{
		MemoryLimit:      0,
		MemoryLimitSet:   false,
		CPUCount:         0,
		CPUCountSet:      false,
		CPUPercentage:    0,
		CPUPercentageSet: false,
		AllowedCPUs:      nil,
		AllowedCPUsSet:   false,
		ThreadLimit:      0,
		ThreadLimitSet:   false,
	}
}
