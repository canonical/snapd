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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// AliasState describes the state of an alias in the context of a snap.
// aliases-v2 top state entry is a snapName -> alias -> AliasState map.
type AliasState struct {
	Status        string `json:"status"` // one of: auto,disabled,manual,overridden
	Target        string `json:"target"`
	AutoTargetBak string `json:"auto-target,omitempty"`
}

func (as *AliasState) Enabled() bool {
	switch as.Status {
	case "auto", "manual", "overridden":
		return true
	}
	return false
}

func (as *AliasState) Manual() bool {
	switch as.Status {
	case "manual", "overridden":
		return true
	}
	return false
}

func (as *AliasState) AutoTarget() string {
	switch as.Status {
	case "manual":
		return ""
	case "overridden":
		return as.AutoTargetBak
	default:
		return as.Target
	}
}

/*
   There are two kinds of aliases:

   * automatic aliases listed with their target application in the
     snap-declaration of the snap (states: auto,disabled,overridden)

   * manual aliases setup with "snap alias SNAP.APP ALIAS" (states:
     manual,overridden)

   Further

   * all automatic aliases of a snap are either enabled (state: auto)
     or disabled together

   * disabling a manual alias removes it from disk and state (for
     simplicity there is no disabled state for manual aliases)

   * therefore enabled automatic aliases and manual aliases never mix
     for a snap (no snap with mixed auto and manual/overridden states)

   * a snap with manual aliases can only have disabled or overridden
     automatic aliases, the latter means a manual alias overlaid on
     top of what is declared by the snap as an automatic alias

   Given that aliases are shared and can conflict between snaps and
   (because of --prefer/prefer) some operations might need to touch
   implicitly the aliases of other snaps, the execution of all tasks
   touching alias states or aliases on disk are serialized, see
   SnapManager.blockedTask
*/

// TODO: helper from snap
func composeTarget(snapName, targetApp string) string {
	if targetApp == snapName {
		return targetApp
	}
	return fmt.Sprintf("%s.%s", snapName, targetApp)
}

// applyAliasChange applies the necessary changes to aliases on disk
// to go from prevStates to newStates for the aliases of snapName. It
// assumes that conflicts have already been checked.
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

func getAliasStates(st *state.State, snapName string) (map[string]*AliasState, error) {
	var allAliasStates map[string]*json.RawMessage
	err := st.Get("aliases-v2", &allAliasStates)
	if err != nil {
		return nil, err
	}
	raw := allAliasStates[snapName]
	if raw == nil {
		return nil, state.ErrNoState
	}
	var aliasStates map[string]*AliasState
	err = json.Unmarshal([]byte(*raw), &aliasStates)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal snap %q alias states: %v", snapName, err)
	}
	return aliasStates, nil
}

// autoAliasStatesDelta compares the alias states with the current snap
// declaration for the installed snaps with the given names (or all if
// names is empty) and returns touched and retired auto-aliases by snap
// name.
func autoAliasStatesDelta(st *state.State, names []string) (touched map[string][]string, retired map[string][]string, err error) {
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
	touched = make(map[string][]string)
	retired = make(map[string][]string)
	for snapName, snapst := range snapStates {
		aliasStates, err := getAliasStates(st, snapName)
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
		for alias, target := range autoAliases {
			curState := aliasStates[alias]
			if curState == nil || curState.AutoTarget() != target {
				touched[snapName] = append(touched[snapName], alias)
			}
		}
		for alias, aliasState := range aliasStates {
			if aliasState.Status == "auto" && autoAliases[alias] == "" {
				retired[snapName] = append(retired[snapName], alias)
			}
		}
	}
	return touched, retired, firstErr
}

