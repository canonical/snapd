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
		if err != state.ErrNoState {
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

// GetQuota returns an individual quota group by name.
func GetQuota(st *state.State, name string) (*quota.Group, error) {
	allGrps, err := AllQuotas(st)
	if err != nil {
		return nil, err
	}

	// if the referenced group does not exist we return a nil group
	return allGrps[name], nil
}

// UpdateQuotas will update the state quota group map with the provided quota
// groups. The groups provided will replace group states if present or be added
// on top of the current set of quota groups in the state, and verified for
// consistency before committed to state. When adding sub-groups, both the
// parent and the sub-group must be added at once since the sub-group needs to
// reference the parent group and vice versa to be fully consistent.
func UpdateQuotas(st *state.State, grps ...*quota.Group) error {
	// get the current set of quotas
	allGrps, err := AllQuotas(st)
	if err != nil {
		// AllQuotas() can't return ErrNoState, in that case it just returns a
		// nil map, which we handle below
		return err
	}
	if allGrps == nil {
		allGrps = make(map[string]*quota.Group)
	}

	// handle trivial case here to prevent panics below
	if len(grps) == 0 {
		return nil
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
		return fmt.Errorf("cannot update quota%s %s: %v", plural, updated, err)
	}

	st.Set("quotas", allGrps)
	return nil
}
