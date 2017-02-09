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

// Package snapstate implements the manager and state aspects responsible for the installation and removal of snaps.
package snapstate

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/i18n/dumb"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

func doInstall(st *state.State, snapst *SnapState, snapsup *SnapSetup) (*state.TaskSet, error) {
	if snapsup.Flags.Classic && !release.OnClassic {
		return nil, fmt.Errorf("classic confinement is only supported on classic systems")
	}
	if !snapst.HasCurrent() { // install?
		// check that the snap command namespace doesn't conflict with an enabled alias
		if err := checkSnapAliasConflict(st, snapsup.Name()); err != nil {
			return nil, err
		}
	}

	if err := CheckChangeConflict(st, snapsup.Name(), snapst); err != nil {
		return nil, err
	}

	targetRevision := snapsup.Revision()
	revisionStr := ""
	if snapsup.SideInfo != nil {
		revisionStr = fmt.Sprintf(" (%s)", targetRevision)
	}

	// check if we already have the revision locally (alters tasks)
	revisionIsLocal := snapst.LastIndex(targetRevision) >= 0

	var prepare, prev *state.Task
	fromStore := false
	// if we have a local revision here we go back to that
	if snapsup.SnapPath != "" || revisionIsLocal {
		prepare = st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q%s"), snapsup.SnapPath, revisionStr))
	} else {
		fromStore = true
		prepare = st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.Name(), revisionStr, snapsup.Channel))
	}
	prepare.Set("snap-setup", snapsup)

	tasks := []*state.Task{prepare}
	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}
	prev = prepare

	if fromStore {
		// fetch and check assertions
		checkAsserts := st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.Name(), revisionStr))
		addTask(checkAsserts)
		prev = checkAsserts
	}

	// mount
	if !revisionIsLocal {
		mount := st.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q%s"), snapsup.Name(), revisionStr))
		addTask(mount)
		prev = mount
	}

	if snapst.Active {
		// unlink-current-snap (will stop services for copy-data)
		stop := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), snapsup.Name()))
		addTask(stop)
		prev = stop

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.Name()))
		addTask(removeAliases)
		prev = removeAliases

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), snapsup.Name()))
		addTask(unlink)
		prev = unlink
	}

	// copy-data (needs stopped services by unlink)
	if !snapsup.Flags.Revert {
		copyData := st.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapsup.Name()))
		addTask(copyData)
		prev = copyData
	}

	// security
	setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), snapsup.Name(), revisionStr))
	addTask(setupSecurity)
	prev = setupSecurity

	// finalize (wrappers+current symlink)
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q%s available to the system"), snapsup.Name(), revisionStr))
	addTask(linkSnap)
	prev = linkSnap

	// setup aliases
	setAutoAliases := st.NewTask("set-auto-aliases", fmt.Sprintf(i18n.G("Set automatic aliases for snap %q"), snapsup.Name()))
	addTask(setAutoAliases)
	prev = setAutoAliases

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.Name()))
	addTask(setupAliases)
	prev = setupAliases

	// run new serices
	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q%s services"), snapsup.Name(), revisionStr))
	addTask(startSnapServices)
	prev = startSnapServices

	// Do not do that if we are reverting to a local revision
	if snapst.HasCurrent() && !snapsup.Flags.Revert {
		seq := snapst.Sequence
		currentIndex := snapst.LastIndex(snapst.Current)

		// discard everything after "current" (we may have reverted to
		// a previous versions earlier)
		for i := currentIndex + 1; i < len(seq); i++ {
			si := seq[i]
			if si.Revision == targetRevision {
				// but don't discard this one; its' the thing we're switching to!
				continue
			}
			ts := removeInactiveRevision(st, snapsup.Name(), si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		// make sure we're not scheduling the removal of the target
		// revision in the case where the target revision is already in
		// the sequence.
		for i := 0; i < currentIndex; i++ {
			si := seq[i]
			if si.Revision == targetRevision {
				// we do *not* want to removeInactiveRevision of this one
				copy(seq[i:], seq[i+1:])
				seq = seq[:len(seq)-1]
				currentIndex--
			}
		}

		// normal garbage collect
		for i := 0; i <= currentIndex-2; i++ {
			si := seq[i]
			if boot.InUse(snapsup.Name(), si.Revision) {
				continue
			}
			ts := removeInactiveRevision(st, snapsup.Name(), si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		addTask(st.NewTask("cleanup", fmt.Sprintf("Clean up %q%s install", snapsup.Name(), revisionStr)))
	}

	var defaults map[string]interface{}

	if !snapst.HasCurrent() && snapsup.SideInfo != nil && snapsup.SideInfo.SnapID != "" {
		gadget, err := GadgetInfo(st)
		if err != nil && err != state.ErrNoState {
			return nil, err
		}
		if err == nil {
			gadgetInfo, err := snap.ReadGadgetInfo(gadget)
			if err != nil {
				return nil, err
			}
			defaults = gadgetInfo.Defaults[snapsup.SideInfo.SnapID]
		}
	}

	installSet := state.NewTaskSet(tasks...)
	configSet := Configure(st, snapsup.Name(), defaults)
	configSet.WaitAll(installSet)
	installSet.AddAll(configSet)

	return installSet, nil
}

var Configure = func(st *state.State, snapName string, patch map[string]interface{}) *state.TaskSet {
	panic("internal error: snapstate.Configure is unset")
}

// CheckChangeConflict ensures that for the given snapName no other
// changes that alters the snap (like remove, install, refresh) are in
// progress. It also ensures that snapst (if not nil) did not get
// modified. If a conflict is detected an error is returned.
func CheckChangeConflict(st *state.State, snapName string, snapst *SnapState) error {
	for _, chg := range st.Changes() {
		if chg.Status().Ready() {
			continue
		}
		if chg.Kind() == "transition-ubuntu-core" {
			return fmt.Errorf("ubuntu-core to core transition in progress, no other changes allowed until this is done")
		}
	}

	for _, task := range st.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if (k == "link-snap" || k == "unlink-snap" || k == "alias") && (chg == nil || !chg.Status().Ready()) {
			snapsup, err := TaskSnapSetup(task)
			if err != nil {
				return fmt.Errorf("internal error: cannot obtain snap setup from task: %s", task.Summary())
			}
			if snapsup.Name() == snapName {
				return fmt.Errorf("snap %q has changes in progress", snapName)
			}
		}
	}

	if snapst != nil {
		// caller wants us to also make sure the SnapState in state
		// matches the one they provided. Necessary because we need to
		// unlock while talking to the store, during which a change can
		// sneak in (if it's before the taskset is created) (e.g. for
		// install, while getting the snap info; for refresh, when
		// getting what needs refreshing).
		var cursnapst SnapState
		if err := Get(st, snapName, &cursnapst); err != nil && err != state.ErrNoState {
			return err
		}

		// TODO: implement the rather-boring-but-more-performant SnapState.Equals
		if !reflect.DeepEqual(snapst, &cursnapst) {
			return fmt.Errorf("snap %q state changed during install preparations", snapName)
		}
	}

	return nil
}

// InstallPath returns a set of tasks for installing snap from a file path.
// Note that the state must be locked by the caller.
// The provided SideInfo can contain just a name which results in a
// local revision and sideloading, or full metadata in which case it
// the snap will appear as installed from the store.
func InstallPath(st *state.State, si *snap.SideInfo, path, channel string, flags Flags) (*state.TaskSet, error) {
	name := si.RealName
	if name == "" {
		return nil, fmt.Errorf("internal error: snap name to install %q not provided", path)
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if si.SnapID != "" {
		if si.Revision.Unset() {
			return nil, fmt.Errorf("internal error: snap id set to install %q but revision is unset", path)
		}
	}

	snapsup := &SnapSetup{
		SideInfo: si,
		SnapPath: path,
		Channel:  channel,
		Flags:    flags.ForSnapSetup(),
	}

	return doInstall(st, &snapst, snapsup)
}

// TryPath returns a set of tasks for trying a snap from a file path.
// Note that the state must be locked by the caller.
func TryPath(st *state.State, name, path string, flags Flags) (*state.TaskSet, error) {
	flags.TryMode = true

	return InstallPath(st, &snap.SideInfo{RealName: name}, path, "", flags)
}

// Install returns a set of tasks for installing snap.
// Note that the state must be locked by the caller.
func Install(st *state.State, name, channel string, revision snap.Revision, userID int, flags Flags) (*state.TaskSet, error) {
	if channel == "" {
		channel = "stable"
	}

	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if snapst.HasCurrent() {
		return nil, &snap.AlreadyInstalledError{Snap: name}
	}

	info, err := snapInfo(st, name, channel, revision, userID)
	if err != nil {
		return nil, err
	}

	if err := validateInfoAndFlags(info, &snapst, flags); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		Channel:      channel,
		UserID:       userID,
		Flags:        flags.ForSnapSetup(),
		DownloadInfo: &info.DownloadInfo,
		SideInfo:     &info.SideInfo,
	}

	return doInstall(st, &snapst, snapsup)
}

// InstallMany installs everything from the given list of names.
// Note that the state must be locked by the caller.
func InstallMany(st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	installed := make([]string, 0, len(names))
	tasksets := make([]*state.TaskSet, 0, len(names))
	for _, name := range names {
		ts, err := Install(st, name, "", snap.R(0), userID, Flags{})
		// FIXME: is this expected behavior?
		if _, ok := err.(*snap.AlreadyInstalledError); ok {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		installed = append(installed, name)
		tasksets = append(tasksets, ts)
	}

	return installed, tasksets, nil
}

// contains determines whether the given string is contained in the
// given list of strings, which must have been previously sorted using
// sort.Strings.
func contains(ns []string, n string) bool {
	i := sort.SearchStrings(ns, n)
	if i >= len(ns) {
		return false
	}
	return ns[i] == n
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	updates, _, err := refreshCandidates(st, nil, user)
	return updates, err
}

func refreshCandidates(st *state.State, names []string, user *auth.UserState) ([]*snap.Info, map[string]*SnapState, error) {
	snapStates, err := All(st)
	if err != nil {
		return nil, nil, err
	}

	sort.Strings(names)

	stateByID := make(map[string]*SnapState, len(snapStates))
	candidatesInfo := make([]*store.RefreshCandidate, 0, len(snapStates))
	for _, snapst := range snapStates {
		if len(names) == 0 && (snapst.TryMode || snapst.DevMode) {
			// no auto-refresh for trymode nor devmode
			continue
		}

		// FIXME: snaps that are not active are skipped for now
		//        until we know what we want to do
		if !snapst.Active {
			continue
		}

		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			// log something maybe?
			continue
		}

		if snapInfo.SnapID == "" {
			// no refresh for sideloaded
			continue
		}

		if len(names) > 0 && !contains(names, snapInfo.Name()) {
			continue
		}

		stateByID[snapInfo.SnapID] = snapst

		// get confinement preference from the snapstate
		candidateInfo := &store.RefreshCandidate{
			// the desired channel (not info.Channel!)
			Channel:  snapst.Channel,
			SnapID:   snapInfo.SnapID,
			Revision: snapInfo.Revision,
			Epoch:    snapInfo.Epoch,
		}

		if len(names) == 0 {
			candidateInfo.Block = snapst.Block()
		}

		candidatesInfo = append(candidatesInfo, candidateInfo)
	}

	theStore := Store(st)

	st.Unlock()
	updates, err := theStore.ListRefresh(candidatesInfo, user)
	st.Lock()
	if err != nil {
		return nil, nil, err
	}

	return updates, stateByID, nil
}

// ValidateRefreshes allows to hook validation into the handling of refresh candidates.
var ValidateRefreshes func(st *state.State, refreshes []*snap.Info, userID int) (validated []*snap.Info, err error)

// UpdateMany updates everything from the given list of names that the
// store says is updateable. If the list is empty, update everything.
// Note that the state must be locked by the caller.
func UpdateMany(st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	updates, stateByID, err := refreshCandidates(st, names, user)
	if err != nil {
		return nil, nil, err
	}

	if ValidateRefreshes != nil && len(updates) != 0 {
		updates, err = ValidateRefreshes(st, updates, userID)
		if err != nil {
			// not doing "refresh all" report the error
			if len(names) != 0 {
				return nil, nil, err
			}
			// doing "refresh all", log the problems
			logger.Noticef("cannot refresh some snaps: %v", err)
		}
	}

	params := func(update *snap.Info) (string, Flags, *SnapState) {
		snapst := stateByID[update.SnapID]
		return snapst.Channel, snapst.Flags, snapst

	}

	return doUpdate(st, names, updates, params, userID)
}

func doUpdate(st *state.State, names []string, updates []*snap.Info, params func(*snap.Info) (channel string, flags Flags, snapst *SnapState), userID int) ([]string, []*state.TaskSet, error) {
	tasksets := make([]*state.TaskSet, 0, len(updates))

	refreshAll := len(names) == 0
	var nameSet map[string]bool
	if len(names) != 0 {
		nameSet = make(map[string]bool, len(names))
		for _, name := range names {
			nameSet[name] = true
		}
	}

	newAutoAliases, mustRetireAutoAliases, transferTargets, err := autoAliasesUpdate(st, names, updates)
	if err != nil {
		return nil, nil, err
	}

	reportUpdated := make(map[string]bool, len(updates))
	var retiredAutoAliasesTs *state.TaskSet

	if len(mustRetireAutoAliases) != 0 {
		var err error
		retiredAutoAliasesTs, err = applyAutoAliasesDelta(st, mustRetireAutoAliases, "retire", refreshAll, func(snapName string, _ *state.TaskSet) {
			if nameSet[snapName] {
				reportUpdated[snapName] = true
			}
		})
		if err != nil {
			return nil, nil, err
		}
		tasksets = append(tasksets, retiredAutoAliasesTs)
	}

	// wait for the auto-alias retire tasks as needed
	scheduleUpdate := func(snapName string, ts *state.TaskSet) {
		if retiredAutoAliasesTs != nil && (mustRetireAutoAliases[snapName] != nil || transferTargets[snapName]) {
			ts.WaitAll(retiredAutoAliasesTs)
		}
		reportUpdated[snapName] = true
	}

	for _, update := range updates {
		channel, flags, snapst := params(update)

		if err := validateInfoAndFlags(update, snapst, flags); err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.Name(), err)
				continue
			}
			return nil, nil, err
		}

		snapsup := &SnapSetup{
			Channel:      channel,
			UserID:       userID,
			Flags:        flags.ForSnapSetup(),
			DownloadInfo: &update.DownloadInfo,
			SideInfo:     &update.SideInfo,
		}

		ts, err := doInstall(st, snapst, snapsup)
		if err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot refresh snap %q: %v", update.Name(), err)
				continue
			}
			return nil, nil, err
		}
		ts.JoinLane(st.NewLane())

		scheduleUpdate(update.Name(), ts)
		tasksets = append(tasksets, ts)
	}

	if len(newAutoAliases) != 0 {
		addAutoAliasesTs, err := applyAutoAliasesDelta(st, newAutoAliases, "enable", refreshAll, scheduleUpdate)
		if err != nil {
			return nil, nil, err
		}
		tasksets = append(tasksets, addAutoAliasesTs)
	}

	updated := make([]string, 0, len(reportUpdated))
	for name := range reportUpdated {
		updated = append(updated, name)
	}

	return updated, tasksets, nil
}

