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

	"github.com/snapcore/snapd/asserts/snapasserts"
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
func InstallComponents(
	ctx context.Context,
	st *state.State,
	names []string,
	info *snap.Info,
	vsets *snapasserts.ValidationSets,
	opts Options,
) ([]*state.TaskSet, error) {
	if err := opts.setDefaultLane(st); err != nil {
		return nil, err
	}

	if err := setDefaultSnapstateOptions(st, &opts); err != nil {
		return nil, err
	}

	var snapst SnapState
	err := Get(st, info.InstanceName(), &snapst)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, &snap.NotInstalledError{Snap: info.InstanceName()}
		}
		return nil, err
	}

	if vsets == nil {
		// we only check for already installed components when no validation
		// sets are provided, since this will allow us to refresh and install
		// new components at the same time when resolving validation sets
		for _, comp := range names {
			if snapst.CurrentComponentSideInfo(naming.NewComponentRef(info.SnapName(), comp)) != nil {
				return nil, snap.AlreadyInstalledComponentError{Component: comp}
			}
		}
	}

	revOpts := RevisionOptions{
		Revision:       snapst.Current,
		Channel:        snapst.TrackingChannel,
		ValidationSets: vsets,
	}

	if err := revOpts.initializeValidationSets(cachedEnforcedValidationSets(st), opts); err != nil {
		return nil, err
	}

	compsups, err := componentSetupsForInstall(ctx, st, names, snapst, revOpts, opts)
	if err != nil {
		return nil, err
	}

	// TODO:COMPS: verify validation sets here

	snapsup := SnapSetup{
		Base:                        info.Base,
		SideInfo:                    &info.SideInfo,
		Channel:                     info.Channel,
		Flags:                       opts.Flags.ForSnapSetup(),
		Type:                        info.Type(),
		Version:                     info.Version,
		PlugsOnly:                   len(info.Slots) == 0,
		InstanceKey:                 info.InstanceKey,
		ComponentExclusiveOperation: true,
	}

	setupSecurity := st.NewTask("setup-profiles",
		fmt.Sprintf(i18n.G("Setup snap %q (%s) security profiles"), info.InstanceName(), info.Revision))
	setupSecurity.Set("snap-setup", snapsup)

	var kmodSetup *state.Task
	if requiresKmodSetup(&snapst, compsups) {
		kmodSetup = st.NewTask("prepare-kernel-modules-components", fmt.Sprintf(
			i18n.G("Prepare kernel-modules components for %q%s"), info.InstanceName(), info.Revision,
		))
		kmodSetup.Set("snap-setup-task", setupSecurity.ID())
	}

	lane := generateLane(st, opts)

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
		cts, ts, err := doInstallComponent(st, &snapst, compsup, snapsup, setupSecurity, setupSecurity, kmodSetup, opts.FromChange)
		if err != nil {
			return nil, err
		}

		compSetupIDs = append(compSetupIDs, cts.compSetupTaskID)

		ts.JoinLane(lane)
		tss = append(tss, ts)
	}

	setupSecurity.Set("component-setup-tasks", compSetupIDs)

	ts := state.NewTaskSet(setupSecurity)
	ts.MarkEdge(setupSecurity, SnapSetupEdge)

	if kmodSetup != nil {
		ts.AddTask(kmodSetup)
	}

	// note that this must come after all tasks are added to the task set
	ts.JoinLane(lane)

	return append(tss, ts), nil
}

