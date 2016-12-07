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

	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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

// Alias enables the aliases for snap
func Alias(st *state.State, snapName string, aliases []string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", snapName)
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("enabling aliases for disabled snap %q not supported", snapName)
	}
	if err := checkChangeConflict(st, snapName, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{RealName: snapName},
		Aliases:  aliases,
	}

	toggleAliases := st.NewTask("toggle-aliases", fmt.Sprintf(i18n.G("Toggle aliases for snap %q"), snapsup.Name()))
	toggleAliases.Set("snap-setup", &snapsup)

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.Name()))
	setupAliases.Set("snap-setup", &snapsup)
	setupAliases.WaitFor(toggleAliases)

	return state.NewTaskSet(toggleAliases, setupAliases), nil
}

func (m *SnapManager) doToggleAliases(t *state.Task, _ *tomb.Tomb) error {
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
	if aliasStates == nil {
		aliasStates = make(map[string]*AliasState)
	}
	changes := make(map[string]string)
	for _, alias := range snapsup.Aliases {
		if curInfo.Aliases[alias] == nil {
			return fmt.Errorf("cannot enable alias %q for %q, no such alias", alias, snapName)
		}
		aliasState := aliasStates.get(alias)
		if aliasState.Enabled == "" {
			if err := checkAliasConflict(st, snapName, alias); err != nil {
				return err
			}
			aliasState.Enabled = snapName
			// TODO: remove from Disabled if it's there etc
			aliasStates.set(alias, &aliasState)
			changes[alias] = ">enabled"
		} else if aliasState.Enabled == snapName {
			// nothing to do
		} else {
			return fmt.Errorf("cannot enable alias %q for %q, already enabled for %q", alias, snapName, aliasState.Enabled)
		}
	}
	if len(changes) != 0 {
		t.Set("changes", changes)
		st.Set("aliases", aliasStates)
	}
	return nil
}

func checkAliasConflict(st *state.State, snapName, alias string) error {
	var snapNames map[string]*json.RawMessage
	err := st.Get("snaps", &snapNames)
	if err != nil && err != state.ErrNoState {
		return err
	}
	for name := range snapNames {
		if name == alias || strings.HasPrefix(alias, name+".") {
			return fmt.Errorf("cannot enable alias %q for %q, it conflicts with the command namespace of installed snap %q", alias, snapName, name)
		}
	}
	return nil
}

func (m *SnapManager) undoToggleAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	var changes map[string]string
	err := t.Get("changes", &changes)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if len(changes) == 0 {
		// nothing to undo
		return nil
	}
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	aliasStates, err := getAliases(st)
	if err != nil {
		return err
	}
	if aliasStates == nil {
		aliasStates = make(map[string]*AliasState)
	}
	for alias, change := range changes {
		aliasState := aliasStates.get(alias)
		switch change {
		case ">enabled":
			if aliasState.Enabled == snapName {
				aliasState.Enabled = ""
				aliasStates.set(alias, &aliasState)
			}
		case "enabled>":
			if aliasState.Enabled == "" {
				aliasState.Enabled = snapName
				aliasStates.set(alias, &aliasState)
			}
		default:
			return fmt.Errorf("internal error: unknown alias %q state change %q for %q", alias, change, snapName)
		}
	}
	st.Set("aliases", aliasStates)
	return nil
}

func (m *SnapManager) doClearAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	aliasStates, err := getAliases(st)
	if err != nil {
		return err
	}
	if len(aliasStates) == 0 {
		// nothing to do
		return nil
	}
	changes := make(map[string]string)
	for alias, aliasState := range aliasStates {
		if aliasState.Enabled == snapName {
			aliasState.Enabled = ""
			changes[alias] = "enabled>"
			aliasStates.set(alias, aliasState)
		}
	}
	if len(changes) != 0 {
		t.Set("changes", changes)
		st.Set("aliases", aliasStates)
	}
	return nil
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
		// TODO: check Disabled => remove
	}
	aliases, err = m.backend.MissingAliases(aliases)
	// TODO: check and error on mismatching (instead of absent) aliases?
	if err != nil {
		return fmt.Errorf("cannot list aliases for snap %q: %v", snapName, err)
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
