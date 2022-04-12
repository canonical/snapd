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
	"github.com/snapcore/snapd/sandbox/cgroup"
)

var (
	cgroupVer    int
	cgroupVerErr error

	cgroupCheckMemoryCgroupErr error
)

func init() {
	cgroupVer, cgroupVerErr = cgroup.Version()
	cgroupCheckMemoryCgroupErr = cgroup.CheckMemoryCgroup()
}

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
	return nil
}

func cpuFitsIntoCPUSet(count, percentage int, cpuSet []int) error {
	if len(cpuSet) > 0 && count != 0 {
		maxCPUUsage := len(cpuSet) * 100
		cpuUsage := count * percentage
		if cpuUsage > maxCPUUsage {
			return fmt.Errorf("cpu usage %d%% is larger than the maximum allowed for provided set %v of %d%%",
				cpuUsage, cpuSet, maxCPUUsage)
		}
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

	// make sure the limit is not going above any cpu-set limit also
	// provided, note that this check is very preliminary, and we
	// cant take all circumstances into account, but we can check
	// against bad input that is obviously not allowed.
	if qr.CPUSet != nil {
		if err := cpuFitsIntoCPUSet(qr.CPU.Count, qr.CPU.Percentage, qr.CPUSet.CPUs); err != nil {
			return err
		}
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

// CheckFeatureRequirements checks if the current system meets the
// requirements for the given resource request.
//
// E.g. a CPUSet only only be set set on systems with cgroup v2.
func (qr *Resources) CheckFeatureRequirements() error {
	if qr.CPUSet != nil {
		if cgroupVerErr != nil {
			return cgroupVerErr
		}
		if cgroupVer < 2 {
			return fmt.Errorf("cannot use CPU set with cgroup version %d", cgroupVer)
		}
	}
	if qr.Memory != nil && cgroupCheckMemoryCgroupErr != nil {
		return fmt.Errorf("cannot use memory quota: %v", cgroupCheckMemoryCgroupErr)
	}

	return nil
}

// Validate performs basic validation of the provided quota resources for a group.
// The restrictions imposed are that at least one limit should be set, and each
// of the imposed limits are not zero.
//
// Note that before applying the quota to the system
// CheckFeatureRequirements() should be called.
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
// We do this validation each time a new group/subgroup is created or updated.
// This is to catch issues where we want to guard against lowering limits where not supported
// or to make sure that certain/all limits are not removed.
// We also require memory limits are above 640kB.
func (qr *Resources) ValidateChange(newLimits Resources) error {
	// Check that the memory limit is not being decreased
	if newLimits.Memory != nil {
		if newLimits.Memory.Limit == 0 {
			return fmt.Errorf("cannot remove memory limit from quota group")
		}

		// make sure the memory limit is at least 640K, that is the minimum size
		// we will require for a quota group. Newer systemd versions require up to
		// 12kB per slice, so we need to ensure 'plenty' of space for this, and also
		// 640kB seems sensible as a minimum.
		if newLimits.Memory.Limit <= 640*quantity.SizeKiB {
			return fmt.Errorf("memory limit %d is too small: size must be larger than 640KB", newLimits.Memory.Limit)
		}

		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if qr.Memory != nil && newLimits.Memory.Limit < qr.Memory.Limit {
			return fmt.Errorf("cannot decrease memory limit, remove and re-create it to decrease the limit")
		}
	}

	// Check that the cpu limit is not being removed, and we want to verify the new limit
	// is valid.
	if newLimits.CPU != nil {
		// Allow count to be changed to zero, but not percentage. This is because count
		// is an optional setting and we want to allow the user to remove the count to indicate
		// that we just want to use % of all cpus
		if qr.CPU != nil && newLimits.CPU.Percentage == 0 && qr.CPU.Percentage != 0 {
			return fmt.Errorf("cannot remove cpu limit from quota group")
		}

		// Check that the CPU percentage still fits into any pre-existing CPU set or
		// the new one if set.
		if newLimits.CPUSet != nil {
			if err := cpuFitsIntoCPUSet(newLimits.CPU.Count, newLimits.CPU.Percentage, newLimits.CPUSet.CPUs); err != nil {
				return err
			}
		} else if qr.CPUSet != nil {
			if err := cpuFitsIntoCPUSet(newLimits.CPU.Count, newLimits.CPU.Percentage, qr.CPUSet.CPUs); err != nil {
				return err
			}
		}
	}

	// Check that we are not removing the entire cpu set, or applying a CPU set that is
	// more restrictive than the current usage
	if newLimits.CPUSet != nil {
		if qr.CPUSet != nil && len(newLimits.CPUSet.CPUs) == 0 {
			return fmt.Errorf("cannot remove all allowed cpus from quota group")
		}

		// if we are applying a new CPU set and not a new CPU quota, we need to make sure
		// that the existing CPU quota fits with the new set
		if newLimits.CPU == nil && qr.CPU != nil {
			if err := cpuFitsIntoCPUSet(qr.CPU.Count, qr.CPU.Percentage, newLimits.CPUSet.CPUs); err != nil {
				return err
			}
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
