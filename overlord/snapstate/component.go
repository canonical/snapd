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
	"errors"
	"fmt"
	"os"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

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

	componentTS, err := doInstallComponent(st, &snapst, compSetup, snapsup, "", "")
	if err != nil {
		return nil, err
	}

	return componentTS.taskSet(), nil
}

type componentInstallFlags struct {
	RemoveComponentPath        bool `json:"remove-component-path,omitempty"`
	JointSnapComponentsInstall bool `json:"joint-snap-components-install,omitempty"`
}

type componentInstallTaskSet struct {
	compSetupTask      *state.Task
	beforeLink         []*state.Task
	linkToHook         []*state.Task
	postOpHookAndAfter []*state.Task
}

func (c *componentInstallTaskSet) taskSet() *state.TaskSet {
	tasks := make([]*state.Task, 0, len(c.beforeLink)+len(c.linkToHook)+len(c.postOpHookAndAfter))
	tasks = append(tasks, c.beforeLink...)
	tasks = append(tasks, c.linkToHook...)
	tasks = append(tasks, c.postOpHookAndAfter...)

	ts := state.NewTaskSet(tasks...)
	ts.MarkEdge(c.compSetupTask, BeginEdge)

	return ts
}

// doInstallComponent might be called with the owner snap installed or not.
func doInstallComponent(st *state.State, snapst *SnapState, compSetup ComponentSetup,
	snapsup SnapSetup, snapSetupTaskID string, fromChange string) (componentInstallTaskSet, error) {

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
		prepare.Set("snap-setup", snapsup)
	}

	prev := prepare
	addTask := func(t *state.Task) {
		t.Set("component-setup-task", prepare.ID())
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		prev = t
	}

	componentTS := componentInstallTaskSet{
		compSetupTask: prepare,
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
	if !compSetup.JointSnapComponentsInstall {
		setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup component %q%s security profiles"), compSi.Component, revisionStr))
		componentTS.beforeLink = append(componentTS.beforeLink, setupSecurity)
		addTask(setupSecurity)
	}

	// finalize (sets SnapState)
	linkSnap := st.NewTask("link-component",
		fmt.Sprintf(i18n.G("Make component %q%s available to the system"),
			compSi.Component, revisionStr))
	componentTS.linkToHook = append(componentTS.linkToHook, linkSnap)
	addTask(linkSnap)

	var postOpHook *state.Task
	if !compInstalled {
		postOpHook = SetupInstallComponentHook(st, snapsup.InstanceName(), compSi.Component.ComponentName)
	} else {
		postOpHook = SetupPostRefreshComponentHook(st, snapsup.InstanceName(), compSi.Component.ComponentName)
	}
	componentTS.postOpHookAndAfter = append(componentTS.postOpHookAndAfter, postOpHook)
	addTask(postOpHook)

	if !compSetup.JointSnapComponentsInstall && compSetup.CompType == snap.KernelModulesComponent {
		kmodSetup := st.NewTask("prepare-kernel-modules-components",
			fmt.Sprintf(i18n.G("Prepare kernel-modules component %q%s"),
				compSi.Component, revisionStr))
		componentTS.postOpHookAndAfter = append(componentTS.postOpHookAndAfter, kmodSetup)
		addTask(kmodSetup)
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
		componentTS.postOpHookAndAfter = append(componentTS.postOpHookAndAfter, discardComp)
		addTask(discardComp)
	}

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
