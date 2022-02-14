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
	Count       int   `json:"count"`
	Percentage  int   `json:"percentage"`
	AllowedCPUs []int `json:"allowed-cpus"`
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

func (qr *Resources) validateCpuQuota() error {
	// if cpu count is non-zero, then percentage should be set
	if qr.CPU.Count != 0 && qr.CPU.Percentage == 0 {
		return fmt.Errorf("cannot validate quota limits with count of >0 and percentage of 0")
	}

	// atleast one cpu limit value must be set
	if qr.CPU.Count == 0 && qr.CPU.Percentage == 0 && len(qr.CPU.AllowedCPUs) == 0 {
		return fmt.Errorf("cannot validate quota limits with a cpu quota of 0 and allowed cpus of 0")
	}
	return nil
}

func (qr *Resources) validateThreadQuota() error {
	// make sure the thread count is not zero
	if qr.Threads.Limit == 0 {
		return fmt.Errorf("cannot create quota group with a thread count of 0")
	}
	return nil
}

// Validate performs validation of the provided quota resources for a group.
// The restrictions imposed are that atleast one limit should be set.
// If memory limit is provided, it must be above 4KB.
// If cpu percentage is provided, it must be between 1 and 100.
// If thread count is provided, it must be above 0.
func (qr *Resources) Validate() error {
	if qr.Memory == nil && qr.CPU == nil && qr.Threads == nil {
		return fmt.Errorf("quota group must have at least one resource limit set")
	}

	if qr.Memory != nil {
		if err := qr.validateMemoryQuota(); err != nil {
			return err
		}
	}

	if qr.CPU != nil {
		if err := qr.validateCpuQuota(); err != nil {
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

	// Check that the memory limit is not being decreased, but we allow it to be removed
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

	return nil
}

// Change updates the current quota limits with the new limits. Additional verification
// logic exists for this operation compared to when setting initial limits. Some changes
// of limits are not allowed.
func (qr *Resources) Change(newLimits Resources) error {
	if err := qr.ValidateChange(newLimits); err != nil {
		return err
	}

	if newLimits.Memory != nil {
		qr.Memory = newLimits.Memory
	}
	if newLimits.CPU != nil {
		if qr.CPU == nil {
			qr.CPU = newLimits.CPU
		} else {
			// update count/percentage as one unit
			if newLimits.CPU.Count != 0 || newLimits.CPU.Percentage != 0 {
				qr.CPU.Count = newLimits.CPU.Count
				qr.CPU.Percentage = newLimits.CPU.Percentage
			}

			// update allowed cpus as one unit
			if len(newLimits.CPU.AllowedCPUs) != 0 {
				qr.CPU.AllowedCPUs = newLimits.CPU.AllowedCPUs
			}
		}
	}
	if newLimits.Threads != nil {
		qr.Threads = newLimits.Threads
	}
	return nil
}
