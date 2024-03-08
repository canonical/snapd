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

	// Read ComponentInfo
	compInfo, _, err := backend.OpenComponentFile(path)
	if err != nil {
		return nil, err
	}

	// Check snap name matches
	if compInfo.Component.SnapName != info.SnapName() {
		return nil, fmt.Errorf(
			"component snap name %q does not match snap name %q",
			compInfo.Component.SnapName, info.RealName)
	}

	// Check that the component is specified in snap metadata
	comp, ok := info.Components[csi.Component.ComponentName]
	if !ok {
		return nil, fmt.Errorf("%q is not a component for snap %q",
			csi.Component.ComponentName, info.RealName)
	}
	// and that types in snap and component match
	if comp.Type != compInfo.Type {
		return nil,
			fmt.Errorf("inconsistent component type (%q in snap, %q in component)",
				comp.Type, compInfo.Type)
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
	}
	// The file passed around is temporary, make sure it gets removed.
	// TODO probably this should be part of a flags type in the future.
	removeComponentPath := true
	return doInstallComponent(st, &snapst, compSetup, snapsup, path, removeComponentPath, "")
}

// doInstallComponent might be called with the owner snap installed or not.
func doInstallComponent(st *state.State, snapst *SnapState, compSetup *ComponentSetup,
	snapsup *SnapSetup, path string, removeComponentPath bool, fromChange string) (*state.TaskSet, error) {

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
	revisionIsPresent := snapst.IsComponentRevPresent(compSi) == true
	revisionStr := fmt.Sprintf(" (%s)", compSi.Revision)

	var prepare, prev *state.Task
	// if we have a local revision here we go back to that
	if path != "" || revisionIsPresent {
		prepare = st.NewTask("prepare-component",
			fmt.Sprintf(i18n.G("Prepare component %q%s"),
				path, revisionStr))
	} else {
		// TODO implement download-component
		return nil, fmt.Errorf("download-component not implemented yet")
	}
	prepare.Set("component-setup", compSetup)
	prepare.Set("snap-setup", snapsup)

	tasks := []*state.Task{prepare}
	prev = prepare

	addTask := func(t *state.Task) {
		t.Set("component-setup-task", prepare.ID())
		t.Set("snap-setup-task", prepare.ID())
		t.WaitFor(prev)
		tasks = append(tasks, t)
		prev = t
	}

	// TODO task to fetch and check assertions for component if from store
	// (equivalent to "validate-snap")

	// Task that copies the file and creates mount units
	if !revisionIsPresent {
		mount := st.NewTask("mount-component",
			fmt.Sprintf(i18n.G("Mount component %q%s"),
				compSi.Component, revisionStr))
		addTask(mount)
	} else {
		if removeComponentPath {
			// If the revision is local, we will not need the
			// temporary snap. This can happen when e.g.
			// side-loading a local revision again. The path is
			// only needed in the "mount-snap" handler and that is
			// skipped for local revisions.
			if err := os.Remove(path); err != nil {
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

	// We might be replacing a component if a local install, otherwise
	// this is not really possible.
	compInstalled := snapst.IsComponentInCurrentSeq(compSi.Component)
	if compInstalled {
		unlink := st.NewTask("unlink-current-component", fmt.Sprintf(i18n.G(
			"Make current revision for component %q unavailable"),
			compSi.Component))
		addTask(unlink)
	}

	// finalize (sets SnapState)
	linkSnap := st.NewTask("link-component",
		fmt.Sprintf(i18n.G("Make component %q%s available to the system"),
			compSi.Component, revisionStr))
	addTask(linkSnap)

	installSet := state.NewTaskSet(tasks...)
	installSet.MarkEdge(prepare, BeginEdge)
	installSet.MarkEdge(linkSnap, MaybeRebootEdge)

	// TODO do we need to set restart boundaries here? (probably
	// for kernel-modules components if installed along the kernel)

	return installSet, nil
}
