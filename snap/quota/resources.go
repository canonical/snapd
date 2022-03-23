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

package quota

import (
	"fmt"

	"github.com/snapcore/snapd/gadget/quantity"
)

type ResourceMemory struct {
	Limit quantity.Size `json:"limit"`
}

type ResourceCPU struct {
	Count      int `json:"count"`
	Percentage int `json:"percentage"`
}

type ResourceCPUSet struct {
	CPUs []int `json:"cpus"`
}

type ResourceThreads struct {
	Limit int `json:"limit"`
}

// Resources are built up of multiple quota limits. Each quota limit is a pointer
// value to indicate that their presence may be optional, and because we want to detect
// whenever someone changes a limit to '0' explicitly.
type Resources struct {
	Memory  *ResourceMemory  `json:"memory,omitempty"`
	CPU     *ResourceCPU     `json:"cpu,omitempty"`
	CPUSet  *ResourceCPUSet  `json:"cpu-set,omitempty"`
	Threads *ResourceThreads `json:"thread,omitempty"`
}

func (qr *Resources) validateMemoryQuota() error {
	// make sure the memory limit is not zero
	if qr.Memory.Limit == 0 {
		return fmt.Errorf("memory quota must have a limit set")
	}

	// make sure the memory limit is at least 4K, that is the minimum size
	// to allow nesting, otherwise groups with less than 4K will trigger the
	// oom killer to be invoked when a new group is added as a sub-group to the
	// larger group.
	if qr.Memory.Limit <= 4*quantity.SizeKiB {
		return fmt.Errorf("memory limit %d is too small: size must be larger than 4KB", qr.Memory.Limit)
	}
	return nil
}

func (qr *Resources) validateCPUQuota() error {
	// if cpu count is non-zero, then percentage should be set
	if qr.CPU.Count != 0 && qr.CPU.Percentage == 0 {
		return fmt.Errorf("invalid cpu quota with count of >0 and percentage of 0")
	}

	// at least one cpu limit value must be set
	if qr.CPU.Count == 0 && qr.CPU.Percentage == 0 {
		return fmt.Errorf("invalid cpu quota with a cpu quota of 0")
	}
	return nil
}

func (qr *Resources) validateCPUSetQuota() error {
	if len(qr.CPUSet.CPUs) == 0 {
		return fmt.Errorf("cpu-set quota must not be empty")
	}
	return nil
}

func (qr *Resources) validateThreadQuota() error {
	// make sure the thread count is greater than 0
	if qr.Threads.Limit <= 0 {
		return fmt.Errorf("invalid thread quota with a thread count of %d", qr.Threads.Limit)
	}
	return nil
}

// Validate performs validation of the provided quota resources for a group.
// The restrictions imposed are that at least one limit should be set.
// If memory limit is provided, it must be above 4KB.
// If cpu percentage is provided, it must be between 1 and 100.
// If cpu set is provided, it must not be empty.
// If thread count is provided, it must be above 0.
func (qr *Resources) Validate() error {
	if qr.Memory == nil && qr.CPU == nil && qr.CPUSet == nil && qr.Threads == nil {
		return fmt.Errorf("quota group must have at least one resource limit set")
	}

	if qr.Memory != nil {
		if err := qr.validateMemoryQuota(); err != nil {
			return err
		}
	}

	if qr.CPU != nil {
		if err := qr.validateCPUQuota(); err != nil {
			return err
		}
	}

	if qr.CPUSet != nil {
		if err := qr.validateCPUSetQuota(); err != nil {
			return err
		}
	}

	if qr.Threads != nil {
		if err := qr.validateThreadQuota(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateChange performs validation of new quota limits against the current limits.
// This is to catch issues where we want to guard against lowering limits where not supported
// or to make sure that certain/all limits are not removed.
func (qr *Resources) ValidateChange(newLimits Resources) error {
	// Check that the memory limit is not being decreased
	if newLimits.Memory != nil {
		if newLimits.Memory.Limit == 0 {
			return fmt.Errorf("cannot remove memory limit from quota group")
		}

		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if qr.Memory != nil && newLimits.Memory.Limit < qr.Memory.Limit {
			return fmt.Errorf("cannot decrease memory limit, remove and re-create it to decrease the limit")
		}
	}

	// Check that the cpu limit is not being removed, we do not support setting these
	// two settings individually. Count/Percentage must be updated in unison.
	if newLimits.CPU != nil && qr.CPU != nil {
		if newLimits.CPU.Count == 0 && qr.CPU.Count != 0 {
			return fmt.Errorf("cannot remove cpu limit from quota group")
		}
		if newLimits.CPU.Percentage == 0 && qr.CPU.Percentage != 0 {
			return fmt.Errorf("cannot remove cpu limit from quota group")
		}
	}

	// Check that we are not removing the entire cpu set
	if newLimits.CPUSet != nil && qr.CPUSet != nil {
		if len(newLimits.CPUSet.CPUs) == 0 {
			return fmt.Errorf("cannot remove all allowed cpus from quota group")
		}
	}

	// Check that the thread limit is not being decreased
	if newLimits.Threads != nil {
		if newLimits.Threads.Limit == 0 {
			return fmt.Errorf("cannot remove thread limit from quota group")
		}

		// we disallow decreasing the thread limit initially until we understand
		// the full consequences of doing so.
		if qr.Threads != nil && newLimits.Threads.Limit < qr.Threads.Limit {
			return fmt.Errorf("cannot decrease thread limit, remove and re-create it to decrease the limit")
		}
	}

	return nil
}

// clone returns a deep copy of the resources.
func (qr *Resources) clone() Resources {
	var resourcesCopy Resources
	if qr.Memory != nil {
		resourcesCopy.Memory = &ResourceMemory{Limit: qr.Memory.Limit}
	}
	if qr.CPU != nil {
		resourcesCopy.CPU = &ResourceCPU{Count: qr.CPU.Count, Percentage: qr.CPU.Percentage}
	}
	if qr.CPUSet != nil {
		resourcesCopy.CPUSet = &ResourceCPUSet{CPUs: qr.CPUSet.CPUs}
	}
	if qr.Threads != nil {
		resourcesCopy.Threads = &ResourceThreads{Limit: qr.Threads.Limit}
	}
	return resourcesCopy
}

// changeInternal applies each new limit provided
func (qr *Resources) changeInternal(newLimits Resources) {
	if newLimits.Memory != nil {
		qr.Memory = newLimits.Memory
	}
	if newLimits.CPU != nil {
		qr.CPU = newLimits.CPU
	}
	if newLimits.CPUSet != nil {
		qr.CPUSet = newLimits.CPUSet
	}
	if newLimits.Threads != nil {
		qr.Threads = newLimits.Threads
	}
}

// Change updates the current quota limits with the new limits. Additional verification
// logic exists for this operation compared to when setting initial limits. Some changes
// of limits are not allowed.
func (qr *Resources) Change(newLimits Resources) error {
	if err := qr.ValidateChange(newLimits); err != nil {
		return err
	}

	// perform the changes initially on a dry-run so we can validate
	// the resulting quotas combined.
	resultingLimits := qr.clone()
	resultingLimits.changeInternal(newLimits)

	// now perform validation on the dry run
	if err := resultingLimits.Validate(); err != nil {
		return err
	}

	// if we get here, we can perform the actual changes
	qr.changeInternal(newLimits)
	return nil
}
