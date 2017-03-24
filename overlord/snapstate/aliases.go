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

func setAliases(st *state.State, snapName string, aliases map[string]string) {
	var allAliases map[string]*json.RawMessage
	err := st.Get("aliases", &allAliases)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal snap aliases state: " + err.Error())
	}
	if allAliases == nil {
		allAliases = make(map[string]*json.RawMessage)
	}
	if len(aliases) == 0 {
		delete(allAliases, snapName)
	} else {
		data, err := json.Marshal(aliases)
		if err != nil {
			panic("internal error: cannot marshal snap aliases state: " + err.Error())
		}
		raw := json.RawMessage(data)
		allAliases[snapName] = &raw
	}
	st.Set("aliases", allAliases)
}

// TODO: reintroduce Alias, Unalias following the new meanings

func resetAliases(st *state.State, snapName string, aliases []string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", snapName)
	}
	if err != nil {
		return nil, err
	}

	if err := CheckChangeConflict(st, snapName, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{RealName: snapName},
	}

	alias := st.NewTask("alias", fmt.Sprintf(i18n.G("Reset aliases for snap %q"), snapsup.Name()))
	alias.Set("snap-setup", &snapsup)
	toReset := map[string]string{}
	for _, alias := range aliases {
		toReset[alias] = "auto"
	}
	alias.Set("aliases", toReset)

	return state.NewTaskSet(alias), nil
}

// enabledAlias returns true if status is one of the enabled alias statuses.
func enabledAlias(status string) bool {
	return status == "enabled" || status == "auto"
}

func (m *SnapManager) doAlias(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var changes map[string]string
	err = t.Get("aliases", &changes)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()
	curInfo, err := snapst.CurrentInfo()
	if err != nil {
		return err
	}
	autoAliases, err := AutoAliases(st, curInfo)
	if err != nil {
		return err
	}
	autoSet := make(map[string]bool, len(autoAliases))
	for alias := range autoAliases {
		autoSet[alias] = true
	}

	aliasStatuses, err := getAliases(st, snapName)
	if err != nil && err != state.ErrNoState {
		return err
	}
	t.Set("old-aliases", aliasStatuses)
	if aliasStatuses == nil {
		aliasStatuses = make(map[string]string)
	}
	var add []*backend.Alias
	var remove []*backend.Alias
	for alias, newStatus := range changes {
		if newStatus != "auto" && aliasStatuses[alias] == newStatus {
			// nothing to do
			continue
		}
		aliasApp := curInfo.Aliases[alias]
		if aliasApp == nil {
			if newStatus == "auto" {
				// reset to default disabled status
				delete(aliasStatuses, alias)
				continue
			}
			var action string
			switch newStatus {
			case "enabled":
				action = "enable"
			case "disabled":
				action = "disable"
			}
			return fmt.Errorf("cannot %s alias %q for %q, no such alias", action, alias, snapName)
		}
		beAlias := &backend.Alias{
			Name:   alias,
			Target: filepath.Base(aliasApp.WrapperPath()),
		}

		if newStatus == "auto" {
			if !autoSet[alias] {
				newStatus = "-" // default disabled status, not stored!
			}
		}
		switch newStatus {
		case "enabled", "auto":
			if !enabledAlias(aliasStatuses[alias]) {
				err := checkAliasConflict(st, snapName, alias)
				if err != nil {
					return err
				}
				add = append(add, beAlias)
			}
		case "disabled", "-":
			if enabledAlias(aliasStatuses[alias]) {
				remove = append(remove, beAlias)
			}
		}
		if newStatus != "-" {
			aliasStatuses[alias] = newStatus
		} else {
			delete(aliasStatuses, alias)
		}
	}
	if snapst.Active {
		st.Unlock()
		err = m.backend.UpdateAliases(add, remove)
		st.Lock()
		if err != nil {
			return err
		}
	}
	setAliases(st, snapName, aliasStatuses)
	return nil
}