func componentSetupsForInstall(ctx context.Context, st *state.State, names []string, snapst SnapState, revOpts RevisionOptions, opts Options) ([]ComponentSetup, error) {
	if len(names) == 0 {
		return nil, nil
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

	action, err := installComponentAction(snapst, revOpts, opts)
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

// installComponentAction returns a store action that is used to get a list of
// components that are available in the store.
func installComponentAction(snapst SnapState, revOpts RevisionOptions, opts Options) (*store.SnapAction, error) {
	if revOpts.Revision.Unset() {
		return nil, errors.New("internal error: must specify snap revision when installing only components")
	}

	index := snapst.LastIndex(revOpts.Revision)
	if index == -1 {
		return nil, fmt.Errorf("internal error: cannot find snap revision %s in sequence", revOpts.Revision)
	}
	si := snapst.Sequence.SideInfos()[index]

	if si.SnapID == "" {
		return nil, errors.New("internal error: cannot install components for a snap that is unknown to the store")
	}

	// we send a refresh action, since that is what the store requested that
	// we do in this case
	action := &store.SnapAction{
		Action:          "refresh",
		SnapID:          si.SnapID,
		InstanceName:    snapst.InstanceName(),
		ResourceInstall: true,
	}

	if err := completeStoreAction(action, revOpts, opts.Flags.IgnoreValidation); err != nil {
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
	path string, opts Options) (*state.TaskSet, error) {
	if err := opts.setDefaultLane(st); err != nil {
		return nil, err
	}

	if err := setDefaultSnapstateOptions(st, &opts); err != nil {
		return nil, err
	}

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
		Base:                        info.Base,
		SideInfo:                    &info.SideInfo,
		Channel:                     info.Channel,
		Flags:                       opts.Flags.ForSnapSetup(),
		Type:                        info.Type(),
		Version:                     info.Version,
		PlugsOnly:                   len(info.Slots) == 0,
		InstanceKey:                 info.InstanceKey,
		ComponentExclusiveOperation: true,
	}
	compSetup := ComponentSetup{
		CompSideInfo: csi,
		CompType:     compInfo.Type,
		CompPath:     path,
		ComponentInstallFlags: ComponentInstallFlags{
			// The file passed around is temporary, make sure it gets removed.
			RemoveComponentPath: true,
		},
	}

	cts, ts, err := doInstallComponent(st, &snapst, compSetup, snapsup, nil, nil, nil, "")
	if err != nil {
		return nil, err
	}

	// TODO:COMPS: instead of doing this, we should convert this function to
	// operate on multiple components so that it works like InstallComponents.
	// this would improve performance, especially in the case of kernel module
	// components.
	begin, err := ts.Edge(BeginEdge)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find begin edge on component install task set: %v", err)
	}

	begin.Set("component-setup-tasks", []string{cts.compSetupTaskID})
	ts.MarkEdge(begin, SnapSetupEdge)

	ts.JoinLane(generateLane(st, opts))

	return ts, nil
}

type ComponentInstallFlags struct {
	RemoveComponentPath   bool `json:"remove-component-path,omitempty"`
	MultiComponentInstall bool `json:"joint-snap-components-install,omitempty"`
}

type componentInstallTaskSet struct {
	compSetupTaskID                     string
	beforeLocalSystemModificationsTasks []*state.Task
	beforeLinkTasks                     []*state.Task
	maybeLinkTask                       *state.Task
	postHookToDiscardTasks              []*state.Task
	maybeDiscardTask                    *state.Task
}

type componentInstallBuilder struct {
	compsup ComponentSetup
	snapsup SnapSetup
	snapst  SnapState

	// compsupTask is the task that carries the ComponentSetup for this
	// operation. Can optionally be injected by the caller.
	compsupTask *state.Task

	// snapsupTask is the task that carries the SnapSetup for this operation.
	// Can optionally be injected by the caller.
	snapsupTask *state.Task

	// setupSecurityTask is the "setup-security" task that might be injected by the
	// caller.
	setupSecurityTask *state.Task

	// kmodSetupTask is the "prepare-kernel-modules-components" task that might be
	// injected by the caller.
	kmodSetupTask *state.Task
}

func newComponentInstallTaskBuilder(
	st *state.State,
	snapsup SnapSetup,
	snapst SnapState,
	compsup ComponentSetup,
	fromChange string,
) (*componentInstallBuilder, error) {
	if compsup.SkipAssertionsDownload {
		return nil, errors.New("internal error: component setup cannot have SkipFetchingAssertions set by caller")
	}

	// if we're doing a revert, we shouldn't attempt to fetch assertions from
	// the store again.
	//
	// if we're installing a component from somewhere on disk, we can't reach
	// out to the store. thus, try and validate the component with what we
	// already have.
	compsup.SkipAssertionsDownload = snapsup.Revert || compsup.CompPath != ""

	if err := ensureSnapAndComponentsAssertionStatus(
		*snapsup.SideInfo, []snap.ComponentSideInfo{*compsup.CompSideInfo},
	); err != nil {
		return nil, err
	}

	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf(
			"cannot install component %q for disabled snap %q",
			compsup.CompSideInfo.Component, snapsup.SideInfo.RealName,
		)
	}

	// we consider the same conflicts as if the component was actually the snap.
	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(), &snapst, fromChange); err != nil {
		return nil, err
	}

	return &componentInstallBuilder{
		snapsup: snapsup,
		snapst:  snapst,
		compsup: compsup,
	}, nil
}

