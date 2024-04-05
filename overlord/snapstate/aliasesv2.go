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
	"errors"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// AliasTarget carries the targets of an alias in the context of snap.
// If Manual is set it is the target of an enabled manual alias.
// Auto is set to the target for an automatic alias, enabled or
// disabled depending on the automatic aliases flag state.
type AliasTarget struct {
	Manual string `json:"manual,omitempty"`
	Auto   string `json:"auto,omitempty"`
}

// Effective returns the target to use considering whether automatic
// aliases are disabled for the whole snap (autoDisabled), returns ""
// if the alias is disabled.
func (at *AliasTarget) Effective(autoDisabled bool) string {
	if at == nil {
		return ""
	}
	if at.Manual != "" {
		return at.Manual
	}
	if !autoDisabled {
		return at.Auto
	}
	return ""
}

/*
   State for aliases for a snap is tracked in SnapState with:

	type SnapState struct {
                ...
		Aliases              map[string]*AliasTarget
		AutoAliasesDisabled  bool
	}

   There are two kinds of aliases:

   * automatic aliases listed with their target application in the
     snap-declaration of the snap (using AliasTarget.Auto)

   * manual aliases setup with "snap alias SNAP.APP ALIAS" (tracked
     using AliasTarget.Manual)

   Further

   * all automatic aliases of a snap are either enabled
     or disabled together (tracked with AutoAliasesDisabled)

   * disabling a manual alias removes it from disk and state (for
     simplicity there is no disabled state for manual aliases)

   * an AliasTarget with both Auto and Manual set is a manual alias
     that has the same name as an automatic one, the manual target
     is what wins

*/

// autoDisabled options and doApply
const (
	autoDis = true
	autoEn  = false

	doApply = false
)

// applyAliasesChange applies the necessary changes to aliases on disk
// to go from prevAliases considering the automatic aliases flag
// (prevAutoDisabled) to newAliases considering newAutoDisabled for
// snapName. It assumes that conflicts have already been checked.
func applyAliasesChange(snapName string, prevAutoDisabled bool, prevAliases map[string]*AliasTarget, newAutoDisabled bool, newAliases map[string]*AliasTarget, be managerBackend, dryRun bool) (add, remove []*backend.Alias, err error) {
	for alias, prevTargets := range prevAliases {
		if _, ok := newAliases[alias]; ok {
			continue
		}
		// gone
		if effTgt := prevTargets.Effective(prevAutoDisabled); effTgt != "" {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: snap.JoinSnapApp(snapName, effTgt),
			})
		}
	}
	for alias, newTargets := range newAliases {
		prevTgt := prevAliases[alias].Effective(prevAutoDisabled)
		newTgt := newTargets.Effective(newAutoDisabled)
		if prevTgt == newTgt {
			// nothing to do
			continue
		}
		if prevTgt != "" {
			remove = append(remove, &backend.Alias{
				Name:   alias,
				Target: snap.JoinSnapApp(snapName, prevTgt),
			})
		}
		if newTgt != "" {
			add = append(add, &backend.Alias{
				Name:   alias,
				Target: snap.JoinSnapApp(snapName, newTgt),
			})
		}
	}
	if !dryRun {
		if err := be.UpdateAliases(add, remove); err != nil {
			return nil, nil, err
		}
	}
	return add, remove, nil
}

// AutoAliases allows to hook support for retrieving the automatic aliases of a snap.
var AutoAliases func(st *state.State, info *snap.Info) (map[string]string, error)

// autoAliasesDelta compares the automatic aliases with the current snap
// declaration for the installed snaps with the given names (or all if
// names is empty) and returns changed and dropped auto-aliases by
// snap name.
func autoAliasesDelta(st *state.State, names []string) (changed map[string][]string, dropped map[string][]string, err error) {
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
	changed = make(map[string][]string)
	dropped = make(map[string][]string)
	for instanceName, snapst := range snapStates {
		aliases := snapst.Aliases
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
			curTarget := aliases[alias]
			if curTarget == nil || curTarget.Auto != target {
				changed[instanceName] = append(changed[instanceName], alias)
			}
		}
		for alias, target := range aliases {
			if target.Auto != "" && autoAliases[alias] == "" {
				dropped[instanceName] = append(dropped[instanceName], alias)
			}
		}
	}
	return changed, dropped, firstErr
}

