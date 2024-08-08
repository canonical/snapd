// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store"
)

// InstallComponents installs all of the components in the given names list. The
// snap represented by info must already be installed, and all of the components
// in names should not be installed prior to calling this function.
//
// TODO:COMPS: respect the transaction that is passed to this function
func InstallComponents(ctx context.Context, st *state.State, names []string, info *snap.Info, opts Options) ([]*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, info.InstanceName(), &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, &snap.NotInstalledError{Snap: info.InstanceName()}
		}
		return nil, err
	}

	for _, comp := range names {
		if snapst.CurrentComponentSideInfo(naming.NewComponentRef(info.SnapName(), comp)) != nil {
			return nil, snap.AlreadyInstalledComponentError{Component: comp}
		}
	}

	compsups, err := componentSetupsForInstall(ctx, st, names, info, opts)
	if err != nil {
		return nil, err
	}

	snapsup := SnapSetup{
		Base:        info.Base,
		SideInfo:    &info.SideInfo,
		Channel:     info.Channel,
		Flags:       opts.Flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}

	setupSecurity := st.NewTask("setup-profiles",
		fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), info.InstanceName(), info.Revision))
	setupSecurity.Set("snap-setup", snapsup)

	var kmodSetup *state.Task
	if requiresKmodSetup(&snapst, compsups) {
		kmodSetup = st.NewTask("prepare-kernel-modules-components", fmt.Sprintf(
			i18n.G("Prepare kernel-modules components for %q%s"), info.InstanceName(), info.Revision,
		))
	}

	tss := make([]*state.TaskSet, 0, len(compsups))
	compSetupIDs := make([]string, 0, len(compsups))
	for _, compsup := range compsups {
		// since we are installing multiple components, we don't want to setup
		// the security profiles until the end
		compsup.MultiComponentInstall = true

		// here we share the setupSecurity and kmodSetup tasks between all of
		// the component task chains. this results in multiple parallel tasks
		// (one per copmonent) that have synchronization points at the
		// setupSecurity and kmodSetup tasks.
		componentTS, err := doInstallComponent(st, &snapst, compsup, snapsup, setupSecurity.ID(), setupSecurity, kmodSetup, opts.FromChange)
		if err != nil {
			return nil, err
		}

		compSetupIDs = append(compSetupIDs, componentTS.compSetupTaskID)
		tss = append(tss, componentTS.taskSet())
	}

	setupSecurity.Set("component-setup-tasks", compSetupIDs)

	ts := state.NewTaskSet(setupSecurity)
	if kmodSetup != nil {
		ts.AddTask(kmodSetup)
	}
	return append(tss, ts), nil
}

func componentSetupsForInstall(ctx context.Context, st *state.State, names []string, info *snap.Info, opts Options) ([]ComponentSetup, error) {
	var snapst SnapState
	err := Get(st, info.InstanceName(), &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, &snap.NotInstalledError{Snap: info.InstanceName()}
		}
		return nil, err
	}

	current, err := currentSnaps(st)
	if err != nil {
		return nil, err
	}

	// TODO:COMPS: figure out which user to use here
	user, err := userFromUserID(st, opts.UserID)
	if err != nil {
		return nil, err
	}

	action, err := installComponentAction(st, snapst, opts)
	if err != nil {
		return nil, err
	}

	refreshOpts, err := refreshOptions(st, &store.RefreshOptions{
		IncludeResources: true,
	})
	if err != nil {
		return nil, err
	}

	sto := Store(st, opts.DeviceCtx)
	st.Unlock()
	sars, _, err := sto.SnapAction(ctx, current, []*store.SnapAction{action}, nil, user, refreshOpts)
	st.Lock()
	if err != nil {
		return nil, err
	}

	if len(sars) != 1 {
		return nil, fmt.Errorf("internal error: expected exactly one snap action result, got %d", len(sars))
	}

	return componentTargetsFromActionResult("install", sars[0], names)
}

