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

package resources

import (
	"fmt"

	"github.com/snapcore/snapd/gadget/quantity"
)

// QuotaResourceMemory is the memory limit for the quota group being controlled,
// either the initial limit the group is created with for the "create"
// action, or if non-zero for the "update" the memory limit, then the new
// value to be set.
type QuotaResourceMemory struct {
	MemoryLimit quantity.Size
}

type QuotaResources struct {
	Memory *QuotaResourceMemory
}

func (qr *QuotaResources) validateMemoryQuota() error {
	// make sure the memory limit is not zero
	if qr.Memory.MemoryLimit == 0 {
		return fmt.Errorf("cannot create quota group with no memory limit set")
	}

	// make sure the memory limit is at least 4K, that is the minimum size
	// to allow nesting, otherwise groups with less than 4K will trigger the
	// oom killer to be invoked when a new group is added as a sub-group to the
	// larger group.
	if qr.Memory.MemoryLimit <= 4*quantity.SizeKiB {
		return fmt.Errorf("memory limit %d is too small: size must be larger than 4KB", qr.Memory.MemoryLimit)
	}
	return nil
}

func (qr *QuotaResources) Validate() error {
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

func (qr *QuotaResources) ValidateChange(newLimits QuotaResources) error {

	// check that the memory limit is not being decreased
	if newLimits.Memory != nil && newLimits.Memory.MemoryLimit != 0 {
		// we disallow decreasing the memory limit because it is difficult to do
		// so correctly with the current state of our code in
		// EnsureSnapServices, see comment in ensureSnapServicesForGroup for
		// full details
		if qr.Memory != nil && newLimits.Memory.MemoryLimit < qr.Memory.MemoryLimit {
			return fmt.Errorf("cannot decrease memory limit, remove and re-create it to decrease the limit")
		}
	}

	return nil
}

func (qr *QuotaResources) Change(newLimits QuotaResources) error {
	if err := qr.ValidateChange(newLimits); err != nil {
		return err
	}

	if newLimits.Memory != nil {
		qr.Memory = newLimits.Memory
	}
	return nil
}

func CreateQuotaResources(memoryLimit quantity.Size) QuotaResources {
	var quotaResources QuotaResources
	if memoryLimit != 0 {
		quotaResources.Memory = &QuotaResourceMemory{
			MemoryLimit: memoryLimit,
		}
	}
	return quotaResources
}
