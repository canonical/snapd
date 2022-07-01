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

package internal

import (
	"errors"
	"fmt"
	"sort"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap/quota"
)

// AllQuotas returns all currently tracked quota groups in the state. They are
// validated for consistency using ResolveCrossReferences before being returned.
func AllQuotas(st *state.State) (map[string]*quota.Group, error) {
	var quotas map[string]*quota.Group
	if err := st.Get("quotas", &quotas); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return nil, err
		}
		// otherwise there are no quotas so just return nil
		return nil, nil
	}

	// quota groups are not serialized with all the necessary tracking
	// information in the objects, so we need to thread some things around
	if err := quota.ResolveCrossReferences(quotas); err != nil {
		return nil, err
	}

	// quotas has now been properly initialized with unexported cross-references
	return quotas, nil
}

// PatchQuotas will update the state quota group map with the provided quota
// groups. It returns the full set of all quota groups after a successful
// update for convenience. The groups provided will replace group states if
// present or be added on top of the current set of quota groups in the state,
// and verified for consistency before committed to state. When adding
// sub-groups, both the parent and the sub-group must be added at once since the
// sub-group needs to reference the parent group and vice versa to be fully
// consistent.
func PatchQuotas(st *state.State, grps ...*quota.Group) (map[string]*quota.Group, error) {
	// get the current set of quotas
	allGrps, err := AllQuotas(st)
	if err != nil {
		// AllQuotas() can't return ErrNoState, in that case it just returns a
		// nil map, which we handle below
		return nil, err
	}
	if allGrps == nil {
		allGrps = make(map[string]*quota.Group)
	}

	// handle trivial case here to prevent panics below
	if len(grps) == 0 {
		return allGrps, nil
	}

	sort.SliceStable(grps, func(i, j int) bool {
		return grps[i].Name < grps[j].Name
	})

	// add to the temporary state map
	for _, grp := range grps {
		allGrps[grp.Name] = grp
	}

	// make sure the full set is still resolved before saving it - this prevents
	// easy errors like trying to add a sub-group quota without updating the
	// parent with references to the sub-group, for cases like those, all
	// related groups must be updated at the same time in one operation to
	// prevent having inconsistent quota groups in state.json
	if err := quota.ResolveCrossReferences(allGrps); err != nil {
		// make a nice error message for this case
		updated := ""
		for _, grp := range grps[:len(grps)-1] {
			updated += fmt.Sprintf("%q, ", grp.Name)
		}
		updated += fmt.Sprintf("%q", grps[len(grps)-1].Name)
		plural := ""
		if len(grps) > 1 {
			plural = "s"
		}
		return nil, fmt.Errorf("cannot update quota%s %s: %v", plural, updated, err)
	}

	st.Set("quotas", allGrps)
	return allGrps, nil
}

// CreateQuotaInState creates a quota group with the given paremeters
// in the state.  It takes the current map of all quota groups.
func CreateQuotaInState(st *state.State, quotaName string, parentGrp *quota.Group, snaps []string, resourceLimits quota.Resources, allGrps map[string]*quota.Group) (*quota.Group, map[string]*quota.Group, error) {
	// make sure that the parent group exists if we are creating a sub-group
	var grp *quota.Group
	var err error
	updatedGrps := []*quota.Group{}
	if parentGrp != nil {
		grp, err = parentGrp.NewSubGroup(quotaName, resourceLimits)
		if err != nil {
			return nil, nil, err
		}

		updatedGrps = append(updatedGrps, parentGrp)
	} else {
		// make a new group
		grp, err = quota.NewGroup(quotaName, resourceLimits)
		if err != nil {
			return nil, nil, err
		}
	}
	updatedGrps = append(updatedGrps, grp)

	// put the snaps in the group
	grp.Snaps = snaps
	// update the modified groups in state
	newAllGrps, err := PatchQuotas(st, updatedGrps...)
	if err != nil {
		return nil, nil, err
	}

	return grp, newAllGrps, nil
}