func applyAutoAliasesDelta(st *state.State, delta map[string][]string, op string, refreshAll bool, linkTs func(snapName string, ts *state.TaskSet)) (*state.TaskSet, error) {
	applyTs := state.NewTaskSet()
	for snapName, aliases := range delta {
		ts, err := ResetAliases(st, snapName, aliases)
		if err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot %s automatic aliases for snap %q: %v", op, snapName, err)
				continue
			}
			return nil, err
		}
		linkTs(snapName, ts)
		applyTs.AddAll(ts)
	}
	return applyTs, nil
}

func autoAliasesUpdate(st *state.State, names []string, updates []*snap.Info) (new map[string][]string, mustRetire map[string][]string, transferTargets map[string]bool, err error) {
	new, retired, err := AutoAliasesDelta(st, nil)
	if err != nil {
		if len(names) != 0 {
			// not "refresh all", error
			return nil, nil, nil, err
		}
		// log and continue
		logger.Noticef("cannot find the delta for automatic aliases for some snaps: %v", err)
	}

	refreshAll := len(names) == 0

	// retired alias -> snapName
	retiringAliases := make(map[string]string, len(retired))
	for snapName, aliases := range retired {
		for _, alias := range aliases {
			retiringAliases[alias] = snapName
		}
	}

	// filter new considering only names if set:
	// we add auto-aliases only for mentioned snaps
	if !refreshAll && len(new) != 0 {
		filteredNew := make(map[string][]string, len(new))
		for _, name := range names {
			if new[name] != nil {
				filteredNew[name] = new[name]
			}
		}
		new = filteredNew
	}

	// mark snaps that are sources or target of transfers
	transferSources := make(map[string]bool, len(retired))
	transferTargets = make(map[string]bool, len(new))
	for snapName, aliases := range new {
		for _, alias := range aliases {
			if source := retiringAliases[alias]; source != "" {
				transferTargets[snapName] = true
				transferSources[source] = true
			}
		}
	}

	// snaps with updates
	updating := make(map[string]bool, len(updates))
	for _, info := range updates {
		updating[info.Name()] = true
	}

	// add explicitly auto-aliases only for snaps that are not updated
	for snapName := range new {
		if updating[snapName] {
			delete(new, snapName)
		}
	}

	// retire explicitly auto-aliases only for snaps that are mentioned
	// and not updated OR the source of transfers
	mustRetire = make(map[string][]string, len(retired))
	for snapName := range transferSources {
		mustRetire[snapName] = retired[snapName]
	}
	if refreshAll {
		for snapName, aliases := range retired {
			if !updating[snapName] {
				mustRetire[snapName] = aliases
			}
		}
	} else {
		for _, name := range names {
			if !updating[name] && retired[name] != nil {
				mustRetire[name] = retired[name]
			}
		}
	}

	return new, mustRetire, transferTargets, nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(st *state.State, name, channel string, revision snap.Revision, userID int, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.HasCurrent() {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}

	// FIXME: snaps that are not active are skipped for now
	//        until we know what we want to do
	if !snapst.Active {
		return nil, fmt.Errorf("refreshing disabled snap %q not supported", name)
	}

	if channel == "" {
		channel = snapst.Channel
	}

	if !(flags.JailMode || flags.DevMode) {
		flags.Classic = flags.Classic || snapst.Flags.Classic
	}

	var updates []*snap.Info
	info, infoErr := infoForUpdate(st, &snapst, name, channel, revision, userID, flags)
	if infoErr != nil {
		if _, ok := infoErr.(*snap.NoUpdateAvailableError); !ok {
			return nil, infoErr
		}
		// there may be some new auto-aliases
	} else {
		updates = append(updates, info)
	}

	params := func(update *snap.Info) (string, Flags, *SnapState) {
		return channel, flags, &snapst
	}

	_, tts, err := doUpdate(st, []string{name}, updates, params, userID)
	if err != nil {
		return nil, err
	}

	// see if we need to update the channel
	if infoErr != nil && snapst.Channel != channel {
		snapsup := &SnapSetup{
			SideInfo: snapst.CurrentSideInfo(),
			Channel:  channel,
		}
		snapsup.SideInfo.Channel = channel

		switchSnap := st.NewTask("switch-snap-channel", fmt.Sprintf(i18n.G("Switch snap %q from %s to %s"), snapsup.Name(), snapst.Channel, channel))
		switchSnap.Set("snap-setup", &snapsup)

		switchSnapTs := state.NewTaskSet(switchSnap)
		for _, ts := range tts {
			switchSnapTs.WaitAll(ts)
		}
		tts = append(tts, switchSnapTs)
	}

	if len(tts) == 0 && len(updates) == 0 {
		// really nothing to do, return the original no-update-available error
		return nil, infoErr
	}
	flat := state.NewTaskSet()
	for _, ts := range tts {
		flat.AddAll(ts)
	}
	return flat, nil
}

func infoForUpdate(st *state.State, snapst *SnapState, name, channel string, revision snap.Revision, userID int, flags Flags) (*snap.Info, error) {
	if revision.Unset() {
		// good ol' refresh
		info, err := updateInfo(st, snapst, channel, userID)
		if err != nil {
			return nil, err
		}
		if err := validateInfoAndFlags(info, snapst, flags); err != nil {
			return nil, err
		}
		if ValidateRefreshes != nil && !flags.IgnoreValidation {
			_, err := ValidateRefreshes(st, []*snap.Info{info}, userID)
			if err != nil {
				return nil, err
			}
		}
		return info, nil
	}
	var sideInfo *snap.SideInfo
	for _, si := range snapst.Sequence {
		if si.Revision == revision {
			sideInfo = si
			break
		}
	}
	if sideInfo == nil {
		// refresh from given revision from store
		return snapInfo(st, name, channel, revision, userID)
	}

	// refresh-to-local
	return readInfo(name, sideInfo)
}

// Enable sets a snap to the active state
func Enable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}

	if snapst.Active {
		return nil, fmt.Errorf("snap %q already enabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: snapst.CurrentSideInfo(),
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), snapsup.Name(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	setupProfiles := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), snapsup.Name(), snapst.Current))
	setupProfiles.Set("snap-setup", &snapsup)
	setupProfiles.WaitFor(prepareSnap)

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system"), snapsup.Name(), snapst.Current))
	linkSnap.Set("snap-setup", &snapsup)
	linkSnap.WaitFor(setupProfiles)

	// setup aliases
	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.Name()))
	setupAliases.Set("snap-setup", &snapsup)
	setupAliases.WaitFor(linkSnap)

	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q (%s) services"), snapsup.Name(), snapst.Current))
	startSnapServices.Set("snap-setup", &snapsup)
	startSnapServices.WaitFor(setupAliases)

	return state.NewTaskSet(prepareSnap, setupProfiles, linkSnap, setupAliases, startSnapServices), nil
}

