// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"sort"
	"strings"
	"time"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// control flags for doInstall
const (
	skipConfigure = 1 << iota
)

// control flags for "Configure()"
const (
	IgnoreHookError = 1 << iota
	TrackHookError
	UseConfigDefaults
)

func doInstall(st *state.State, snapst *SnapState, snapsup *SnapSetup, flags int) (*state.TaskSet, error) {
	if snapsup.InstanceName() == "system" {
		return nil, fmt.Errorf("cannot install reserved snap name 'system'")
	}
	if snapsup.InstanceName() == "snapd" {
		model, err := Model(st)
		if err != nil && err != state.ErrNoState {
			return nil, err
		}
		if model == nil || model.Base() == "" {
			return nil, fmt.Errorf("cannot install snapd snap on a model without a base snap yet")
		}
	}
	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf("cannot update disabled snap %q", snapsup.InstanceName())
	}

	if snapsup.Flags.Classic {
		if !release.OnClassic {
			return nil, fmt.Errorf("classic confinement is only supported on classic systems")
		} else if !dirs.SupportsClassicConfinement() {
			return nil, fmt.Errorf(i18n.G("classic confinement requires snaps under /snap or symlink from /snap to %s"), dirs.SnapMountDir)
		}
	}
	if !snapst.IsInstalled() { // install?
		// check that the snap command namespace doesn't conflict with an enabled alias
		if err := checkSnapAliasConflict(st, snapsup.InstanceName()); err != nil {
			return nil, err
		}
	}

	// TODO parallel-install: block parallel installation of core, kernel,
	// gadget and snapd snaps

	if err := CheckChangeConflict(st, snapsup.InstanceName(), snapst); err != nil {
		return nil, err
	}

	if snapst.IsInstalled() {
		// consider also the current revision to set plugs-only hint
		info, err := snapst.CurrentInfo()
		if err != nil {
			return nil, err
		}
		snapsup.PlugsOnly = snapsup.PlugsOnly && (len(info.Slots) == 0)
	}

	ts := state.NewTaskSet()

	targetRevision := snapsup.Revision()
	revisionStr := ""
	if snapsup.SideInfo != nil {
		revisionStr = fmt.Sprintf(" (%s)", targetRevision)
	}

	// check if we already have the revision locally (alters tasks)
	revisionIsLocal := snapst.LastIndex(targetRevision) >= 0

	prereq := st.NewTask("prerequisites", fmt.Sprintf(i18n.G("Ensure prerequisites for %q are available"), snapsup.InstanceName()))
	prereq.Set("snap-setup", snapsup)

	var prepare, prev *state.Task
	fromStore := false
	// if we have a local revision here we go back to that
	if snapsup.SnapPath != "" || revisionIsLocal {
		prepare = st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q%s"), snapsup.SnapPath, revisionStr))
	} else {
		fromStore = true
		prepare = st.NewTask("download-snap", fmt.Sprintf(i18n.G("Download snap %q%s from channel %q"), snapsup.InstanceName(), revisionStr, snapsup.Channel))
	}
	prepare.Set("snap-setup", snapsup)
	prepare.WaitFor(prereq)

	tasks := []*state.Task{prereq, prepare}
	addTask := func(t *state.Task) {
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
	}
	prev = prepare

	if fromStore {
		// fetch and check assertions
		checkAsserts := st.NewTask("validate-snap", fmt.Sprintf(i18n.G("Fetch and check assertions for snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(checkAsserts)
		prev = checkAsserts
	}

	// mount
	if !revisionIsLocal {
		mount := st.NewTask("mount-snap", fmt.Sprintf(i18n.G("Mount snap %q%s"), snapsup.InstanceName(), revisionStr))
		addTask(mount)
		prev = mount
	}

	// run refresh hooks when updating existing snap, otherwise run install hook further down.
	runRefreshHooks := (snapst.IsInstalled() && !snapsup.Flags.Revert)
	if runRefreshHooks {
		preRefreshHook := SetupPreRefreshHook(st, snapsup.InstanceName())
		addTask(preRefreshHook)
		prev = preRefreshHook
	}

	if snapst.IsInstalled() {
		// unlink-current-snap (will stop services for copy-data)
		stop := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), snapsup.InstanceName()))
		stop.Set("stop-reason", snap.StopReasonRefresh)
		addTask(stop)
		prev = stop

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
		addTask(removeAliases)
		prev = removeAliases

		unlink := st.NewTask("unlink-current-snap", fmt.Sprintf(i18n.G("Make current revision for snap %q unavailable"), snapsup.InstanceName()))
		addTask(unlink)
		prev = unlink
	}

	// copy-data (needs stopped services by unlink)
	if !snapsup.Flags.Revert {
		copyData := st.NewTask("copy-snap-data", fmt.Sprintf(i18n.G("Copy snap %q data"), snapsup.InstanceName()))
		addTask(copyData)
		prev = copyData
	}

	// security
	setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), snapsup.InstanceName(), revisionStr))
	addTask(setupSecurity)
	prev = setupSecurity

	// finalize (wrappers+current symlink)
	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q%s available to the system"), snapsup.InstanceName(), revisionStr))
	addTask(linkSnap)
	prev = linkSnap

	// auto-connections
	autoConnect := st.NewTask("auto-connect", fmt.Sprintf(i18n.G("Automatically connect eligible plugs and slots of snap %q"), snapsup.InstanceName()))
	addTask(autoConnect)
	prev = autoConnect

	// setup aliases
	setAutoAliases := st.NewTask("set-auto-aliases", fmt.Sprintf(i18n.G("Set automatic aliases for snap %q"), snapsup.InstanceName()))
	addTask(setAutoAliases)
	prev = setAutoAliases

	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	addTask(setupAliases)
	prev = setupAliases

	if runRefreshHooks {
		postRefreshHook := SetupPostRefreshHook(st, snapsup.InstanceName())
		addTask(postRefreshHook)
		prev = postRefreshHook
	}

	// only run install hook if installing the snap for the first time
	if !snapst.IsInstalled() {
		installHook := SetupInstallHook(st, snapsup.InstanceName())
		addTask(installHook)
		prev = installHook
	}

	// run new serices
	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q%s services"), snapsup.InstanceName(), revisionStr))
	addTask(startSnapServices)
	prev = startSnapServices

	// Do not do that if we are reverting to a local revision
	if snapst.IsInstalled() && !snapsup.Flags.Revert {
		var retain int
		if err := config.NewTransaction(st).Get("core", "refresh.retain", &retain); err != nil {
			retain = 3
		}
		retain-- //  we're adding one

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
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.Revision)
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
		for i := 0; i <= currentIndex-retain; i++ {
			si := seq[i]
			if boot.InUse(snapsup.InstanceName(), si.Revision) {
				continue
			}
			ts := removeInactiveRevision(st, snapsup.InstanceName(), si.Revision)
			ts.WaitFor(prev)
			tasks = append(tasks, ts.Tasks()...)
			prev = tasks[len(tasks)-1]
		}

		addTask(st.NewTask("cleanup", fmt.Sprintf("Clean up %q%s install", snapsup.InstanceName(), revisionStr)))
	}

	installSet := state.NewTaskSet(tasks...)
	installSet.WaitAll(ts)
	ts.AddAll(installSet)

	if flags&skipConfigure != 0 {
		return installSet, nil
	}

	var confFlags int
	if !snapst.IsInstalled() && snapsup.SideInfo != nil && snapsup.SideInfo.SnapID != "" {
		// installation, run configure using the gadget defaults
		// if available
		confFlags |= UseConfigDefaults
	}

	// we do not support configuration for bases or the "snapd" snap yet
	if snapsup.Type != snap.TypeBase && snapsup.InstanceName() != "snapd" {
		configSet := ConfigureSnap(st, snapsup.InstanceName(), confFlags)
		configSet.WaitAll(ts)
		ts.AddAll(configSet)
	}

	return ts, nil
}