// refreshAliasStates applies the current snap-declaration aliases
// considering which applications exist in info and produces new alias
// states for the snap. It preserves the overall enabled or disabled
// state of automatic aliases for the snap.
func refreshAliasStates(st *state.State, info *snap.Info, curStates map[string]*AliasState) (newStates map[string]*AliasState, err error) {
	auto := true
	for _, aliasState := range curStates {
		if aliasState.Status != "auto" {
			auto = false
			break
		}
	}
	autoAliases, err := AutoAliases(st, info)
	if err != nil {
		return nil, err
	}

	newStates = make(map[string]*AliasState, len(autoAliases))
	// apply the current auto-aliases
	for alias, target := range autoAliases {
		if info.Apps[target] == nil {
			// not an existing app
			continue
		}
		status := "disabled"
		if auto {
			status = "auto"
		}
		newStates[alias] = &AliasState{Status: status, Target: target}
	}
	if auto {
		// nothing else to do
		return newStates, nil
	}
	// carry over the current manual ones
	for alias, curState := range curStates {
		if !curState.Manual() {
			continue
		}
		if info.Apps[curState.Target] == nil {
			// not an existing app
			continue
		}
		newState := newStates[alias]
		if newState == nil {
			newStates[alias] = &AliasState{Status: "manual", Target: curState.Target}
		} else {
			// alias is both manually setup but has an overlapping auto-alias
			newStates[alias] = &AliasState{Status: "overridden", Target: curState.Target, AutoTargetBak: newState.Target}
		}
	}
	return newStates, nil
}

type AliasConflictError struct {
	Snap      string
	Alias     string
	Reason    string
	Conflicts map[string][]string
}

func (e *AliasConflictError) Error() string {
	if len(e.Conflicts) != 0 {
		errParts := []string{"cannot enable"}
		first := true
		for snapName, aliases := range e.Conflicts {
			if !first {
				errParts = append(errParts, "nor")
			}
			if len(aliases) == 1 {
				errParts = append(errParts, fmt.Sprintf("alias %q", aliases[0]))
			} else {
				errParts = append(errParts, fmt.Sprintf("aliases %s", strutil.Quoted(aliases)))
			}
			if first {
				errParts = append(errParts, fmt.Sprintf("for %q,", e.Snap))
				first = false
			}
			errParts = append(errParts, fmt.Sprintf("already enabled for %q", snapName))
		}
		// TODO: add recommendation about what to do next
		return strings.Join(errParts, " ")
	}
	return fmt.Sprintf("cannot enable alias %q for %q, %s", e.Alias, e.Snap, e.Reason)
}

func checkAgainstEnabledAliasStates(st *state.State, checkedSnap string, checker func(alias, otherSnap string) error) error {
	var allAliasStates map[string]map[string]*AliasState
	err := st.Get("aliases-v2", &allAliasStates)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}
	for otherSnap, aliasStates := range allAliasStates {
		if otherSnap == checkedSnap {
			// skip
			continue
		}
		for alias, aliasState := range aliasStates {
			if aliasState.Enabled() {
				if err := checker(alias, otherSnap); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkAliasStatesConflicts checks candStates for conflicts against
// other snaps' alias states returning conflicting snaps and aliases
// for alias conflicts.
func checkAliasStatesConflicts(st *state.State, snapName string, candStates map[string]*AliasState) (conflicts map[string][]string, err error) {
	var snapNames map[string]*json.RawMessage
	err = st.Get("snaps", &snapNames)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	enabled := make(map[string]bool, len(candStates))
	for alias, candState := range candStates {
		if candState.Enabled() {
			enabled[alias] = true
		}
		namespace := alias
		if i := strings.IndexRune(alias, '.'); i != -1 {
			namespace = alias[:i]
		}
		// check against snap namespaces
		if snapNames[namespace] != nil {
			return nil, &AliasConflictError{
				Alias:  alias,
				Snap:   snapName,
				Reason: fmt.Sprintf("it conflicts with the command namespace of installed snap %q", namespace),
			}
		}
	}

	// check against enabled aliases
	conflicts = make(map[string][]string)
	err = checkAgainstEnabledAliasStates(st, snapName, func(alias, otherSnap string) error {
		if enabled[alias] {
			conflicts[otherSnap] = append(conflicts[otherSnap], alias)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(conflicts) != 0 {
		return conflicts, &AliasConflictError{Snap: snapName, Conflicts: conflicts}
	}
	return nil, nil
}