// Disable sets a snap to the inactive state
func Disable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}
	if !snapst.Active {
		return nil, fmt.Errorf("snap %q already disabled", name)
	}

	info, err := Info(st, name, snapst.Current)
	if err != nil {
		return nil, err
	}
	if !canDisable(info) {
		return nil, fmt.Errorf("snap %q cannot be disabled", name)
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: snapst.Current,
		},
	}

	stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q (%s) services"), snapsup.Name(), snapst.Current))
	stopSnapServices.Set("snap-setup", &snapsup)

	removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.Name()))
	removeAliases.Set("snap-setup-task", stopSnapServices.ID())
	removeAliases.WaitFor(stopSnapServices)

	unlinkSnap := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) unavailable to the system"), snapsup.Name(), snapst.Current))
	unlinkSnap.Set("snap-setup-task", stopSnapServices.ID())
	unlinkSnap.WaitFor(removeAliases)

	removeProfiles := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profiles of snap %q"), snapsup.Name()))
	removeProfiles.Set("snap-setup-task", stopSnapServices.ID())
	removeProfiles.WaitFor(unlinkSnap)

	return state.NewTaskSet(stopSnapServices, removeAliases, unlinkSnap, removeProfiles), nil
}

// canDisable verifies that a snap can be deactivated.
func canDisable(si *snap.Info) bool {
	for _, importantSnapType := range []snap.Type{snap.TypeGadget, snap.TypeKernel, snap.TypeOS} {
		if importantSnapType == si.Type {
			return false
		}
	}

	return true
}