// refreshAliases applies the current snap-declaration aliases
// considering which applications exist in info and produces new aliases
// for the snap.
func refreshAliases(st *state.State, info *snap.Info, curAliases map[string]*AliasTarget) (newAliases map[string]*AliasTarget, err error) {
	autoAliases, err := AutoAliases(st, info)
	if err != nil {
		return nil, err
	}

	newAliases = make(map[string]*AliasTarget, len(autoAliases))
	// apply the current auto-aliases
	for alias, target := range autoAliases {
		if app := info.Apps[target]; app == nil || app.IsService() {
			// non-existing app or a daemon
			continue
		}
		newAliases[alias] = &AliasTarget{Auto: target}
	}

	// carry over the current manual ones
	for alias, curTarget := range curAliases {
		if curTarget.Manual == "" {
			continue
		}
		if app := info.Apps[curTarget.Manual]; app == nil || app.IsService() {
			// non-existing app or daemon
			continue
		}
		newTarget := newAliases[alias]
		if newTarget == nil {
			newAliases[alias] = &AliasTarget{Manual: curTarget.Manual}
		} else {
			// alias is both manually setup but has an underlying auto-alias
			newAliases[alias].Manual = curTarget.Manual
		}
	}
	return newAliases, nil
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
		for instanceName, aliases := range e.Conflicts {
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
			errParts = append(errParts, fmt.Sprintf("already enabled for %q", instanceName))
		}
		// TODO: add recommendation about what to do next
		return strings.Join(errParts, " ")
	}
	return fmt.Sprintf("cannot enable alias %q for %q, %s", e.Alias, e.Snap, e.Reason)
}

func addAliasConflicts(st *state.State, skipSnap string, testAliases map[string]bool, aliasConflicts map[string][]string, changing map[string]*SnapState) error {
	snapStates, err := All(st)
	if err != nil {
		return err
	}
	for otherSnap, snapst := range snapStates {
		if otherSnap == skipSnap {
			// skip
			continue
		}
		if nextSt, ok := changing[otherSnap]; ok {
			snapst = nextSt
		}
		autoDisabled := snapst.AutoAliasesDisabled
		var confls []string
		if len(snapst.Aliases) < len(testAliases) {
			for alias, target := range snapst.Aliases {
				if testAliases[alias] && target.Effective(autoDisabled) != "" {
					confls = append(confls, alias)
				}
			}
		} else {
			for alias := range testAliases {
				target := snapst.Aliases[alias]
				if target != nil && target.Effective(autoDisabled) != "" {
					confls = append(confls, alias)
				}
			}
		}
		if len(confls) > 0 {
			aliasConflicts[otherSnap] = confls
		}
	}
	return nil
}