// ConfigureSnap returns a set of tasks to configure snapName as done during installation/refresh.
func ConfigureSnap(st *state.State, snapName string, confFlags int) *state.TaskSet {
	// This is slightly ugly, ideally we would check the type instead
	// of hardcoding the name here. Unfortunately we do not have the
	// type until we actually run the change.
	if snapName == defaultCoreSnapName {
		confFlags |= IgnoreHookError
		confFlags |= TrackHookError
	}
	return Configure(st, snapName, nil, confFlags)
}

var Configure = func(st *state.State, snapName string, patch map[string]interface{}, flags int) *state.TaskSet {
	panic("internal error: snapstate.Configure is unset")
}

var SetupInstallHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupInstallHook is unset")
}

var SetupPreRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPreRefreshHook is unset")
}

var SetupPostRefreshHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupPostRefreshHook is unset")
}

var SetupRemoveHook = func(st *state.State, snapName string) *state.Task {
	panic("internal error: snapstate.SetupRemoveHook is unset")
}

// WaitRestart will return a Retry error if there is a pending restart
// and a real error if anything went wrong (like a rollback across
// restarts)
func WaitRestart(task *state.Task, snapsup *SnapSetup) (err error) {
	if ok, _ := task.State().Restarting(); ok {
		// don't continue until we are in the restarted snapd
		task.Logf("Waiting for restart...")
		return &state.Retry{}
	}

	snapInfo, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return err
	}

	// If not on classic check there was no rollback. A reboot
	// can be triggered by:
	// - core (old core16 world, system-reboot)
	// - bootable base snap (new core18 world, system-reboot)
	//
	// TODO: Detect "snapd" snap daemon-restarts here that
	//       fallback into the old version (once we have
	//       better snapd rollback support in core18).
	if !release.OnClassic {
		// TODO: double check that we really rebooted
		// otherwise this could be just a spurious restart
		// of snapd

		model, err := Model(task.State())
		if err != nil {
			return err
		}
		bootName := "core"
		typ := snap.TypeOS
		if model.Base() != "" {
			bootName = model.Base()
			typ = snap.TypeBase
		}
		// if it is not a bootable snap we are not interested
		if snapsup.InstanceName() != bootName {
			return nil
		}

		name, rev, err := CurrentBootNameAndRevision(typ)
		if err == ErrBootNameAndRevisionAgain {
			return &state.Retry{After: 5 * time.Second}
		}
		if err != nil {
			return err
		}

		if snapsup.InstanceName() != name || snapInfo.Revision != rev {
			// TODO: make sure this revision gets ignored for
			//       automatic refreshes
			return fmt.Errorf("cannot finish %s installation, there was a rollback across reboot", snapsup.InstanceName())
		}
	}

	return nil
}