// canRemove verifies that a snap can be removed.
func canRemove(si *snap.Info, snapst *SnapState, removeAll bool) bool {
	// removing single revisions is generally allowed
	if !removeAll {
		return true
	}

	// required snaps cannot be removed
	if snapst.Required {
		return false
	}

	// TODO: use Required for these too

	// Gadget snaps should not be removed as they are a key
	// building block for Gadgets. Do not remove their last
	// revision left.
	if si.Type == snap.TypeGadget {
		return false
	}

	// Allow "ubuntu-core" removals here because we might have two
	// core snaps installed (ubuntu-core and core). Note that
	// ideally we would only allow the removal of "ubuntu-core" if
	// we know that "core" is installed too and if we are part of
	// the "ubuntu-core->core" transition. But this transition
	// starts automatically on startup so the window of a user
	// triggering this manually is very small.
	//
	// Once the ubuntu-core -> core transition has landed for some
	// time we can remove the two lines below.
	if si.Name() == "ubuntu-core" && si.Type == snap.TypeOS {
		return true
	}

	// You never want to remove a kernel or OS. Do not remove their
	// last revision left.
	if si.Type == snap.TypeKernel || si.Type == snap.TypeOS {
		return false
	}

	// TODO: on classic likely let remove core even if active if it's only snap left.

	// never remove anything that is used for booting
	if boot.InUse(si.Name(), si.Revision) {
		return false
	}

	return true
}

