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

package servicestatetest

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/servicestate/internal"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

// PatchQuotas will update the state quota group map with the provided quota
// groups. It exposes internal.PatchQuotas for use in tests.
func PatchQuotas(st *state.State, grps ...*quota.Group) (map[string]*quota.Group, error) {
	return internal.PatchQuotas(st, grps...)
}

func MockQuotaInState(st *state.State, quotaName string, parentName string, snaps, services []string, resourceLimits quota.Resources) error {
	allGrps := mylog.Check2(internal.AllQuotas(st))

	var parentGrp *quota.Group
	if parentName != "" {
		var ok bool
		parentGrp, ok = allGrps[parentName]
		if !ok {
			return fmt.Errorf("cannot use non-existing quota group %q as parent group for %q", parentName, quotaName)
		}
	}

	_, _ = mylog.Check3(internal.CreateQuotaInState(st, quotaName, parentGrp, snaps, services, resourceLimits, allGrps))
	return err
}
