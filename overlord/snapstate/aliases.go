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
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
)

type AliasState struct {
	// Enabled tracks for which snap the alias has been enabled, "" if none
	Enabled string `json:"enabled,omitempty"`
	// Auto is true if the alias has been auto-enabled
	// TODO: Auto bool `json:"auto,omitempy"`
	// Disabled tracks the possibly many snap for which the alias has
	// been forcefully disabled
	// TODO: Disabled []string `json:"disabled,omitempty"`
}

func getAliases(st *state.State) (map[string]*AliasState, error) {
	var aliasStates map[string]*AliasState
	err := st.Get("aliases", &aliasStates)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	return aliasStates, nil
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
	aliasStates, err := getAliases(st)
	if err != nil {
		return err
	}
	var aliases []*backend.Alias
	for alias, aliasState := range aliasStates {
		if aliasState.Enabled == snapName {
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
		// TODO: check Disabled => remove
	}
	aliases, err = m.backend.MissingAliases(aliases)
	if err != nil {
		return fmt.Errorf("cannot establish missing enabled aliases for snap %q: %v", snapName, err)
	}
	t.Set("add", aliases)
	st.Unlock()
	defer st.Lock()
	return m.backend.UpdateAliases(aliases, nil)
}

func (m *SnapManager) undoSetupAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	var adding []*backend.Alias
	err = t.Get("add", &adding)
	if err != nil {
		return err
	}
	rmAliases, err := m.backend.MatchingAliases(adding)
	if err != nil {
		return fmt.Errorf("cannot establish matching enabled aliases for snap %q: %v", snapName, err)
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
	aliasStates, err := getAliases(st)
	if err != nil {
		return err
	}
	prefix := fmt.Sprintf("%s.", snapName)
	for alias, aliasState := range aliasStates {
		if alias == snapName || strings.HasPrefix(alias, prefix) {
			if aliasState.Enabled != "" {
				return fmt.Errorf("snap %q command namespace conflicts with enabled alias %q for %q", snapName, alias, aliasState.Enabled)
			}
		}
	}
	return nil
}