func (m *SnapManager) undoAlias(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	var oldStatuses map[string]string
	err := t.Get("old-aliases", &oldStatuses)
	if err != nil {
		return err
	}
	snapsup, snapst, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	var changes map[string]string
	err = t.Get("aliases", &changes)
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
	var add []*backend.Alias
	var remove []*backend.Alias
Next:
	for alias, newStatus := range changes {
		if newStatus != "auto" && oldStatuses[alias] == newStatus {
			// nothing to undo
			continue
		}
		aliasApp := curInfo.Aliases[alias]
		if aliasApp == nil {
			if newStatus == "auto" {
				// nothing to undo
				continue
			}
			// unexpected
			return fmt.Errorf("internal error: cannot re-toggle alias %q for %q, no such alias", alias, snapName)
		}
		beAlias := &backend.Alias{
			Name:   alias,
			Target: filepath.Base(aliasApp.WrapperPath()),
		}

		if newStatus == "auto" {
			if aliasStatuses[alias] != "auto" {
				newStatus = "-" // default disabled status
			}
		}
		switch newStatus {
		case "enabled", "auto":
			if !enabledAlias(oldStatuses[alias]) {
				remove = append(remove, beAlias)

			}
		case "disabled", "-":
			if enabledAlias(oldStatuses[alias]) {
				// can actually be reinstated only if it doesn't conflict
				err := checkAliasConflict(st, snapName, alias)
				if err != nil {
					if _, ok := err.(*aliasConflictError); ok {
						// TODO mark the conflict if it was auto?
						delete(oldStatuses, alias)
						t.Errorf("%v", err)
						continue Next
					}
					return err
				}
				add = append(add, beAlias)
			}
		}
	}
	if snapst.Active {
		st.Unlock()
		remove, err = m.backend.MatchingAliases(remove)
		st.Lock()
		if err != nil {
			return fmt.Errorf("cannot list aliases for snap %q: %v", snapName, err)
		}
		st.Unlock()
		add, err = m.backend.MissingAliases(add)
		st.Lock()
		if err != nil {
			return fmt.Errorf("cannot list aliases for snap %q: %v", snapName, err)
		}
		st.Unlock()
		err = m.backend.UpdateAliases(add, remove)
		st.Lock()
		if err != nil {
			return err
		}
	}
	setAliases(st, snapName, oldStatuses)
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
	aliasStatuses, err := getAliases(st, snapName)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if len(aliasStatuses) == 0 {
		// nothing to do
		return nil
	}
	t.Set("old-aliases", aliasStatuses)
	setAliases(st, snapName, nil)
	return nil
}

