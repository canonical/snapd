// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/state"
)

func getAliases(st *state.State, snapName string) (map[string]string, error) {
	var allAliases map[string]*json.RawMessage
	err := st.Get("aliases", &allAliases)
	if err != nil {
		return nil, err
	}
	raw := allAliases[snapName]
	if raw == nil {
		return nil, state.ErrNoState
	}
	var aliases map[string]string
	err = json.Unmarshal([]byte(*raw), &aliases)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal snap aliases state: %v", err)
	}
	return aliases, nil
}

// TODO: reintroduce Alias, Unalias following the new meanings

// Aliases returns a map snap -> alias -> status covering all installed snaps.
func Aliases(st *state.State) (map[string]map[string]string, error) {
	var snapNames map[string]*json.RawMessage
	err := st.Get("snaps", &snapNames)
	if err == state.ErrNoState {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var res map[string]map[string]string
	for snapName := range snapNames {
		aliasStatuses, err := getAliases(st, snapName)
		if err != nil && err != state.ErrNoState {
			return nil, err
		}
		if len(aliasStatuses) != 0 {
			if res == nil {
				res = make(map[string]map[string]string)
			}
			res[snapName] = aliasStatuses
		}
	}
	return res, nil
}