func contentAttr(attrer interfaces.Attrer) string {
	var s string
	err := attrer.Attr("content", &s)
	if err != nil {
		return ""
	}
	return s
}

func contentIfaceAvailable(st *state.State, contentTag string) bool {
	repo := ifacerepo.Get(st)
	for _, slot := range repo.AllSlots("content") {
		if contentAttr(slot) == "" {
			continue
		}
		if contentAttr(slot) == contentTag {
			return true
		}
	}
	return false
}

// defaultContentPlugProviders takes a snap.Info and returns what
// default providers there are.
func defaultContentPlugProviders(st *state.State, info *snap.Info) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, plug := range info.Plugs {
		if plug.Interface == "content" {
			if contentAttr(plug) == "" {
				continue
			}
			if !contentIfaceAvailable(st, contentAttr(plug)) {
				var dprovider string
				err := plug.Attr("default-provider", &dprovider)
				if err != nil || dprovider == "" {
					continue
				}
				// The default-provider is a name. However old
				// documentation said it is "snapname:ifname",
				// we deal with this gracefully by just
				// stripping of the part after the ":"
				if name := strings.SplitN(dprovider, ":", 2)[0]; !seen[name] {
					out = append(out, name)
					seen[name] = true
				}
			}
		}
	}
	return out
}

// validateFeatureFlags validates the given snap only uses experimental
// features that are enabled by the user.
func validateFeatureFlags(st *state.State, info *snap.Info) error {
	tr := config.NewTransaction(st)

	if len(info.Layout) > 0 {
		var flag bool
		if err := tr.GetMaybe("core", "experimental.layouts", &flag); err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.layouts' to true")
		}
	}

	if info.InstanceKey != "" {
		var flag bool
		if err := tr.GetMaybe("core", "experimental.parallel-instances", &flag); err != nil {
			return err
		}
		if !flag {
			return fmt.Errorf("experimental feature disabled - test it by setting 'experimental.parallel-instances' to true")
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

	if err := canSwitchChannel(st, name, channel); err != nil {
		return nil, err
	}

	var instFlags int
	if flags.SkipConfigure {
		// extract it as a doInstall flag, this is not passed
		// into SnapSetup
		instFlags |= skipConfigure
	}

	// It is ok do open the snap file here because we either
	// have side info or the user passed --dangerous
	info, container, err := backend.OpenSnapFile(path, si)
	if err != nil {
		return nil, err
	}

	if err := validateContainer(container, info, logger.Noticef); err != nil {
		return nil, err
	}
	if err := validateFeatureFlags(st, info); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		Base:      info.Base,
		Prereq:    defaultContentPlugProviders(st, info),
		SideInfo:  si,
		SnapPath:  path,
		Channel:   channel,
		Flags:     flags.ForSnapSetup(),
		Type:      info.Type,
		PlugsOnly: len(info.Slots) == 0,
	}

	return doInstall(st, &snapst, snapsup, instFlags)
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
	if snapst.IsInstalled() {
		return nil, &snap.AlreadyInstalledError{Snap: name}
	}

	info, err := installInfo(st, name, channel, revision, userID)
	if err != nil {
		return nil, err
	}

	if err := validateInfoAndFlags(info, &snapst, flags); err != nil {
		return nil, err
	}
	if err := validateFeatureFlags(st, info); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		Channel:      channel,
		Base:         info.Base,
		Prereq:       defaultContentPlugProviders(st, info),
		UserID:       userID,
		Flags:        flags.ForSnapSetup(),
		DownloadInfo: &info.DownloadInfo,
		SideInfo:     &info.SideInfo,
		Type:         info.Type,
		PlugsOnly:    len(info.Slots) == 0,
		InstanceKey:  info.InstanceKey,
	}

	return doInstall(st, &snapst, snapsup, 0)
}

// InstallMany installs everything from the given list of names.
// Note that the state must be locked by the caller.
func InstallMany(st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	installed := make([]string, 0, len(names))
	tasksets := make([]*state.TaskSet, 0, len(names))
	// TODO: this could be reorged to do one single store call
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
		ts.JoinLane(st.NewLane())
		tasksets = append(tasksets, ts)
	}

	return installed, tasksets, nil
}

// RefreshCandidates gets a list of candidates for update
// Note that the state must be locked by the caller.
func RefreshCandidates(st *state.State, user *auth.UserState) ([]*snap.Info, error) {
	updates, _, _, err := refreshCandidates(context.TODO(), st, nil, user, nil)
	return updates, err
}

// ValidateRefreshes allows to hook validation into the handling of refresh candidates.
var ValidateRefreshes func(st *state.State, refreshes []*snap.Info, ignoreValidation map[string]bool, userID int) (validated []*snap.Info, err error)