// Remove returns a set of tasks for removing snap.
// Note that the state must be locked by the caller.
func Remove(st *state.State, name string, revision snap.Revision) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if !snapst.HasCurrent() {
		return nil, &snap.NotInstalledError{Snap: name, Rev: snap.R(0)}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	active := snapst.Active
	var removeAll bool
	if revision.Unset() {
		revision = snapst.Current
		removeAll = true
	} else {
		if active {
			if revision == snapst.Current {
				msg := "cannot remove active revision %s of snap %q"
				if len(snapst.Sequence) > 1 {
					msg += " (revert first?)"
				}
				return nil, fmt.Errorf(msg, revision, name)
			}
			active = false
		}

		if !revisionInSequence(&snapst, revision) {
			return nil, &snap.NotInstalledError{Snap: name, Rev: revision}
		}

		removeAll = len(snapst.Sequence) == 1
	}

	info, err := Info(st, name, revision)
	if err != nil {
		return nil, err
	}

	// check if this is something that can be removed
	if !canRemove(info, &snapst, removeAll) {
		return nil, fmt.Errorf("snap %q is not removable", name)
	}

	// main/current SnapSetup
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: revision,
		},
	}

	// trigger remove

	full := state.NewTaskSet()
	var chain *state.TaskSet

	addNext := func(ts *state.TaskSet) {
		if chain != nil {
			ts.WaitAll(chain)
		}
		full.AddAll(ts)
		chain = ts
	}

	if active { // unlink
		stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), name))
		stopSnapServices.Set("snap-setup", snapsup)

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), name))
		removeAliases.WaitFor(stopSnapServices)
		removeAliases.Set("snap-setup-task", stopSnapServices.ID())

		unlink := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))
		unlink.Set("snap-setup-task", stopSnapServices.ID())
		unlink.WaitFor(removeAliases)

		removeSecurity := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q (%s)"), name, revision))
		removeSecurity.WaitFor(unlink)
		removeSecurity.Set("snap-setup-task", stopSnapServices.ID())

		addNext(state.NewTaskSet(stopSnapServices, removeAliases, unlink, removeSecurity))
	}

	if removeAll {
		seq := snapst.Sequence
		for i := len(seq) - 1; i >= 0; i-- {
			si := seq[i]
			addNext(removeInactiveRevision(st, name, si.Revision))
		}

		clearAliases := st.NewTask("clear-aliases", fmt.Sprintf(i18n.G("Clear alias state for snap %q"), name))
		clearAliases.Set("snap-setup", &SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		discardConns := st.NewTask("discard-conns", fmt.Sprintf(i18n.G("Discard interface connections for snap %q (%s)"), name, revision))
		discardConns.WaitFor(clearAliases)
		discardConns.Set("snap-setup", &SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		addNext(state.NewTaskSet(clearAliases, discardConns))

	} else {
		addNext(removeInactiveRevision(st, name, revision))
	}

	return full, nil
}

func removeInactiveRevision(st *state.State, name string, revision snap.Revision) *state.TaskSet {
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: name,
			Revision: revision,
		},
	}

	clearData := st.NewTask("clear-snap", fmt.Sprintf(i18n.G("Remove data for snap %q (%s)"), name, revision))
	clearData.Set("snap-setup", snapsup)

	discardSnap := st.NewTask("discard-snap", fmt.Sprintf(i18n.G("Remove snap %q (%s) from the system"), name, revision))
	discardSnap.WaitFor(clearData)
	discardSnap.Set("snap-setup-task", clearData.ID())

	return state.NewTaskSet(clearData, discardSnap)
}