func (cb *componentInstallBuilder) BeforeLocalSystemMod(st *state.State, s *span) error {
	// Check if we already have the revision in the snaps folder (alters tasks).
	// Note that this will search for all snap revisions in the system.
	revisionIsPresent := cb.snapst.IsComponentRevPresent(cb.compsup.CompSideInfo)
	revisionStr := fmt.Sprintf(" (%s)", cb.compsup.CompSideInfo.Revision)
	needsDownload := cb.compsup.CompPath == "" && !revisionIsPresent

	var prepare *state.Task
	if needsDownload {
		// if we have a local revision here we go back to that
		prepare = st.NewTask("download-component", fmt.Sprintf(
			i18n.G("Download component %q%s"), cb.compsup.ComponentName(), revisionStr))
	} else {
		prepare = st.NewTask("prepare-component", fmt.Sprintf(
			i18n.G("Prepare component %q%s"), cb.compsup.CompPath, revisionStr))
	}

	if cb.compsupTask != nil {
		prepare.Set("component-setup-task", cb.compsupTask.ID())
	} else {
		prepare.Set("component-setup", cb.compsup)
		prepare.Set("component-setup-task", prepare.ID())
		cb.compsupTask = prepare
	}

	if cb.snapsupTask != nil {
		prepare.Set("snap-setup-task", cb.snapsupTask.ID())
	} else {
		prepare.Set("snap-setup", cb.snapsup)
		prepare.Set("snap-setup-task", prepare.ID())
		cb.snapsupTask = prepare
	}

	// let the task builder know that future tasks should have this metadata
	// attached to them
	s.SetMetadata(map[string]any{
		"component-setup-task": cb.compsupTask.ID(),
		"snap-setup-task":      cb.snapsupTask.ID(),
	})

	// prepare already has the needed metadata attached to it
	s.AddWithoutMetadata(prepare)
	s.UpdateEdge(prepare, BeginEdge)
	s.UpdateEdge(prepare, LastBeforeLocalModificationsEdge)

	// if the component we're installing has a revision from the store, then we
	// need to validate it. note that we will still run this task even if we're
	// reusing an already installed component, since we will most likely need to
	// fetch a new snap-resource-pair assertion. note that the behavior of
	// validate-component is dependent on ComponentSetup.SkipAssertionsDownload.
	if cb.compsup.Revision().Store() {
		validate := st.NewTask("validate-component", fmt.Sprintf(
			i18n.G("Fetch and check assertions for component %q%s"), cb.compsup.ComponentName(), revisionStr))
		s.Add(validate)
		s.UpdateEdge(validate, LastBeforeLocalModificationsEdge)
	}

	return nil
}