// UpdateMany updates everything from the given list of names that the
// store says is updateable. If the list is empty, update everything.
// Note that the state must be locked by the caller.
func UpdateMany(ctx context.Context, st *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
	user, err := userFromUserID(st, userID)
	if err != nil {
		return nil, nil, err
	}

	updates, stateByID, ignoreValidation, err := refreshCandidates(ctx, st, names, user, nil)
	if err != nil {
		return nil, nil, err
	}

	if ValidateRefreshes != nil && len(updates) != 0 {
		updates, err = ValidateRefreshes(st, updates, ignoreValidation, userID)
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

	newAutoAliases, mustPruneAutoAliases, transferTargets, err := autoAliasesUpdate(st, names, updates)
	if err != nil {
		return nil, nil, err
	}

	reportUpdated := make(map[string]bool, len(updates))
	var pruningAutoAliasesTs *state.TaskSet

	if len(mustPruneAutoAliases) != 0 {
		var err error
		pruningAutoAliasesTs, err = applyAutoAliasesDelta(st, mustPruneAutoAliases, "prune", refreshAll, func(snapName string, _ *state.TaskSet) {
			if nameSet[snapName] {
				reportUpdated[snapName] = true
			}
		})
		if err != nil {
			return nil, nil, err
		}
		tasksets = append(tasksets, pruningAutoAliasesTs)
	}

	// wait for the auto-alias prune tasks as needed
	scheduleUpdate := func(snapName string, ts *state.TaskSet) {
		if pruningAutoAliasesTs != nil && (mustPruneAutoAliases[snapName] != nil || transferTargets[snapName]) {
			ts.WaitAll(pruningAutoAliasesTs)
		}
		reportUpdated[snapName] = true
	}

	// first snapd, core, bases, then rest
	sort.Sort(byKind(updates))
	prereqs := make(map[string]*state.TaskSet)
	waitPrereq := func(ts *state.TaskSet, prereqName string) {
		preTs := prereqs[prereqName]
		if preTs != nil {
			ts.WaitAll(preTs)
		}
	}

	// updates is sorted by kind so this will process first core
	// and bases and then other snaps
	for _, update := range updates {
		channel, flags, snapst := params(update)

		if err := validateInfoAndFlags(update, snapst, flags); err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}
		if err := validateFeatureFlags(st, update); err != nil {
			if refreshAll {
				logger.Noticef("cannot update %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}

		snapUserID, err := userIDForSnap(st, snapst, userID)
		if err != nil {
			return nil, nil, err
		}

		snapsup := &SnapSetup{
			Base:         update.Base,
			Prereq:       defaultContentPlugProviders(st, update),
			Channel:      channel,
			UserID:       snapUserID,
			Flags:        flags.ForSnapSetup(),
			DownloadInfo: &update.DownloadInfo,
			SideInfo:     &update.SideInfo,
			Type:         update.Type,
			PlugsOnly:    len(update.Slots) == 0,
			InstanceKey:  update.InstanceKey,
		}

		ts, err := doInstall(st, snapst, snapsup, 0)
		if err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot refresh snap %q: %v", update.InstanceName(), err)
				continue
			}
			return nil, nil, err
		}
		ts.JoinLane(st.NewLane())

		// because of the sorting of updates we fill prereqs
		// first (if branch) and only then use it to setup
		// waits (else branch)
		if update.Type == snap.TypeOS || update.Type == snap.TypeBase {
			// prereq types come first in updates, we
			// also assume bases don't have hooks, otherwise
			// they would need to wait on core
			prereqs[update.InstanceName()] = ts
		} else {
			// prereqs were processed already, wait for
			// them as necessary for the other kind of
			// snaps
			waitPrereq(ts, defaultCoreSnapName)
			if update.Base != "" {
				waitPrereq(ts, update.Base)
			}
		}

		scheduleUpdate(update.InstanceName(), ts)
		tasksets = append(tasksets, ts)
	}

	if len(newAutoAliases) != 0 {
		addAutoAliasesTs, err := applyAutoAliasesDelta(st, newAutoAliases, "refresh", refreshAll, scheduleUpdate)
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

type byKind []*snap.Info

func (bk byKind) Len() int      { return len(bk) }
func (bk byKind) Swap(i, j int) { bk[i], bk[j] = bk[j], bk[i] }

var kindRevOrder = map[snap.Type]int{
	snap.TypeOS:   2,
	snap.TypeBase: 1,
}

func (bk byKind) Less(i, j int) bool {
	// snapd sorts first to ensure that on all refrehses it is the first
	// snap package that gets refreshed.
	if bk[i].SnapName() == "snapd" {
		return true
	}
	if bk[j].SnapName() == "snapd" {
		return false
	}

	iRevOrd := kindRevOrder[bk[i].Type]
	jRevOrd := kindRevOrder[bk[j].Type]
	return iRevOrd >= jRevOrd
}

func applyAutoAliasesDelta(st *state.State, delta map[string][]string, op string, refreshAll bool, linkTs func(snapName string, ts *state.TaskSet)) (*state.TaskSet, error) {
	applyTs := state.NewTaskSet()
	kind := "refresh-aliases"
	msg := i18n.G("Refresh aliases for snap %q")
	if op == "prune" {
		kind = "prune-auto-aliases"
		msg = i18n.G("Prune automatic aliases for snap %q")
	}
	for snapName, aliases := range delta {
		if err := CheckChangeConflict(st, snapName, nil); err != nil {
			if refreshAll {
				// doing "refresh all", just skip this snap
				logger.Noticef("cannot %s automatic aliases for snap %q: %v", op, snapName, err)
				continue
			}
			return nil, err
		}

		snapsup := &SnapSetup{
			SideInfo: &snap.SideInfo{RealName: snapName},
		}
		alias := st.NewTask(kind, fmt.Sprintf(msg, snapsup.InstanceName()))
		alias.Set("snap-setup", &snapsup)
		if op == "prune" {
			alias.Set("aliases", aliases)
		}
		ts := state.NewTaskSet(alias)
		linkTs(snapName, ts)
		applyTs.AddAll(ts)
	}
	return applyTs, nil
}

func autoAliasesUpdate(st *state.State, names []string, updates []*snap.Info) (changed map[string][]string, mustPrune map[string][]string, transferTargets map[string]bool, err error) {
	changed, dropped, err := autoAliasesDelta(st, nil)
	if err != nil {
		if len(names) != 0 {
			// not "refresh all", error
			return nil, nil, nil, err
		}
		// log and continue
		logger.Noticef("cannot find the delta for automatic aliases for some snaps: %v", err)
	}

	refreshAll := len(names) == 0

	// dropped alias -> snapName
	droppedAliases := make(map[string][]string, len(dropped))
	for snapName, aliases := range dropped {
		for _, alias := range aliases {
			droppedAliases[alias] = append(droppedAliases[alias], snapName)
		}
	}

	// filter changed considering only names if set:
	// we add auto-aliases only for mentioned snaps
	if !refreshAll && len(changed) != 0 {
		filteredChanged := make(map[string][]string, len(changed))
		for _, name := range names {
			if changed[name] != nil {
				filteredChanged[name] = changed[name]
			}
		}
		changed = filteredChanged
	}

	// mark snaps that are sources or target of transfers
	transferSources := make(map[string]bool, len(dropped))
	transferTargets = make(map[string]bool, len(changed))
	for snapName, aliases := range changed {
		for _, alias := range aliases {
			if sources := droppedAliases[alias]; len(sources) != 0 {
				transferTargets[snapName] = true
				for _, source := range sources {
					transferSources[source] = true
				}
			}
		}
	}

	// snaps with updates
	updating := make(map[string]bool, len(updates))
	for _, info := range updates {
		updating[info.InstanceName()] = true
	}

	// add explicitly auto-aliases only for snaps that are not updated
	for snapName := range changed {
		if updating[snapName] {
			delete(changed, snapName)
		}
	}

	// prune explicitly auto-aliases only for snaps that are mentioned
	// and not updated OR the source of transfers
	mustPrune = make(map[string][]string, len(dropped))
	for snapName := range transferSources {
		mustPrune[snapName] = dropped[snapName]
	}
	if refreshAll {
		for snapName, aliases := range dropped {
			if !updating[snapName] {
				mustPrune[snapName] = aliases
			}
		}
	} else {
		for _, name := range names {
			if !updating[name] && dropped[name] != nil {
				mustPrune[name] = dropped[name]
			}
		}
	}

	return changed, mustPrune, transferTargets, nil
}

// canSwitchChannel returns an error if switching to newChannel for snap is
// forbidden.
func canSwitchChannel(st *state.State, snapName, newChannel string) error {
	// nothing to do
	if newChannel == "" {
		return nil
	}

	// ensure we do not switch away from the kernel-track in the model
	model, err := Model(st)
	if err != nil && err != state.ErrNoState {
		return err
	}
	if model == nil {
		return nil
	}

	if snapName != model.Kernel() {
		return nil
	}
	if model.KernelTrack() == "" {
		return nil
	}
	nch, err := snap.ParseChannel(newChannel, "")
	if err != nil {
		return err
	}
	if nch.Track != model.KernelTrack() {
		return fmt.Errorf("cannot switch from kernel-track %q to %q", model.KernelTrack(), nch.String())
	}

	return nil
}

// Switch switches a snap to a new channel
func Switch(st *state.State, name, channel string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	if err := CheckChangeConflict(st, name, nil); err != nil {
		return nil, err
	}

	if err := canSwitchChannel(st, name, channel); err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Channel:     channel,
		InstanceKey: snapst.InstanceKey,
	}

	switchSnap := st.NewTask("switch-snap", fmt.Sprintf(i18n.G("Switch snap %q to %s"), snapsup.InstanceName(), snapsup.Channel))
	switchSnap.Set("snap-setup", &snapsup)

	return state.NewTaskSet(switchSnap), nil
}

// Update initiates a change updating a snap.
// Note that the state must be locked by the caller.
func Update(st *state.State, name, channel string, revision snap.Revision, userID int, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: name}
	}

	// FIXME: snaps that are not active are skipped for now
	//        until we know what we want to do
	if !snapst.Active {
		return nil, fmt.Errorf("refreshing disabled snap %q not supported", name)
	}

	if err := canSwitchChannel(st, name, channel); err != nil {
		return nil, err
	}

	if channel == "" {
		channel = snapst.Channel
	}

	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode) {
		flags.Classic = flags.Classic || snapst.Flags.Classic
	}

	var updates []*snap.Info
	info, infoErr := infoForUpdate(st, &snapst, name, channel, revision, userID, flags)
	switch infoErr {
	case nil:
		updates = append(updates, info)
	case store.ErrNoUpdateAvailable:
		// there may be some new auto-aliases
	default:
		return nil, infoErr
	}

	params := func(update *snap.Info) (string, Flags, *SnapState) {
		return channel, flags, &snapst
	}

	_, tts, err := doUpdate(st, []string{name}, updates, params, userID)
	if err != nil {
		return nil, err
	}

	// see if we need to update the channel or toggle ignore-validation
	if infoErr == store.ErrNoUpdateAvailable && (snapst.Channel != channel || snapst.IgnoreValidation != flags.IgnoreValidation) {
		if err := CheckChangeConflict(st, name, nil); err != nil {
			return nil, err
		}

		snapsup := &SnapSetup{
			SideInfo:    snapst.CurrentSideInfo(),
			Flags:       snapst.Flags.ForSnapSetup(),
			InstanceKey: snapst.InstanceKey,
		}

		if snapst.Channel != channel {
			// update the tracked channel
			snapsup.Channel = channel
			// Update the current snap channel as well. This ensures that
			// the UI displays the right values.
			snapsup.SideInfo.Channel = channel

			switchSnap := st.NewTask("switch-snap-channel", fmt.Sprintf(i18n.G("Switch snap %q from %s to %s"), snapsup.InstanceName(), snapst.Channel, channel))
			switchSnap.Set("snap-setup", &snapsup)

			switchSnapTs := state.NewTaskSet(switchSnap)
			for _, ts := range tts {
				switchSnapTs.WaitAll(ts)
			}
			tts = append(tts, switchSnapTs)
		}

		if snapst.IgnoreValidation != flags.IgnoreValidation {
			// toggle ignore validation
			snapsup.IgnoreValidation = flags.IgnoreValidation
			toggle := st.NewTask("toggle-snap-flags", fmt.Sprintf(i18n.G("Toggle snap %q flags"), snapsup.InstanceName()))
			toggle.Set("snap-setup", &snapsup)

			toggleTs := state.NewTaskSet(toggle)
			for _, ts := range tts {
				toggleTs.WaitAll(ts)
			}
			tts = append(tts, toggleTs)
		}
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
		opts := &updateInfoOpts{
			channel:          channel,
			ignoreValidation: flags.IgnoreValidation,
			amend:            flags.Amend,
		}
		info, err := updateInfo(st, snapst, opts, userID)
		if err != nil {
			return nil, err
		}
		if err := validateInfoAndFlags(info, snapst, flags); err != nil {
			return nil, err
		}
		if ValidateRefreshes != nil && !flags.IgnoreValidation {
			_, err := ValidateRefreshes(st, []*snap.Info{info}, nil, userID)
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
		return updateToRevisionInfo(st, snapst, revision, userID)
	}

	// refresh-to-local, this assumes the snap revision is mounted
	return readInfo(name, sideInfo, errorOnBroken)
}

