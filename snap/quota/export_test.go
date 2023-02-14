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
	"github.com/snapcore/snapd/testutil"
)

type GroupQuotaAllocations = groupQuotaAllocations

func (grp *Group) GetCPUQuotaPercentage() int {
	return grp.getCurrentCPUAllocation()
}

func (grp *Group) SetInternalSubGroups(grps []*Group) {
	grp.subGroups = grps
}

func (grp *Group) QuotaUpdateCheck(resourceLimits Resources) error {
	return grp.validateQuotasFit(resourceLimits)
}

func (grp *Group) ValidateGroup() error {
	return grp.validate()
}

func (grp *Group) InspectInternalQuotaAllocations() map[string]*GroupQuotaAllocations {
	allQuotas := make(map[string]*GroupQuotaAllocations)
	grp.getQuotaAllocations(allQuotas)
	return allQuotas
}

func ResourcesClone(r *Resources) Resources {
	return r.clone()
}

func MockCgroupVer(mockVer int) (restore func()) {
	r := testutil.Backup(&cgroupVer)
	cgroupVer = mockVer
	return r
}

func MockCgroupVerErr(mockErr error) (restore func()) {
	r := testutil.Backup(&cgroupVerErr)
	cgroupVerErr = mockErr
	return r
}

func MockRuntimeNumCPU(mock func() int) (restore func()) {
	r := testutil.Backup(&runtimeNumCPU)
	runtimeNumCPU = mock
	return r
}