func (cb *componentInstallBuilder) BeforeLink(st *state.State, s *span) error {
	csi := cb.compsup.CompSideInfo
	si := cb.snapsup.SideInfo

	revisionIsPresent := cb.snapst.IsComponentRevPresent(csi)
	revisionStr := fmt.Sprintf(" (%s)", csi.Revision)

	// Task that copies the file and creates mount units
	if !revisionIsPresent {
		mount := st.NewTask("mount-component", fmt.Sprintf(i18n.G("Mount component %q%s"), csi.Component, revisionStr))
		s.Add(mount)
	} else if cb.compsup.RemoveComponentPath {
		// If the revision is local, we will not need the temporary snap. This can happen when e.g.
		// side-loading a local revision again. The path is only needed in the "mount-snap" handler
		// and that is skipped for local revisions.
		if err := os.Remove(cb.compsup.CompPath); err != nil {
			return err
		}
	}

	installed := cb.snapst.IsComponentInCurrentSeq(csi.Component)

	if !cb.snapsup.Revert && installed {
		preRefreshHook := SetupPreRefreshComponentHook(st, cb.snapsup.InstanceName(), csi.Component.ComponentName)
		s.Add(preRefreshHook)
	}

	changingSnapRev := cb.snapst.IsInstalled() && cb.snapst.Current != si.Revision

	// note that we don't unlink the currect component if we're also changing
	// snap revisions while installing this component. that is because we don't
	// want to remove the component from the state of the previous snap revision
	// (for the purpose of something like a revert). additionally, this is
	// consistent with us keeping previous snap revisions mounted after changing
	// their revision. so we really only want to create this task if we are
	// replacing a component in the current snap revision
	if !changingSnapRev && installed {
		unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G("Make current revision for component %q unavailable"), csi.Component))
		s.Add(unlink)
	}

	// (MultiComponentInstall && securityTask == nil) results in the absence of
	// a setup-profiles task. this happens during snap installation, where we
	// reuse the snap's setup-profiles task
	if !cb.compsup.MultiComponentInstall && cb.setupSecurityTask == nil {
		setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup component %q%s security profiles"), csi.Component, revisionStr))
		s.Add(setupSecurity)
	} else if cb.setupSecurityTask != nil {
		// pre-existing tasks don't get metadata added to them, nor do they get
		// added to the task set. just set up the dependency
		s.Splice(cb.setupSecurityTask)
	}

	return nil
}

func (cb *componentInstallBuilder) PostHookToBeforeDiscard(st *state.State, s *span) error {
	csi := cb.compsup.CompSideInfo
	revisionStr := fmt.Sprintf(" (%s)", csi.Revision)

	installed := cb.snapst.IsComponentInCurrentSeq(csi.Component)

	if !installed {
		hook := SetupInstallComponentHook(st, cb.snapsup.InstanceName(), csi.Component.ComponentName)
		s.Add(hook)
	} else {
		hook := SetupPostRefreshComponentHook(st, cb.snapsup.InstanceName(), csi.Component.ComponentName)
		s.Add(hook)
	}

	// kernel-modules preparation when not handled by a shared task
	if !cb.compsup.MultiComponentInstall && cb.kmodSetupTask == nil && cb.compsup.CompType == snap.KernelModulesComponent {
		cb.kmodSetupTask = st.NewTask("prepare-kernel-modules-components",
			fmt.Sprintf(i18n.G("Prepare kernel-modules component %q%s"), csi.Component, revisionStr))
		s.Add(cb.kmodSetupTask)
	} else if cb.kmodSetupTask != nil {
		// pre-existing tasks don't get metadata added to them, nor do they get
		// added to the task set. just set up the dependency
		s.Splice(cb.kmodSetupTask)
	}

	return nil
}

func (cb *componentInstallBuilder) build(st *state.State) (componentInstallTaskSet, *state.TaskSet, error) {
	b := builder{
		ts: state.NewTaskSet(),
	}

	beforeLocalSystemMods := b.NewSpan()
	if err := cb.BeforeLocalSystemMod(st, &beforeLocalSystemMods); err != nil {
		return componentInstallTaskSet{}, nil, err
	}

	beforeLink := b.NewSpan()
	if err := cb.BeforeLink(st, &beforeLink); err != nil {
		return componentInstallTaskSet{}, nil, err
	}

	csi := cb.compsup.CompSideInfo

	var maybeLink *state.Task
	if !cb.snapsup.Revert {
		// finalize (sets SnapState). if we're reverting, there isn't anything to
		// change in SnapState regarding the component
		maybeLink = st.NewTask(
			"link-component", fmt.Sprintf(
				i18n.G("Make component %q (%s) available to the system"), csi.Component, csi.Revision,
			),
		)
		b.Add(maybeLink)
	}

	postOpHookToBeforeDiscard := b.NewSpan()
	if err := cb.PostHookToBeforeDiscard(st, &postOpHookToBeforeDiscard); err != nil {
		return componentInstallTaskSet{}, nil, err
	}

	var maybeDiscard *state.Task
	if canDiscardComponent(cb.compsup, cb.snapsup, cb.snapst) {
		// we can only discard the component if all of the following are true:
		// * we are not changing the snap revision
		// * we are actually changing the component revision (or it is not installed)
		// * the component is not used in any other sequence point
		maybeDiscard = st.NewTask("discard-component", fmt.Sprintf(i18n.G("Discard previous revision for component %q"), csi.Component))
		b.Add(maybeDiscard)
	}

	return componentInstallTaskSet{
		compSetupTaskID:                     cb.compsupTask.ID(),
		beforeLocalSystemModificationsTasks: beforeLocalSystemMods.tasks,
		beforeLinkTasks:                     beforeLink.tasks,
		maybeLinkTask:                       maybeLink,
		postHookToDiscardTasks:              postOpHookToBeforeDiscard.tasks,
		maybeDiscardTask:                    maybeDiscard,
	}, b.ts, nil
}

