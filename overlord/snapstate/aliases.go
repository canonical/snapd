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
	"path/filepath"
	"strings"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
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

func (m *SnapManager) doSetupAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	aliasStatuses, err := getAliases(st, snapName)
	if err != nil && err != state.ErrNoState {
		return err
	}
	var aliases []*backend.Alias
	for alias, aliasStatus := range aliasStatuses {
		if aliasStatus == "enabled" {
			aliasApp := curInfo.Aliases[alias]
			if aliasApp == nil {
				// not a known alias anymore, skip
				continue
			}
			aliases = append(aliases, &backend.Alias{
				Name:   alias,
				Target: filepath.Base(aliasApp.WrapperPath()),
			})
		}
	}
	st.Unlock()
	defer st.Lock()
	return m.backend.UpdateAliases(aliases, nil)
}

func (m *SnapManager) undoSetupAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	aliasStatuses, err := getAliases(st, snapName)
	if err != nil && err != state.ErrNoState {
		return err
	}
	var aliases []*backend.Alias
	for alias, aliasStatus := range aliasStatuses {
		if aliasStatus == "enabled" {
			aliasApp := curInfo.Aliases[alias]
			if aliasApp == nil {
				// not a known alias, skip
				continue
			}
			aliases = append(aliases, &backend.Alias{
				Name:   alias,
				Target: filepath.Base(aliasApp.WrapperPath()),
			})
		}
	}
	st.Unlock()
	rmAliases, err := m.backend.MatchingAliases(aliases)
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot list aliases for snap %q: %v", snapName, err)
	}
	st.Unlock()
	defer st.Lock()
	return m.backend.UpdateAliases(nil, rmAliases)
}

func (m *SnapManager) doRemoveAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	st.Unlock()
	defer st.Lock()
	return m.backend.RemoveSnapAliases(snapName)
}

func checkSnapAliasConflict(st *state.State, snapName string) error {
	var allAliases map[string]map[string]string
	err := st.Get("aliases", &allAliases)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s.", snapName)
	for otherSnap, aliasStatuses := range allAliases {
		for alias, aliasStatus := range aliasStatuses {
			if aliasStatus == "enabled" {
				if alias == snapName || strings.HasPrefix(alias, prefix) {
					return fmt.Errorf("snap %q command namespace conflicts with enabled alias %q for %q", snapName, alias, otherSnap)
				}
			}
		}
	}
	return nil
}