// checkAliasesStatConflicts checks candAliases considering
// candAutoDisabled for conflicts against other snap aliases returning
// conflicting snaps and aliases for alias conflicts.
// changing can specify about to be set states for some snaps that will
// then be considered.
func checkAliasesConflicts(st *state.State, snapName string, candAutoDisabled bool, candAliases map[string]*AliasTarget, changing map[string]*SnapState) (conflicts map[string][]string, err error) {
	var snapNames map[string]*json.RawMessage
	err = st.Get("snaps", &snapNames)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}

	enabled := make(map[string]bool, len(candAliases))
	for alias, candTarget := range candAliases {
		if candTarget.Effective(candAutoDisabled) != "" {
			enabled[alias] = true
		} else {
			continue
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
	if err := addAliasConflicts(st, snapName, enabled, conflicts, changing); err != nil {
		return nil, err
	}
	if len(conflicts) != 0 {
		return conflicts, &AliasConflictError{Snap: snapName, Conflicts: conflicts}
	}
	return nil, nil
}

// checkSnapAliasConflict checks whether instanceName and its command
// namespace conflicts against installed snap aliases.
func checkSnapAliasConflict(st *state.State, instanceName string) error {
	prefix := fmt.Sprintf("%s.", instanceName)
	snapStates, err := All(st)
	if err != nil {
		return err
	}
	for otherSnap, snapst := range snapStates {
		autoDisabled := snapst.AutoAliasesDisabled
		for alias, target := range snapst.Aliases {
			if alias == instanceName || strings.HasPrefix(alias, prefix) {
				if target.Effective(autoDisabled) != "" {
					return fmt.Errorf("snap %q command namespace conflicts with alias %q for %q snap", instanceName, alias, otherSnap)
				}
			}
		}
	}
	return nil
}

// disableAliases returns newAliases corresponding to the disabling of
// curAliases, for manual aliases that means removed.
func disableAliases(curAliases map[string]*AliasTarget) (newAliases map[string]*AliasTarget, disabledManual map[string]string) {
	newAliases = make(map[string]*AliasTarget, len(curAliases))
	disabledManual = make(map[string]string, len(curAliases))
	for alias, curTarget := range curAliases {
		if curTarget.Manual != "" {
			disabledManual[alias] = curTarget.Manual
		}
		if curTarget.Auto != "" {
			newAliases[alias] = &AliasTarget{Auto: curTarget.Auto}
		}
	}
	if len(disabledManual) == 0 {
		disabledManual = nil
	}
	return newAliases, disabledManual
}

// reenableAliases returns newAliases corresponding to the re-enabling over
// curAliases of disabledManual manual aliases.
func reenableAliases(info *snap.Info, curAliases map[string]*AliasTarget, disabledManual map[string]string) (newAliases map[string]*AliasTarget) {
	newAliases = make(map[string]*AliasTarget, len(curAliases))
	for alias, aliasTarget := range curAliases {
		newAliases[alias] = aliasTarget
	}

	for alias, manual := range disabledManual {
		if app := info.Apps[manual]; app == nil || app.IsService() {
			// not a non-daemon app presently
			continue
		}

		newTarget := newAliases[alias]
		if newTarget == nil {
			newAliases[alias] = &AliasTarget{Manual: manual}
		} else {
			manualTarget := *newTarget
			manualTarget.Manual = manual
			newAliases[alias] = &manualTarget
		}
	}

	return newAliases
}

// pruneAutoAliases returns newAliases by dropping the automatic
// aliases autoAliases from curAliases, used as the task
// prune-auto-aliases to handle transfers of automatic aliases in a
// refresh.
func pruneAutoAliases(curAliases map[string]*AliasTarget, autoAliases []string) (newAliases map[string]*AliasTarget) {
	newAliases = make(map[string]*AliasTarget, len(curAliases))
	for alias, aliasTarget := range curAliases {
		newAliases[alias] = aliasTarget
	}
	for _, alias := range autoAliases {
		curTarget := curAliases[alias]
		if curTarget == nil {
			// nothing to do
			continue
		}
		if curTarget.Manual == "" {
			delete(newAliases, alias)
		} else {
			newAliases[alias] = &AliasTarget{Manual: curTarget.Manual}
		}
	}
	return newAliases
}

// transition to aliases v2
func (m *SnapManager) ensureAliasesV2() error {
	m.state.Lock()
	defer m.state.Unlock()

	var aliasesV1 map[string]interface{}
	err := m.state.Get("aliases", &aliasesV1)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if len(aliasesV1) == 0 {
		if err == nil { // something empty was there, delete it
			m.state.Set("aliases", nil)
		}
		// nothing to do
		return nil
	}

	snapStates, err := All(m.state)
	if err != nil {
		return err
	}

	// mark pending "alias" tasks as errored
	// they were never parts of lanes but either standalone or at
	// the start of wait chains
	for _, t := range m.state.Tasks() {
		if t.Kind() == "alias" && !t.Status().Ready() {
			var param interface{}
			err := t.Get("aliases", &param)
			if errors.Is(err, state.ErrNoState) {
				// not the old variant, leave alone
				continue
			}
			t.Errorf("internal representation for aliases has changed, please retry")
			t.SetStatus(state.ErrorStatus)
		}
	}

	withAliases := make(map[string]*SnapState, len(snapStates))
	for instanceName, snapst := range snapStates {
		err := m.backend.RemoveSnapAliases(instanceName)
		if err != nil {
			logger.Noticef("cannot cleanup aliases for %q: %v", instanceName, err)
			continue
		}

		info, err := snapst.CurrentInfo()
		if err != nil {
			logger.Noticef("cannot get info for %q: %v", instanceName, err)
			continue
		}
		newAliases, err := refreshAliases(m.state, info, nil)
		if err != nil {
			logger.Noticef("cannot get automatic aliases for %q: %v", instanceName, err)
			continue
		}
		// TODO: check for conflicts
		if len(newAliases) != 0 {
			snapst.Aliases = newAliases
			withAliases[instanceName] = snapst
		}
		snapst.AutoAliasesDisabled = false
		if !snapst.Active {
			snapst.AliasesPending = true
		}
	}

	for instanceName, snapst := range withAliases {
		if !snapst.AliasesPending {
			_, _, err := applyAliasesChange(instanceName, autoDis, nil, autoEn, snapst.Aliases, m.backend, doApply)
			if err != nil {
				// try to clean up and disable
				logger.Noticef("cannot create automatic aliases for %q: %v", instanceName, err)
				m.backend.RemoveSnapAliases(instanceName)
				snapst.AutoAliasesDisabled = true
			}
		}
		Set(m.state, instanceName, snapst)
	}

	m.state.Set("aliases", nil)
	return nil
}

// Alias sets up a manual alias from alias to app in snapName.
func Alias(st *state.State, instanceName, app, alias string) (*state.TaskSet, error) {
	if err := snap.ValidateAlias(alias); err != nil {
		return nil, err
	}

	var snapst SnapState
	err := Get(st, instanceName, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: instanceName}
	}
	if err != nil {
		return nil, err
	}
	if err := CheckChangeConflict(st, instanceName, nil); err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	snapsup := &SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: snapName},
		InstanceKey: instanceKey,
	}

	manualAlias := st.NewTask("alias", fmt.Sprintf(i18n.G("Setup manual alias %q => %q for snap %q"), alias, app, snapsup.InstanceName()))
	manualAlias.Set("alias", alias)
	manualAlias.Set("target", app)
	manualAlias.Set("snap-setup", &snapsup)

	return state.NewTaskSet(manualAlias), nil
}