func canDiscardComponent(compsup ComponentSetup, snapsup SnapSetup, snapst SnapState) bool {
	si := snapsup.SideInfo
	csi := compsup.CompSideInfo

	changingComponentRev := false
	installed := snapst.IsComponentInCurrentSeq(csi.Component)

	if installed {
		currentRev := snapst.CurrentComponentSideInfo(csi.Component).Revision
		changingComponentRev = currentRev != csi.Revision
	}

	changingSnapRev := snapst.IsInstalled() && snapst.Current != si.Revision
	canDiscardComponent := !changingSnapRev && changingComponentRev &&
		!snapst.IsCurrentComponentRevInAnyNonCurrentSeq(csi.Component)

	return canDiscardComponent
}

// doInstallComponent might be called with the owner snap installed or not.
func doInstallComponent(
	st *state.State,
	snapst *SnapState,
	compsup ComponentSetup,
	snapsup SnapSetup,
	snapsupTask *state.Task,
	setupSecurity, kmodSetup *state.Task,
	fromChange string,
) (componentInstallTaskSet, *state.TaskSet, error) {
	cb, err := newComponentInstallTaskBuilder(st, snapsup, *snapst, compsup, fromChange)
	if err != nil {
		return componentInstallTaskSet{}, nil, err
	}

	// we inject some tasks here. the builder considers these when building the
	// full chain and splices them into the correct places. if they're nil, then
	// the builder will create them on demand.
	cb.setupSecurityTask = setupSecurity
	cb.kmodSetupTask = kmodSetup
	cb.snapsupTask = snapsupTask

	cts, ts, err := cb.build(st)
	if err != nil {
		return componentInstallTaskSet{}, nil, err
	}

	return cts, ts, nil
}

type RemoveComponentsOpts struct {
	RefreshProfile bool
	FromChange     string
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
		ts, err := removeComponentTasks(st, &snapst, compst, info, setupSecurity, opts.FromChange)
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

func removeComponentTasks(st *state.State, snapst *SnapState, compst *sequence.ComponentState, info *snap.Info, setupSecurity *state.Task, fromChange string) (*state.TaskSet, error) {
	instName := info.InstanceName()

	// For the moment we consider the same conflicts as if the component
	// was actually the snap.
	if err := checkChangeConflictIgnoringOneChange(st, instName, nil, fromChange); err != nil {
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

	removeHook := SetupRemoveComponentHook(st, instName, compst.SideInfo.Component.ComponentName)
	removeHook.Set("component-setup", compSetup)
	removeHook.Set("snap-setup", snapSup)

	setupTask, prev := removeHook, removeHook
	tasks := []*state.Task{removeHook}
	addTask := func(t *state.Task) {
		t.Set("component-setup-task", setupTask.ID())
		t.Set("snap-setup-task", setupTask.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
		prev = t
	}

	// Unlink component
	unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
		"Make current revision for component %q unavailable"),
		compst.SideInfo.Component))
	addTask(unlink)

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
		setupSecurity.Set("snap-setup-task", setupTask.ID())
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