// RemoveMany removes everything from the given list of names.
// Note that the state must be locked by the caller.
func RemoveMany(st *state.State, names []string) ([]string, []*state.TaskSet, error) {
	removed := make([]string, 0, len(names))
	tasksets := make([]*state.TaskSet, 0, len(names))
	for _, name := range names {
		ts, err := Remove(st, name, snap.R(0))
		// FIXME: is this expected behavior?
		if _, ok := err.(*snap.NotInstalledError); ok {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		removed = append(removed, name)
		tasksets = append(tasksets, ts)
	}

	return removed, tasksets, nil
}

// Revert returns a set of tasks for reverting to the previous version of the snap.
// Note that the state must be locked by the caller.
func Revert(st *state.State, name string, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	pi := snapst.previousSideInfo()
	if pi == nil {
		return nil, fmt.Errorf("no revision to revert to")
	}

	return RevertToRevision(st, name, pi.Revision, flags)
}

func RevertToRevision(st *state.State, name string, rev snap.Revision, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}

	if snapst.Current == rev {
		return nil, fmt.Errorf("already on requested revision")
	}

	if !snapst.Active {
		return nil, fmt.Errorf("cannot revert inactive snaps")
	}
	i := snapst.LastIndex(rev)
	if i < 0 {
		return nil, fmt.Errorf("cannot find revision %s for snap %q", rev, name)
	}
	flags.Revert = true
	snapsup := &SnapSetup{
		SideInfo: snapst.Sequence[i],
		Flags:    flags.ForSnapSetup(),
	}
	return doInstall(st, &snapst, snapsup)
}