// manualAliases returns newAliases with a manual alias to target setup over
// curAliases.
func manualAlias(info *snap.Info, curAliases map[string]*AliasTarget, target, alias string) (newAliases map[string]*AliasTarget, err error) {
	if app := info.Apps[target]; app == nil || app.IsService() {
		var reason string
		if app == nil {
			reason = fmt.Sprintf("target application %q does not exist", target)
		} else {
			reason = fmt.Sprintf("target application %q is a daemon", target)
		}
		return nil, fmt.Errorf("cannot enable alias %q for %q, %s", alias, info.InstanceName(), reason)
	}
	newAliases = make(map[string]*AliasTarget, len(curAliases))
	for alias, aliasTarget := range curAliases {
		newAliases[alias] = aliasTarget
	}

	newTarget := newAliases[alias]
	if newTarget == nil {
		newAliases[alias] = &AliasTarget{Manual: target}
	} else {
		manualTarget := *newTarget
		manualTarget.Manual = target
		newAliases[alias] = &manualTarget
	}

	return newAliases, nil
}

// DisableAllAliases disables all aliases of a snap, removing all manual ones.
func DisableAllAliases(st *state.State, instanceName string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, instanceName, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: instanceName}
	}
	if err != nil {
		return nil, err
	}

	if err := CheckChangeConflict(st, instanceName, nil); err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	snapsup := &SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: snapName},
		InstanceKey: instanceKey,
	}

	disableAll := st.NewTask("disable-aliases", fmt.Sprintf(i18n.G("Disable aliases for snap %q"), instanceName))
	disableAll.Set("snap-setup", &snapsup)

	return state.NewTaskSet(disableAll), nil
}

// RemoveManualAlias removes a manual alias.
func RemoveManualAlias(st *state.State, alias string) (ts *state.TaskSet, instanceName string, err error) {
	instanceName, err = findSnapOfManualAlias(st, alias)
	if err != nil {
		return nil, "", err
	}

	if err := CheckChangeConflict(st, instanceName, nil); err != nil {
		return nil, "", err
	}

	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	snapsup := &SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: snapName},
		InstanceKey: instanceKey,
	}

	unalias := st.NewTask("unalias", fmt.Sprintf(i18n.G("Remove manual alias %q for snap %q"), alias, instanceName))
	unalias.Set("alias", alias)
	unalias.Set("snap-setup", &snapsup)

	return state.NewTaskSet(unalias), instanceName, nil
}

func findSnapOfManualAlias(st *state.State, alias string) (snapName string, err error) {
	snapStates, err := All(st)
	if err != nil {
		return "", err
	}
	for instanceName, snapst := range snapStates {
		target := snapst.Aliases[alias]
		if target != nil && target.Manual != "" {
			return instanceName, nil
		}
	}
	return "", fmt.Errorf("cannot find manual alias %q in any snap", alias)
}

// manualUnalias returns newAliases with the manual alias removed from
// curAliases.
func manualUnalias(curAliases map[string]*AliasTarget, alias string) (newAliases map[string]*AliasTarget, err error) {
	newTarget := curAliases[alias]
	if newTarget == nil {
		return nil, fmt.Errorf("no alias %q", alias)
	}
	newAliases = make(map[string]*AliasTarget, len(curAliases))
	for alias, aliasTarget := range curAliases {
		newAliases[alias] = aliasTarget
	}

	if newTarget.Auto == "" {
		delete(newAliases, alias)
	} else {
		newAliases[alias] = &AliasTarget{Auto: newTarget.Auto}
	}

	return newAliases, nil
}

// Prefer enables all aliases of a snap in preference to conflicting aliases
// of other snaps whose aliases will be disabled (removed for manual ones).
func Prefer(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if errors.Is(err, state.ErrNoState) {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	snapName, instanceKey := snap.SplitInstanceName(name)
	snapsup := &SnapSetup{
		SideInfo:    &snap.SideInfo{RealName: snapName},
		InstanceKey: instanceKey,
	}

	prefer := st.NewTask("prefer-aliases", fmt.Sprintf(i18n.G("Prefer aliases for snap %q"), name))
	prefer.Set("snap-setup", &snapsup)

	return state.NewTaskSet(prefer), nil
}
