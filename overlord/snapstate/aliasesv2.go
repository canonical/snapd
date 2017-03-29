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

// AliasesStatus represents the global automatic aliases status for a snap.
type AliasesStatus string

// Possible global automatic aliases statuses for a snap.
const (
	EnabledAliases  AliasesStatus = "enabled"
	DisabledAliases AliasesStatus = "disabled"
)

// AliasTarget carries the targets of an alias in the context of snap.
// If Manual is set it is the target of an enabled manual alias.
// Auto is set to the target for an automatic alias, enabled or
// disabled depending on the aliases status of the whole snap.
type AliasTarget struct {
	Manual string `json:"manual,omitempty"`
	Auto   string `json:"auto,omitempty"`
}

// Effective returns the target to use based on the aliasesStatus of the whole snap, returns "" if the alias is disabled.
func (at *AliasTarget) Effective(aliasesStatus AliasesStatus) string {
	if at == nil {
		return ""
	}
	if at.Manual != "" {
		return at.Manual
	}
	if aliasesStatus == EnabledAliases {
		return at.Auto
	}
	return ""
}

// TODO: helper from snap
func composeTarget(snapName, targetApp string) string {
	if targetApp == snapName {
		return targetApp
	}
	return fmt.Sprintf("%s.%s", snapName, targetApp)
}

// applyAliasesChange applies the necessary changes to aliases on disk
// to go from prevAliases under the snap global prevStatus for
// automatic aliases to newAliases under newStatus for snapName.
// It assumes that conflicts have already been checked.
func applyAliasesChange(st *state.State, snapName string, prevStatus AliasesStatus, prevAliases map[string]*AliasTarget, newStatus AliasesStatus, newAliases map[string]*AliasTarget, be managerBackend) error {
	var add, remove []*backend.Alias
	for alias, prevTargets := range prevAliases {
		if _, ok := newAliases[alias]; ok {
			continue
		}
		// gone
		if effTgt := prevTargets.Effective(prevStatus); effTgt != "" {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, effTgt),
			})
		}
	}
	for alias, newTargets := range newAliases {
		prevTgt := prevAliases[alias].Effective(prevStatus)
		newTgt := newTargets.Effective(newStatus)
		if prevTgt == newTgt {
			// nothing to do
			continue
		}
		if prevTgt != "" {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, prevTgt),
			})
		}
		if newTgt != "" {
			add = append(add, &backend.Alias{
				Name:   alias,
				Target: composeTarget(snapName, newTgt),
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
