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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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

	snapsup := &SnapSetup{
		Base:        info.Base,
		SideInfo:    &info.SideInfo,
		Channel:     info.Channel,
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}
	compSetup := &ComponentSetup{
		CompSideInfo: csi,
		CompType:     compInfo.Type,
		CompPath:     path,
		componentInstallFlags: componentInstallFlags{
			// The file passed around is temporary, make sure it gets removed.
			RemoveComponentPath: true,
		},
	}

	return doInstallComponent(st, &snapst, compSetup, snapsup, "")
}

type componentInstallFlags struct {
	RemoveComponentPath bool `json:"remove-component-path,omitempty"`
	SkipProfiles        bool `json:"skip-profiles,omitempty"`
}

// doInstallComponent might be called with the owner snap installed or not.
func doInstallComponent(st *state.State, snapst *SnapState, compSetup *ComponentSetup,
	snapsup *SnapSetup, fromChange string) (*state.TaskSet, error) {

	// TODO check for experimental flag that will hide temporarily components

	snapSi := snapsup.SideInfo
	compSi := compSetup.CompSideInfo

	if snapst.IsInstalled() && !snapst.Active {
		return nil, fmt.Errorf("cannot install component %q for disabled snap %q",
			compSi.Component, snapSi.RealName)
	}

	// For the moment we consider the same conflicts as if the component
	// was actually the snap.
	if err := checkChangeConflictIgnoringOneChange(st, snapsup.InstanceName(),
		snapst, fromChange); err != nil {
		return nil, err
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
	prepare.Set("snap-setup", snapsup)

	tasks := []*state.Task{prepare}
	prev := prepare

	addTask := func(t *state.Task) {
		t.Set("component-setup-task", prepare.ID())
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
		prev = t
	}

	if fromStore {
		validate := st.NewTask("validate-component", fmt.Sprintf(
			i18n.G("Fetch and check assertions for component %q%s"), compSetup.ComponentName(), revisionStr),
		)
		addTask(validate)
	}

	// Task that copies the file and creates mount units
	if !revisionIsPresent {
		mount := st.NewTask("mount-component",
			fmt.Sprintf(i18n.G("Mount component %q%s"),
				compSi.Component, revisionStr))
		addTask(mount)
	} else {
		if compSetup.RemoveComponentPath {
			// If the revision is local, we will not need the
			// temporary snap. This can happen when e.g.
			// side-loading a local revision again. The path is
			// only needed in the "mount-snap" handler and that is
			// skipped for local revisions.
			if err := os.Remove(compSetup.CompPath); err != nil {
				return nil, err
			}
		}
	}

	// TODO hooks for components

	if compSetup.CompType == snap.KernelModulesComponent {
		kmodSetup := st.NewTask("prepare-kernel-modules-components",
			fmt.Sprintf(i18n.G("Prepare kernel-modules component %q%s"),
				compSi.Component, revisionStr))
		addTask(kmodSetup)
	}

	// We might be replacing a component
	compInstalled := snapst.IsComponentInCurrentSeq(compSi.Component)
	if compInstalled {
		unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
			"Make current revision for component %q unavailable"),
			compSi.Component))
		addTask(unlink)
	}

	// security
	if !compSetup.SkipProfiles {
		setupSecurity := st.NewTask("setup-profiles", fmt.Sprintf(i18n.G("Setup component %q%s security profiles"), compSi.Component, revisionStr))
		addTask(setupSecurity)
	}

	// finalize (sets SnapState)
	linkSnap := st.NewTask("link-component",
		fmt.Sprintf(i18n.G("Make component %q%s available to the system"),
			compSi.Component, revisionStr))
	addTask(linkSnap)

	// clean-up previous revision of the component if present and
	// not used in previous sequence points
	if compInstalled &&
		!snapst.IsCurrentComponentRevInAnyNonCurrentSeq(compSetup.CompSideInfo.Component) {

		discardComp := st.NewTask("discard-component", fmt.Sprintf(i18n.G(
			"Discard previous revision for component %q"),
			compSi.Component))
		addTask(discardComp)
	}

	installSet := state.NewTaskSet(tasks...)
	installSet.MarkEdge(prepare, BeginEdge)
	installSet.MarkEdge(linkSnap, MaybeRebootEdge)

	// TODO do we need to set restart boundaries here? (probably
	// for kernel-modules components if installed along the kernel)

	return installSet, nil
}
