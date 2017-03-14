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

// Alias enables the provided aliases for the snap with the given name.
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
	if err := CheckChangeConflict(st, snapName, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{RealName: snapName},
	}

	alias := st.NewTask("alias", fmt.Sprintf(i18n.G("Enable aliases for snap %q"), snapsup.Name()))
	alias.Set("snap-setup", &snapsup)
	toEnable := map[string]string{}
	for _, alias := range aliases {
		toEnable[alias] = "enabled"
	}
	alias.Set("aliases", toEnable)

	return state.NewTaskSet(alias), nil
}

// Unalias explicitly disables the provided aliases for the snap with the given name.
func Unalias(st *state.State, snapName string, aliases []string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", snapName)
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("disabling aliases for disabled snap %q not supported", snapName)
	}
	if err := CheckChangeConflict(st, snapName, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{RealName: snapName},
	}

	alias := st.NewTask("alias", fmt.Sprintf(i18n.G("Disable aliases for snap %q"), snapsup.Name()))
	alias.Set("snap-setup", &snapsup)
	toDisable := map[string]string{}
	for _, alias := range aliases {
		toDisable[alias] = "disabled"
	}
	alias.Set("aliases", toDisable)

	return state.NewTaskSet(alias), nil
}

// ResetAliases resets the provided aliases for the snap with the given name to their default state, enabled for auto-aliases, disabled otherwise.
func ResetAliases(st *state.State, snapName string, aliases []string) (*state.TaskSet, error) {
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

func parseAliasState(state string) (status, targetApp string) {
	if state == "" { // default disabled status
		return "-", ""
	}
	stateComps := strings.SplitN(state, ":", 2)
	if len(stateComps) == 1 {
		return stateComps[0], ""
	}
	return stateComps[0], stateComps[1]
}

func mkAliasState(status, targetApp string) string {
	if targetApp == "" {
		return status
	}
	return status + ":" + targetApp
}

// expandAliasState expands an alias state with possibly just status to both status and target app
func expandAliasState(st *state.State, info *snap.Info, alias string, state string, fromState bool) (status, targetApp string, err error) {
	status, targetApp = parseAliasState(state)
	switch status {
	case "-":
		return "-", "", nil
	case "disabled":
		if targetApp != "" {
			return "", "", fmt.Errorf("internal error: target app is meaningless for disabled status of an alias %q: %s", alias, state)
		}
		return "disabled", "", nil
	case "enabled":
		if targetApp == "" {
			app := info.Aliases[alias]
			if app == nil {
				// during the transition, this means
				// a refresh of the snap has removed
				// the alias, there is no target,
				// nor alias on disk
				return "enabled", "", nil
			}
			targetApp = app.Name
		}
		return "enabled", targetApp, nil
	case "auto":
		if fromState {
			// this is from a state entry
			if targetApp == "" {
				// no target, old style entry
				app := info.Aliases[alias]
				if app == nil {
					// during the transition, this means
					// a refresh of the snap has removed
					// the alias, there is no target,
					// nor alias on disk
					return "auto", "", nil
				}
				targetApp = app.Name
			}
			return "auto", targetApp, nil
		}
		autoAliases, err := AutoAliases(st, info)
		if err != nil {
			return "", "", err
		}
		targetApp = autoAliases[alias]
		if info.Apps[targetApp] == nil {
			return "-", "", nil // default disabled status, not stored!
		}
		return "auto", targetApp, nil
	default:
		return "", "", fmt.Errorf("internal error: unknown status %q for an alias %q: %s", status, alias, state)
	}
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
	aliasStates, err := getAliases(st, snapName)
	if err != nil && err != state.ErrNoState {
		return err
	}
	t.Set("old-aliases", aliasStates)
	if aliasStates == nil {
		aliasStates = make(map[string]string)
	}
	var add []*backend.Alias
	var remove []*backend.Alias
	for alias, newState := range changes {
		oldState := aliasStates[alias]
		oldStatus, oldApp, err := expandAliasState(st, curInfo, alias, oldState, true)
		if err != nil {
			return err
		}
		newStatus, newApp, err := expandAliasState(st, curInfo, alias, newState, false)
		if err != nil {
			return err
		}

		newState = mkAliasState(newStatus, newApp)
		if oldStatus == newStatus && oldApp == newApp {
			if newApp != "" && newState != oldState {
				// record target
				aliasStates[alias] = newState
			}
			// nothing to do
			continue
		}

		aliasApp := curInfo.Apps[newApp]
		if aliasApp == nil && newStatus == "enabled" {
			return fmt.Errorf("cannot enable alias %q targeting %q for %q, no such application", alias, newApp, snapName)
		}
		oldAliasApp := curInfo.Apps[oldApp]
		// XXX: what to check/do for disabled in the new world?

		if newStatus != "-" {
			aliasStates[alias] = newState
		} else {
			delete(aliasStates, alias)
		}

		if oldApp == newApp {
			// no disk changes needed
			continue
		}

		if oldAliasApp != nil {
			beAlias := &backend.Alias{
				Name:   alias,
				Target: filepath.Base(oldAliasApp.WrapperPath()),
			}

			remove = append(remove, beAlias)
		}
		if aliasApp != nil {
			beAlias := &backend.Alias{
				Name:   alias,
				Target: filepath.Base(aliasApp.WrapperPath()),
			}

			err := checkAliasConflict(st, snapName, alias)
			if err != nil {
				return err
			}
			add = append(add, beAlias)
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
	setAliases(st, snapName, aliasStates)
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
			// XXX quick hack to still pass unit tests
			curStatus, _ := parseAliasState(aliasStatuses[alias])
			if curStatus != "auto" {
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
	// TODO: deal with conversion from "enabled" to "enabled:<target>" state keeping
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
	// TODO: deal with auto aliases changing target and with
	// conversion from "auto" to "auto:<target>" state keeping
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