func (m *SnapManager) undoClearAliases(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()
	var oldStatuses map[string]string
	err := t.Get("old-aliases", &oldStatuses)
	if err == state.ErrNoState {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}
	snapsup, _, err := snapSetupAndState(t)
	if err != nil {
		return err
	}
	snapName := snapsup.Name()

	for alias, status := range oldStatuses {
		if enabledAlias(status) {
			// can actually be reinstated only if it doesn't conflict
			err := checkAliasConflict(st, snapName, alias)
			if err != nil {
				if _, ok := err.(*aliasConflictError); ok {
					// TODO mark the conflict if it was auto?
					delete(oldStatuses, alias)
					t.Errorf("%v", err)
					continue
				}
				return err
			}
		}
	}
	setAliases(st, snapName, oldStatuses)
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
		if enabledAlias(aliasStatus) {
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
		if enabledAlias(aliasStatus) {
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

func checkAgainstEnabledAliases(st *state.State, checker func(alias, otherSnap string) error) error {
	var allAliases map[string]map[string]string
	err := st.Get("aliases", &allAliases)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	for otherSnap, aliasStatuses := range allAliases {
		for alias, aliasStatus := range aliasStatuses {
			if enabledAlias(aliasStatus) {
				if err := checker(alias, otherSnap); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkSnapAliasConflict(st *state.State, snapName string) error {
	prefix := fmt.Sprintf("%s.", snapName)
	return checkAgainstEnabledAliases(st, func(alias, otherSnap string) error {
		if alias == snapName || strings.HasPrefix(alias, prefix) {
			return fmt.Errorf("snap %q command namespace conflicts with enabled alias %q for %q", snapName, alias, otherSnap)
		}
		return nil
	})
}

type aliasConflictError struct {
	Alias  string
	Snap   string
	Reason string
}

func (e *aliasConflictError) Error() string {
	return fmt.Sprintf("cannot enable alias %q for %q, %s", e.Alias, e.Snap, e.Reason)
}

func checkAliasConflict(st *state.State, snapName, alias string) error {
	// check against snaps
	var snapNames map[string]*json.RawMessage
	err := st.Get("snaps", &snapNames)
	if err != nil && err != state.ErrNoState {
		return err
	}
	for name := range snapNames {
		if name == alias || strings.HasPrefix(alias, name+".") {
			return &aliasConflictError{
				Alias:  alias,
				Snap:   snapName,
				Reason: fmt.Sprintf("it conflicts with the command namespace of installed snap %q", name),
			}
		}
	}

	// check against aliases
	return checkAgainstEnabledAliases(st, func(otherAlias, otherSnap string) error {
		if otherAlias == alias && otherSnap != snapName {
			return &aliasConflictError{
				Alias:  alias,
				Snap:   snapName,
				Reason: fmt.Sprintf("already enabled for %q", otherSnap),
			}
		}
		return nil
	})
}

// AutoAliases allows to hook support for retrieving auto-aliases of a snap.
var AutoAliases func(st *state.State, info *snap.Info) (map[string]string, error)

// AutoAliasesDelta compares the alias statuses with the current snap
// declaration for the installed snaps with the given names (or all if
// names is empty) and returns new and retired auto-aliases by snap
// name. It accounts for already set enabled/disabled statuses but
// does not check for conflicts.
func AutoAliasesDelta(st *state.State, names []string) (new map[string][]string, retired map[string][]string, err error) {
	var snapStates map[string]*SnapState
	if len(names) == 0 {
		var err error
		snapStates, err = All(st)
		if err != nil {
			return nil, nil, err
		}
	} else {
		snapStates = make(map[string]*SnapState, len(names))
		for _, name := range names {
			var snapst SnapState
			err := Get(st, name, &snapst)
			if err != nil {
				return nil, nil, err
			}
			snapStates[name] = &snapst
		}
	}
	var firstErr error
	new = make(map[string][]string)
	retired = make(map[string][]string)
	for snapName, snapst := range snapStates {
		aliasStatuses, err := getAliases(st, snapName)
		if err != nil && err != state.ErrNoState {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		info, err := snapst.CurrentInfo()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		autoAliases, err := AutoAliases(st, info)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		autoSet := make(map[string]bool, len(autoAliases))
		for alias := range autoAliases {
			autoSet[alias] = true
			if aliasStatuses[alias] == "" { // not auto, or disabled, or enabled
				new[snapName] = append(new[snapName], alias)
			}
		}
		for alias, curStatus := range aliasStatuses {
			if curStatus == "auto" && !autoSet[alias] {
				retired[snapName] = append(retired[snapName], alias)
			}
		}
	}
	return new, retired, firstErr
}

func (m *SnapManager) doSetAutoAliases(t *state.Task, _ *tomb.Tomb) error {
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
	t.Set("old-aliases", aliasStatuses)
	if aliasStatuses == nil {
		aliasStatuses = make(map[string]string)
	}
	allNew, allRetired, err := AutoAliasesDelta(st, []string{snapName})
	if err != nil {
		return err
	}
	for _, alias := range allRetired[snapName] {
		delete(aliasStatuses, alias)
	}

	for _, alias := range allNew[snapName] {
		aliasApp := curInfo.Aliases[alias]
		if aliasApp == nil {
			// not a known alias anymore or yet, skip
			continue
		}
		// TODO: only mark/log conflict if this is an update instead of an install?
		err := checkAliasConflict(st, snapName, alias)
		if err != nil {
			return err
		}
		aliasStatuses[alias] = "auto"
	}
	setAliases(st, snapName, aliasStatuses)
	return nil
}

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