// AutoRefreshAssertions allows to hook fetching of important assertions
// into the Autorefresh function.
var AutoRefreshAssertions func(st *state.State, userID int) error

// AutoRefresh is the wrapper that will do a refresh of all the installed
// snaps on the system. In addition to that it will also refresh important
// assertions.
func AutoRefresh(ctx context.Context, st *state.State) ([]string, []*state.TaskSet, error) {
	userID := 0

	if AutoRefreshAssertions != nil {
		if err := AutoRefreshAssertions(st, userID); err != nil {
			return nil, nil, err
		}
	}

	return UpdateMany(ctx, st, nil, userID)
}

// Enable sets a snap to the active state
func Enable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
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

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.CurrentSideInfo(),
		Flags:       snapst.Flags.ForSnapSetup(),
		Type:        info.Type,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	prepareSnap := st.NewTask("prepare-snap", fmt.Sprintf(i18n.G("Prepare snap %q (%s)"), snapsup.InstanceName(), snapst.Current))
	prepareSnap.Set("snap-setup", &snapsup)

	setupProfiles := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), snapsup.InstanceName(), snapst.Current))
	setupProfiles.Set("snap-setup-task", prepareSnap.ID())
	setupProfiles.WaitFor(prepareSnap)

	linkSnap := st.NewTask("link-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) available to the system"), snapsup.InstanceName(), snapst.Current))
	linkSnap.Set("snap-setup-task", prepareSnap.ID())
	linkSnap.WaitFor(setupProfiles)

	// setup aliases
	setupAliases := st.NewTask("setup-aliases", fmt.Sprintf(i18n.G("Setup snap %q aliases"), snapsup.InstanceName()))
	setupAliases.Set("snap-setup-task", prepareSnap.ID())
	setupAliases.WaitFor(linkSnap)

	startSnapServices := st.NewTask("start-snap-services", fmt.Sprintf(i18n.G("Start snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	startSnapServices.Set("snap-setup-task", prepareSnap.ID())
	startSnapServices.WaitFor(setupAliases)

	return state.NewTaskSet(prepareSnap, setupProfiles, linkSnap, setupAliases, startSnapServices), nil
}

// Disable sets a snap to the inactive state
func Disable(st *state.State, name string) (*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
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
			RealName: snap.InstanceSnap(name),
			Revision: snapst.Current,
		},
		Type:        info.Type,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}

	stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q (%s) services"), snapsup.InstanceName(), snapst.Current))
	stopSnapServices.Set("snap-setup", &snapsup)
	stopSnapServices.Set("stop-reason", snap.StopReasonDisable)

	removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), snapsup.InstanceName()))
	removeAliases.Set("snap-setup-task", stopSnapServices.ID())
	removeAliases.WaitFor(stopSnapServices)

	unlinkSnap := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q (%s) unavailable to the system"), snapsup.InstanceName(), snapst.Current))
	unlinkSnap.Set("snap-setup-task", stopSnapServices.ID())
	unlinkSnap.WaitFor(removeAliases)

	removeProfiles := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profiles of snap %q"), snapsup.InstanceName()))
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
func canRemove(st *state.State, si *snap.Info, snapst *SnapState, removeAll bool) bool {
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
	if si.InstanceName() == "ubuntu-core" && si.Type == snap.TypeOS {
		return true
	}

	// Allow snap.TypeOS removals if a different base is in use
	//
	// Note that removal of the boot base itself is prevented
	// via the snapst.Required flag that is set on firstboot.
	if si.Type == snap.TypeOS {
		if model, err := Model(st); err == nil {
			if model.Base() != "" {
				return true
			}
		}
	}

	// You never want to remove a kernel or OS. Do not remove their
	// last revision left.
	if si.Type == snap.TypeKernel || si.Type == snap.TypeOS {
		return false
	}

	// TODO: ensure that you cannot remove bases/core if those are
	//       needed by one of the installed snaps.

	// TODO: on classic likely let remove core even if active if it's only snap left.

	// never remove anything that is used for booting
	if boot.InUse(si.InstanceName(), si.Revision) {
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

	if !snapst.IsInstalled() {
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
	if !canRemove(st, info, &snapst, removeAll) {
		return nil, fmt.Errorf("snap %q is not removable", name)
	}

	// main/current SnapSetup
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snap.InstanceSnap(name),
			Revision: revision,
		},
		Type:        info.Type,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
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

	var removeHook *state.Task
	// only run remove hook if uninstalling the snap completely
	if removeAll {
		removeHook = SetupRemoveHook(st, snapsup.InstanceName())
	}

	if active { // unlink
		var prev *state.Task

		stopSnapServices := st.NewTask("stop-snap-services", fmt.Sprintf(i18n.G("Stop snap %q services"), name))
		stopSnapServices.Set("snap-setup", snapsup)
		stopSnapServices.Set("stop-reason", snap.StopReasonRemove)
		prev = stopSnapServices

		tasks := []*state.Task{stopSnapServices}
		if removeHook != nil {
			tasks = append(tasks, removeHook)
			removeHook.WaitFor(prev)
			prev = removeHook
		}

		removeAliases := st.NewTask("remove-aliases", fmt.Sprintf(i18n.G("Remove aliases for snap %q"), name))
		removeAliases.WaitFor(prev)
		removeAliases.Set("snap-setup-task", stopSnapServices.ID())

		unlink := st.NewTask("unlink-snap", fmt.Sprintf(i18n.G("Make snap %q unavailable to the system"), name))
		unlink.Set("snap-setup-task", stopSnapServices.ID())
		unlink.WaitFor(removeAliases)

		removeSecurity := st.NewTask("remove-profiles", fmt.Sprintf(i18n.G("Remove security profile for snap %q (%s)"), name, revision))
		removeSecurity.WaitFor(unlink)
		removeSecurity.Set("snap-setup-task", stopSnapServices.ID())

		tasks = append(tasks, removeAliases, unlink, removeSecurity)
		addNext(state.NewTaskSet(tasks...))
	} else if removeHook != nil {
		addNext(state.NewTaskSet(removeHook))
	}

	if removeAll {
		seq := snapst.Sequence
		for i := len(seq) - 1; i >= 0; i-- {
			si := seq[i]
			addNext(removeInactiveRevision(st, name, si.Revision))
		}

		discardConns := st.NewTask("discard-conns", fmt.Sprintf(i18n.G("Discard interface connections for snap %q (%s)"), name, revision))
		discardConns.Set("snap-setup", &SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: snap.InstanceSnap(name),
			},
			InstanceKey: snapst.InstanceKey,
		})
		addNext(state.NewTaskSet(discardConns))
	} else {
		addNext(removeInactiveRevision(st, name, revision))
	}

	return full, nil
}