// TransitionCore transitions from an old core snap name to a new core
// snap name. It is used for the ubuntu-core -> core transition (that
// is not just a rename because the two snaps have different snapIDs)
//
// Note that this function makes some assumptions like:
// - no aliases setup for both snaps
// - no data needs to be copied
// - all interfaces are absolutely identical on both new and old
// Do not use this as a general way to transition from snap A to snap B.
func TransitionCore(st *state.State, oldName, newName string) ([]*state.TaskSet, error) {
	var oldSnapst, newSnapst SnapState
	err := Get(st, oldName, &oldSnapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !oldSnapst.HasCurrent() {
		return nil, fmt.Errorf("cannot transition snap %q: not installed", oldName)
	}

	var userID int
	newInfo, err := snapInfo(st, newName, oldSnapst.Channel, snap.R(0), userID)
	if err != nil {
		return nil, err
	}

	var all []*state.TaskSet
	// install new core (if not already installed)
	err = Get(st, newName, &newSnapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !newSnapst.HasCurrent() {
		// start by instaling the new snap
		tsInst, err := doInstall(st, &newSnapst, &SnapSetup{
			Channel:      oldSnapst.Channel,
			DownloadInfo: &newInfo.DownloadInfo,
			SideInfo:     &newInfo.SideInfo,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, tsInst)
	}

	// then transition the interface connections over
	transIf := st.NewTask("transition-ubuntu-core", fmt.Sprintf(i18n.G("Transition security profiles from %q to %q"), oldName, newName))
	transIf.Set("old-name", oldName)
	transIf.Set("new-name", newName)
	if len(all) > 0 {
		transIf.WaitAll(all[0])
	}
	tsTrans := state.NewTaskSet(transIf)
	all = append(all, tsTrans)

	// FIXME: this is just here for the tests
	transIf.Set("snap-setup", &SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: oldName,
		},
	})

	// then remove the old snap
	tsRm, err := Remove(st, oldName, snap.R(0))
	if err != nil {
		return nil, err
	}
	tsRm.WaitFor(transIf)
	all = append(all, tsRm)

	return all, nil
}

// State/info accessors

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(st *state.State, name string, revision snap.Revision) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	if err != nil {
		return nil, err
	}

	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if si := snapst.Sequence[i]; si.Revision == revision {
			return readInfo(name, si)
		}
	}

	return nil, fmt.Errorf("cannot find snap %q at revision %s", name, revision.String())
}

// CurrentInfo returns the information about the current revision of a snap with the given name.
func CurrentInfo(st *state.State, name string) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	info, err := snapst.CurrentInfo()
	if err == ErrNoCurrent {
		return nil, fmt.Errorf("cannot find snap %q", name)
	}
	return info, err
}

