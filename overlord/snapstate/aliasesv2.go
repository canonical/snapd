// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package snapstate

import (
	"fmt"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
)

// AliasState describes the state of an alias in the context of a snap.
// aliases-v2 top state entry is a snapName -> alias -> AliasState map.
type AliasState struct {
	Status string `json:"status"` // one of: auto,disabled,manual,overridden
	Target string `json:"target"`
}

func (as *AliasState) Enabled() bool {
	switch as.Status {
	case "auto", "manual", "overridden":
		return true
	}
	return false
}

// TODO: helper from snap
func composeTarget(snapName, targetApp string) string {
	if targetApp == snapName {
		return targetApp
	}
	return fmt.Sprintf("%s.%s", snapName, targetApp)
}

// applyAliasChange applies the necessary changes to aliases on disk to go from prevStates to newStates for the aliases of snapName. It assumes that conflicts have already been checked.
func applyAliasChange(st *state.State, snapName string, prevStates map[string]*AliasState, newStates map[string]*AliasState, be managerBackend) error {
	var add, remove []*backend.Alias
	for alias, prevState := range prevStates {
		_, ok := newStates[alias]
		if ok {
			continue
		}
		// gone
		if prevState.Enabled() {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, prevState.Target),
			})
		}
	}
	for alias, newState := range newStates {
		prevState := prevStates[alias]
		if prevState == nil {
			prevState = &AliasState{Status: "-"}
		}
		if prevState.Enabled() == newState.Enabled() && (!newState.Enabled() || prevState.Target == newState.Target) {
			// nothing to do
			continue
		}
		if prevState.Enabled() {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, prevState.Target),
			})
		}
		if newState.Enabled() {
			add = append(add, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, newState.Target),
			})
		}
	}
	st.Unlock()
	err := be.UpdateAliases(add, remove)
	st.Lock()
	if err != nil {
		return err
	}
	return nil
}