func installComponentAction(st *state.State, snapst SnapState, opts Options) (*store.SnapAction, error) {
	si := snapst.CurrentSideInfo()
	if si == nil {
		return nil, errors.New("internal error: cannot install components for a snap that is not installed")
	}

	if si.SnapID == "" {
		return nil, errors.New("internal error: cannot install components for a snap that is unknown to the store")
	}

	enforcedSetsFunc := cachedEnforcedValidationSets(st)

	// we send a refresh action, since that is what the store requested that
	// we do in this case
	action := &store.SnapAction{
		Action:          "refresh",
		SnapID:          si.SnapID,
		InstanceName:    snapst.InstanceName(),
		ResourceInstall: true,
	}

	// we send an action that contains the current channel and revision so
	// that we make sure to get back components that are compatible with the
	// currently installed snap
	revOpts := RevisionOptions{
		Channel:  snapst.TrackingChannel,
		Revision: snapst.Current,
	}

	// TODO:COMPS: handle validation sets here
	if err := completeStoreAction(action, revOpts, opts.Flags.IgnoreValidation, enforcedSetsFunc); err != nil {
		return nil, err
	}

	return action, nil
}

// InstallComponentPath returns a set of tasks for installing a snap component
// from a file path.
//
// Note that the state must be locked by the caller. The provided SideInfo can
// contain just a name which results in local sideloading of the component, or
// full metadata in which case the component will appear as installed from the
// store.
func InstallComponentPath(st *state.State, csi *snap.ComponentSideInfo, info *snap.Info,
	path string, flags Flags) (*state.TaskSet, error) {
	var snapst SnapState
	// owner snap must be already installed
	err := Get(st, info.InstanceName(), &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, &snap.NotInstalledError{Snap: info.InstanceName()}
		}
		return nil, err
	}

	// Read ComponentInfo and verify that the component is consistent with the
	// data in the snap info
	compInfo, _, err := backend.OpenComponentFile(path, info, csi)
	if err != nil {
		return nil, err
	}

	snapsup := SnapSetup{
		Base:        info.Base,
		SideInfo:    &info.SideInfo,
		Channel:     info.Channel,
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}
	compSetup := ComponentSetup{
		CompSideInfo: csi,
		CompType:     compInfo.Type,
		CompPath:     path,
		componentInstallFlags: componentInstallFlags{
			// The file passed around is temporary, make sure it gets removed.
			RemoveComponentPath: true,
		},
	}

	componentTS, err := doInstallComponent(st, &snapst, compSetup, snapsup, "", nil, nil, "")
	if err != nil {
		return nil, err
	}

	return componentTS.taskSet(), nil
}

type componentInstallFlags struct {
	RemoveComponentPath   bool `json:"remove-component-path,omitempty"`
	MultiComponentInstall bool `json:"joint-snap-components-install,omitempty"`
}

type componentInstallTaskSet struct {
	compSetupTaskID    string
	beforeLink         []*state.Task
	linkTask           *state.Task
	postOpHookAndAfter []*state.Task
	discardTask        *state.Task
}

func (c *componentInstallTaskSet) taskSet() *state.TaskSet {
	tasks := make([]*state.Task, 0, len(c.beforeLink)+1+len(c.postOpHookAndAfter)+1)
	tasks = append(tasks, c.beforeLink...)
	tasks = append(tasks, c.linkTask)
	tasks = append(tasks, c.postOpHookAndAfter...)
	if c.discardTask != nil {
		tasks = append(tasks, c.discardTask)
	}

	ts := state.NewTaskSet(tasks...)
	for _, t := range ts.Tasks() {
		if t.ID() == c.compSetupTaskID {
			ts.MarkEdge(t, BeginEdge)
		}
	}

	return ts
}

