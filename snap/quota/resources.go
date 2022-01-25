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

// ResourceMemory is the memory limit for a quota group.
type ResourceMemory struct {
	Limit quantity.Size `json:"limit"`
}

// Resources is built up of multiple quota limits. Each quota limit is a pointer
// value to indicate that their presence may be optional, and because we want to detect
// whenever someone changes a limit to '0' explicitly.
type Resources struct {
	Memory *ResourceMemory `json:"memory,omitempty"`
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

// Validate performs validation of the provided quota resources for a group.
// The restrictions imposed are that atleast one limit should exist (memory for now),
// and the memory limit must be above 4KB.
func (qr *Resources) Validate() error {
	if qr.Memory == nil {
		return fmt.Errorf("quota group must have a memory limit set")
	}

	if qr.Memory != nil {
		if err := qr.validateMemoryQuota(); err != nil {
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
	return nil
}

func NewResources(memoryLimit quantity.Size) Resources {
	var quotaResources Resources
	if memoryLimit != 0 {
		quotaResources.Memory = &ResourceMemory{
			Limit: memoryLimit,
		}
	}
	return quotaResources
}
