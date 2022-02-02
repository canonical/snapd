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

package servicestate

import (
	"errors"

	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

var ErrQuotaNotFound = errors.New("quota not found")

func verifyRequiredCGroupsEnabledSingle(group *quota.Group) error {
	if group.MemoryLimit != 0 && memoryCGroupError != nil {
		return memoryCGroupError
	}
	if group.CpuLimit != nil {
		if len(group.CpuLimit.AllowedCpus) > 0 && cpuSetCGroupError != nil {
			return cpuSetCGroupError
		}
		if (group.CpuLimit.Count != 0 || group.CpuLimit.Percentage != 0) && cpuCGroupError != nil {
			return cpuCGroupError
		}
	}
	if group.TaskLimit != 0 && cpuCGroupError != nil {
		return cpuCGroupError
	}
	return nil
}

func verifyRequiredCGroupsEnabled(groups map[string]*quota.Group) error {
	for _, group := range groups {
		if err := verifyRequiredCGroupsEnabledSingle(group); err != nil {
			return err
		}
	}
	return nil
}

// AllQuotas returns all currently tracked quota groups in the state. They are
// validated for consistency using ResolveCrossReferences before being returned.
func AllQuotas(st *state.State) (map[string]*quota.Group, error) {
	return internal.AllQuotas(st)
}

// GetQuota returns an individual quota group by name.
func GetQuota(st *state.State, name string) (*quota.Group, error) {
	allGrps, err := internal.AllQuotas(st)
	if err != nil {
		return nil, err
	}

	group, ok := allGrps[name]
	if !ok {
		return nil, ErrQuotaNotFound
	}

	if err := verifyRequiredCGroupsEnabledSingle(group); err != nil {
		return nil, err
	}
	return group, nil
}