// doInstallComponent might be called with the owner snap installed or not.
func doInstallComponent(st *state.State, snapst *SnapState, compSetup ComponentSetup,
	snapsup SnapSetup, snapSetupTaskID string, setupSecurity, kmodSetup *state.Task, fromChange string) (componentInstallTaskSet, error) {

	// TODO check for experimental flag that will hide temporarily components

	snapSi := snapsup.SideInfo
	compSi := compSetup.CompSideInfo

	if snapst.IsInstalled() && !snapst.Active {
		return componentInstallTaskSet{}, fmt.Errorf("cannot install component %q for disabled snap %q",
			compSi.Component, snapSi.RealName)
	}

	// For the moment we consider the same conflicts as if the component
	// was actually the snap.
	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(),
		snapst, fromChange); err != nil {
		return componentInstallTaskSet{}, err
	}

	// Check if we already have the revision in the snaps folder (alters tasks).
	// Note that this will search for all snap revisions in the system.
	revisionIsPresent := snapst.IsComponentRevPresent(compSi)
	revisionStr := fmt.Sprintf(" (%s)", compSi.Revision)

	fromStore := compSetup.CompPath == "" && !revisionIsPresent

	var prepare *state.Task
	// if we have a local revision here we go back to that
	if fromStore {
		prepare = st.NewTask("download-component", fmt.Sprintf(i18n.G("Download component %q%s"), compSetup.ComponentName(), revisionStr))
	} else {
		prepare = st.NewTask("prepare-component", fmt.Sprintf(i18n.G("Prepare component %q%s"), compSetup.CompPath, revisionStr))
	}

	prepare.Set("component-setup", compSetup)

	if snapSetupTaskID != "" {
		prepare.Set("snap-setup-task", snapSetupTaskID)
	} else {
		snapSetupTaskID = prepare.ID()
		prepare.Set("snap-setup", snapsup)
	}

	prev := prepare
	addTask := func(t *state.Task) {
		t.Set("component-setup-task", prepare.ID())
		t.Set("snap-setup-task", snapSetupTaskID)
		t.WaitFor(prev)
		prev = t
	}

	componentTS := componentInstallTaskSet{
		compSetupTaskID: prepare.ID(),
	}

	componentTS.beforeLink = append(componentTS.beforeLink, prepare)

	if fromStore {
		validate := st.NewTask("validate-component", fmt.Sprintf(
			i18n.G("Fetch and check assertions for component %q%s"), compSetup.ComponentName(), revisionStr),
		)
		componentTS.beforeLink = append(componentTS.beforeLink, validate)
		addTask(validate)
	}

	// Task that copies the file and creates mount units
	if !revisionIsPresent {
		mount := st.NewTask("mount-component",
			fmt.Sprintf(i18n.G("Mount component %q%s"),
				compSi.Component, revisionStr))
		componentTS.beforeLink = append(componentTS.beforeLink, mount)
		addTask(mount)
	} else {
		if compSetup.RemoveComponentPath {
			// If the revision is local, we will not need the
			// temporary snap. This can happen when e.g.
			// side-loading a local revision again. The path is
			// only needed in the "mount-snap" handler and that is
			// skipped for local revisions.
			if err := os.Remove(compSetup.CompPath); err != nil {
				return componentInstallTaskSet{}, err
			}
		}
	}

	compInstalled := snapst.IsComponentInCurrentSeq(compSi.Component)

	if !snapsup.Revert && compInstalled {
		preRefreshHook := SetupPreRefreshComponentHook(st, snapsup.InstanceName(), compSi.Component.ComponentName)
		componentTS.beforeLink = append(componentTS.beforeLink, preRefreshHook)
		addTask(preRefreshHook)
	}

	changingSnapRev := snapst.IsInstalled() && snapst.Current != snapSi.Revision

	// note that we don't unlink the currect component if we're also changing
	// snap revisions while installing this component. that is because we don't
	// want to remove the component from the state of the previous snap revision
	// (for the purpose of something like a revert). additionally, this is
	// consistent with us keeping previous snap revisions mounted after changing
	// their revision. so we really only want to create this task if we are
	// replacing a component in the current snap revision
	if !changingSnapRev && compInstalled {
		unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
			"Make current revision for component %q unavailable"),
			compSi.Component))
		componentTS.beforeLink = append(componentTS.beforeLink, unlink)
		addTask(unlink)
	}

	// security
	if !compSetup.MultiComponentInstall && setupSecurity == nil {
		setupSecurity = st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup component %q%s security profiles"), compSi.Component, revisionStr))
		setupSecurity.Set("component-setup-task", prepare.ID())
		setupSecurity.Set("snap-setup-task", snapSetupTaskID)
		componentTS.beforeLink = append(componentTS.beforeLink, setupSecurity)
	}
	if setupSecurity != nil {
		// note that we don't use addTask here because this task is shared and
		// we don't want to add "component-setup-task" or "snap-setup-task" to
		// it
		setupSecurity.WaitFor(prev)
		prev = setupSecurity
	}

	// finalize (sets SnapState)
	linkSnap := st.NewTask("link-component",
		fmt.Sprintf(i18n.G("Make component %q%s available to the system"),
			compSi.Component, revisionStr))
	componentTS.linkTask = linkSnap
	addTask(linkSnap)

	var postOpHook *state.Task
	if !compInstalled {
		postOpHook = SetupInstallComponentHook(st, snapsup.InstanceName(), compSi.Component.ComponentName)
	} else {
		postOpHook = SetupPostRefreshComponentHook(st, snapsup.InstanceName(), compSi.Component.ComponentName)
	}
	componentTS.postOpHookAndAfter = append(componentTS.postOpHookAndAfter, postOpHook)
	addTask(postOpHook)

	if !compSetup.MultiComponentInstall && kmodSetup == nil && compSetup.CompType == snap.KernelModulesComponent {
		kmodSetup = st.NewTask("prepare-kernel-modules-components",
			fmt.Sprintf(i18n.G("Prepare kernel-modules component %q%s"),
				compSi.Component, revisionStr))
		kmodSetup.Set("component-setup-task", prepare.ID())
		kmodSetup.Set("snap-setup-task", snapSetupTaskID)
		componentTS.postOpHookAndAfter = append(componentTS.postOpHookAndAfter, kmodSetup)
	}
	if kmodSetup != nil {
		// note that we don't use addTask here because this task is shared and
		// we don't want to add "component-setup-task" or "snap-setup-task" to
		// it
		kmodSetup.WaitFor(prev)
		prev = kmodSetup
	}

	changingComponentRev := false
	if compInstalled {
		currentRev := snapst.CurrentComponentSideInfo(compSetup.CompSideInfo.Component).Revision
		changingComponentRev = currentRev != compSetup.CompSideInfo.Revision
	}

	// we can only discard the component if all of the following are true:
	// * we are not changing the snap revision
	// * we are actually changing the component revision (or it is not installed)
	// * the component is not used in any other sequence point
	canDiscardComponent := !changingSnapRev && changingComponentRev &&
		!snapst.IsCurrentComponentRevInAnyNonCurrentSeq(compSetup.CompSideInfo.Component)

	if canDiscardComponent {
		discardComp := st.NewTask("discard-component", fmt.Sprintf(i18n.G(
			"Discard previous revision for component %q"),
			compSi.Component))
		componentTS.discardTask = discardComp
		addTask(discardComp)
	}

	// TODO:COMPS: do we need to set restart boundaries here? (probably for
	// kernel-modules components if installed along the kernel)

	return componentTS, nil
}