// Get retrieves the SnapState of the given snap.
func Get(st *state.State, name string, snapst *SnapState) error {
	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil {
		return err
	}
	raw, ok := snaps[name]
	if !ok {
		return state.ErrNoState
	}
	err = json.Unmarshal([]byte(*raw), &snapst)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap state: %v", err)
	}
	return nil
}

// All retrieves return a map from name to SnapState for all current snaps in the system state.
func All(st *state.State) (map[string]*SnapState, error) {
	// XXX: result is a map because sideloaded snaps carry no name
	// atm in their sideinfos
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	curStates := make(map[string]*SnapState, len(stateMap))
	for snapName, snapst := range stateMap {
		if snapst.HasCurrent() {
			curStates[snapName] = snapst
		}
	}
	return curStates, nil
}

// Set sets the SnapState of the given snap, overwriting any earlier state.
func Set(st *state.State, name string, snapst *SnapState) {
	var snaps map[string]*json.RawMessage
	err := st.Get("snaps", &snaps)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal snaps state: " + err.Error())
	}
	if snaps == nil {
		snaps = make(map[string]*json.RawMessage)
	}
	if snapst == nil || (len(snapst.Sequence) == 0) {
		delete(snaps, name)
	} else {
		data, err := json.Marshal(snapst)
		if err != nil {
			panic("internal error: cannot marshal snap state: " + err.Error())
		}
		raw := json.RawMessage(data)
		snaps[name] = &raw
	}
	st.Set("snaps", snaps)
}

// ActiveInfos returns information about all active snaps.
func ActiveInfos(st *state.State) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	var infos []*snap.Info
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}
	for snapName, snapst := range stateMap {
		if !snapst.Active {
			continue
		}
		snapInfo, err := snapst.CurrentInfo()
		if err != nil {
			logger.Noticef("cannot retrieve info for snap %q: %s", snapName, err)
			continue
		}
		infos = append(infos, snapInfo)
	}
	return infos, nil
}

func infosForTypes(st *state.State, snapType snap.Type) ([]*snap.Info, error) {
	var stateMap map[string]*SnapState
	if err := st.Get("snaps", &stateMap); err != nil && err != state.ErrNoState {
		return nil, err
	}

	var res []*snap.Info
	for _, snapst := range stateMap {
		if !snapst.HasCurrent() {
			continue
		}
		typ, err := snapst.Type()
		if err != nil {
			return nil, err
		}
		if typ != snapType {
			continue
		}
		si, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		res = append(res, si)
	}

	if len(res) == 0 {
		return nil, state.ErrNoState
	}

	return res, nil
}

func infoForType(st *state.State, snapType snap.Type) (*snap.Info, error) {
	res, err := infosForTypes(st, snapType)
	if err != nil {
		return nil, err
	}
	return res[0], nil
}

// GadgetInfo finds the current gadget snap's info.
func GadgetInfo(st *state.State) (*snap.Info, error) {
	return infoForType(st, snap.TypeGadget)
}

// KernelInfo finds the current kernel snap's info.
func KernelInfo(st *state.State) (*snap.Info, error) {
	return infoForType(st, snap.TypeKernel)
}

// AutoRefreshAssertions allows to hook fetching of important assertions
// into the Autorefresh function.
var AutoRefreshAssertions func(st *state.State, userID int) error

// AutoRefresh is the wrapper that will do a refresh of all the installed
// snaps on the system. In addition to that it will also refresh important
// assertions.
func AutoRefresh(st *state.State) ([]string, []*state.TaskSet, error) {
	userID := 0

	if AutoRefreshAssertions != nil {
		if err := AutoRefreshAssertions(st, userID); err != nil {
			return nil, nil, err
		}
	}

	return UpdateMany(st, nil, userID)
}

// CoreInfo finds the current OS snap's info. If both
// "core" and "ubuntu-core" is installed then "core"
// is preferred. Different core names are not supported
// currently and will result in an error.
//
// Once enough time has passed and everyone transitioned
// from ubuntu-core to core we can simplify this again
// and make it the same as the above "KernelInfo".
func CoreInfo(st *state.State) (*snap.Info, error) {
	res, err := infosForTypes(st, snap.TypeOS)
	if err != nil {
		return nil, err
	}

	// a single core: just return it
	if len(res) == 1 {
		return res[0], nil
	}

	// some systems have two cores: ubuntu-core/core
	// we always return "core" in this case
	if len(res) == 2 {
		if res[0].Name() == "core" && res[1].Name() == "ubuntu-core" {
			return res[0], nil
		}
		if res[0].Name() == "ubuntu-core" && res[1].Name() == "core" {
			return res[1], nil
		}
		return nil, fmt.Errorf("unexpected cores %q and %q", res[0].Name(), res[1].Name())
	}

	return nil, fmt.Errorf("unexpected number of cores, got %d", len(res))
}