func removeInactiveRevision(st *state.State, name string, revision snap.Revision) *state.TaskSet {
	snapName, instanceKey := snap.SplitInstanceName(name)
	snapsup := SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			Revision: revision,
		},
		InstanceKey: instanceKey,
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
		ts.JoinLane(st.NewLane())
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
	// TODO: make flags be per revision to avoid this logic (that
	//       leaves corner cases all over the place)
	if !(flags.JailMode || flags.DevMode || flags.Classic) {
		if snapst.Flags.DevMode {
			flags.DevMode = true
		}
		if snapst.Flags.JailMode {
			flags.JailMode = true
		}
		if snapst.Flags.Classic {
			flags.Classic = true
		}
	}

	info, err := Info(st, name, rev)
	if err != nil {
		return nil, err
	}

	snapsup := &SnapSetup{
		SideInfo:    snapst.Sequence[i],
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: snapst.InstanceKey,
	}
	return doInstall(st, &snapst, snapsup, 0)
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
	if !oldSnapst.IsInstalled() {
		return nil, fmt.Errorf("cannot transition snap %q: not installed", oldName)
	}

	var all []*state.TaskSet
	// install new core (if not already installed)
	err = Get(st, newName, &newSnapst)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if !newSnapst.IsInstalled() {
		var userID int
		newInfo, err := installInfo(st, newName, oldSnapst.Channel, snap.R(0), userID)
		if err != nil {
			return nil, err
		}

		// start by instaling the new snap
		tsInst, err := doInstall(st, &newSnapst, &SnapSetup{
			Channel:      oldSnapst.Channel,
			DownloadInfo: &newInfo.DownloadInfo,
			SideInfo:     &newInfo.SideInfo,
			Type:         newInfo.Type,
		}, 0)
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

// Installing returns whether there's an in-progress installation.
func Installing(st *state.State) bool {
	for _, task := range st.Tasks() {
		k := task.Kind()
		chg := task.Change()
		if k == "mount-snap" && chg != nil && !chg.Status().Ready() {
			return true
		}
	}
	return false
}

// Info returns the information about the snap with given name and revision.
// Works also for a mounted candidate snap in the process of being installed.
func Info(st *state.State, name string, revision snap.Revision) (*snap.Info, error) {
	var snapst SnapState
	err := Get(st, name, &snapst)
	if err == state.ErrNoState {
		return nil, &snap.NotInstalledError{Snap: name}
	}
	if err != nil {
		return nil, err
	}

	for i := len(snapst.Sequence) - 1; i >= 0; i-- {
		if si := snapst.Sequence[i]; si.Revision == revision {
			return readInfo(name, si, 0)
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
		return nil, &snap.NotInstalledError{Snap: name}
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
		curStates[snapName] = snapst
	}
	return curStates, nil
}

// NumSnaps returns the number of installed snaps.
func NumSnaps(st *state.State) (int, error) {
	var snaps map[string]*json.RawMessage
	if err := st.Get("snaps", &snaps); err != nil && err != state.ErrNoState {
		return -1, err
	}
	return len(snaps), nil
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
		if !snapst.IsInstalled() {
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
		if res[0].InstanceName() == defaultCoreSnapName && res[1].InstanceName() == "ubuntu-core" {
			return res[0], nil
		}
		if res[0].InstanceName() == "ubuntu-core" && res[1].InstanceName() == defaultCoreSnapName {
			return res[1], nil
		}
		return nil, fmt.Errorf("unexpected cores %q and %q", res[0].InstanceName(), res[1].InstanceName())
	}

	return nil, fmt.Errorf("unexpected number of cores, got %d", len(res))
}

// ConfigDefaults returns the configuration defaults for the snap specified in
// the gadget. If gadget is absent or the snap has no snap-id it returns
// ErrNoState.
func ConfigDefaults(st *state.State, snapName string) (map[string]interface{}, error) {
	gadget, err := GadgetInfo(st)
	if err != nil {
		return nil, err
	}

	var snapst SnapState
	if err := Get(st, snapName, &snapst); err != nil {
		return nil, err
	}

	core, err := CoreInfo(st)
	if err != nil {
		return nil, err
	}
	isCoreDefaults := core.InstanceName() == snapName

	si := snapst.CurrentSideInfo()
	// core snaps can be addressed even without a snap-id via the special
	// "system" value in the config; first-boot always configures the core
	// snap with UseConfigDefaults
	if si.SnapID == "" && !isCoreDefaults {
		return nil, state.ErrNoState
	}

	gadgetInfo, err := snap.ReadGadgetInfo(gadget, release.OnClassic)
	if err != nil {
		return nil, err
	}

	// we support setting core defaults via "system"
	if isCoreDefaults {
		if defaults, ok := gadgetInfo.Defaults["system"]; ok {
			if _, ok := gadgetInfo.Defaults[si.SnapID]; ok && si.SnapID != "" {
				logger.Noticef("core snap configuration defaults found under both 'system' key and core-snap-id, preferring 'system'")
			}

			return defaults, nil
		}
	}

	defaults, ok := gadgetInfo.Defaults[si.SnapID]
	if !ok {
		return nil, state.ErrNoState
	}

	return defaults, nil
}

// GadgetConnections returns the interface connection instructions
// specified in the gadget. If gadget is absent it returns ErrNoState.
func GadgetConnections(st *state.State) ([]snap.GadgetConnection, error) {
	gadget, err := GadgetInfo(st)
	if err != nil {
		return nil, err
	}

	gadgetInfo, err := snap.ReadGadgetInfo(gadget, release.OnClassic)
	if err != nil {
		return nil, err
	}

	return gadgetInfo.Connections, nil
}