type RemoveComponentsOpts struct {
	RefreshProfile bool
}

// RemoveComponents returns a taskset that removes the components in compName
// that belog to snapName.
func RemoveComponents(st *state.State, snapName string, compName []string, opts RemoveComponentsOpts) ([]*state.TaskSet, error) {
	var snapst SnapState
	err := Get(st, snapName, &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if !snapst.IsInstalled() {
		return nil, &snap.NotInstalledError{Snap: snapName, Rev: snap.R(0)}
	}

	info, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	var setupSecurity *state.Task
	if opts.RefreshProfile {
		revisionStr := fmt.Sprintf(" (%s)", info.Revision)
		setupSecurity = st.NewTask("setup-profiles",
			fmt.Sprintf(i18n.G("Setup snap %q%s security profiles"), snapName, revisionStr))
	}

	var tss []*state.TaskSet
	for _, comp := range compName {
		cref := naming.NewComponentRef(snapName, comp)
		compst := snapst.CurrentComponentState(cref)
		if compst == nil {
			return nil, &snap.ComponentNotInstalledError{
				NotInstalledError: snap.NotInstalledError{
					Snap: info.InstanceName(),
					Rev:  info.Revision,
				},
				Component: comp,
				CompRev:   snap.R(0),
			}
		}
		ts, err := removeComponentTasks(st, &snapst, compst, info, setupSecurity)
		if err != nil {
			return nil, err
		}
		tss = append(tss, ts)
	}

	if opts.RefreshProfile {
		tss = append(tss, state.NewTaskSet(setupSecurity))
	}

	return tss, nil
}

func removeComponentTasks(st *state.State, snapst *SnapState, compst *sequence.ComponentState, info *snap.Info, setupSecurity *state.Task) (*state.TaskSet, error) {
	instName := info.InstanceName()

	// For the moment we consider the same conflicts as if the component
	// was actually the snap.
	if err := CheckChangeConflict(st, instName, nil); err != nil {
		return nil, err
	}

	// TODO:COMPS: check if component is enforced by validation set (see snapstate.canRemove)

	snapSup := &SnapSetup{
		Base:        info.Base,
		SideInfo:    &info.SideInfo,
		Channel:     info.Channel,
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}
	compSetup := &ComponentSetup{
		CompSideInfo: compst.SideInfo,
		CompType:     compst.CompType,
	}

	// TODO:COMPS: Run component remove hook. This will change the first task run,
	// changing uses of unlink below to the new task.

	// Unlink component
	unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
		"Make current revision for component %q unavailable"),
		compst.SideInfo.Component))
	unlink.Set("component-setup", compSetup)
	unlink.Set("snap-setup", snapSup)

	var prev *state.Task
	tasks := []*state.Task{unlink}
	prev = unlink

	addTask := func(t *state.Task) {
		t.Set("component-setup-task", unlink.ID())
		t.Set("snap-setup-task", unlink.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
		prev = t
	}

	// For kernel-modules, regenerate drivers tree
	revisionStr := fmt.Sprintf(" (%s)", compst.SideInfo.Revision)
	if compst.CompType == snap.KernelModulesComponent {
		kmodSetup := st.NewTask("prepare-kernel-modules-components",
			fmt.Sprintf(i18n.G("Clear kernel-modules component %q%s"),
				compst.SideInfo.Component, revisionStr))
		addTask(kmodSetup)
	}

	// Refreshing the security profiles happens before discarding the
	// component file, as that task cannot be undone.
	if setupSecurity != nil {
		setupSecurity.WaitFor(prev)
		// We will be overwriting this object if removing multiple
		// components, but should be fine as the SnapSetup does not
		// change (snap still the same).
		setupSecurity.Set("snap-setup-task", unlink.ID())
		prev = setupSecurity
	}

	// Discard component if not used in other sequence points
	if !snapst.IsCurrentComponentRevInAnyNonCurrentSeq(compSetup.CompSideInfo.Component) {
		discardComp := st.NewTask("discard-component", fmt.Sprintf(i18n.G(
			"Discard previous revision for component %q"),
			compst.SideInfo.Component))
		addTask(discardComp)
	}

	return state.NewTaskSet(tasks...), nil
}
