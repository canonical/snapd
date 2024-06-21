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
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
	"gopkg.in/tomb.v2"
)

// TaskComponentSetup returns the ComponentSetup and SnapSetup with task params hold
// by or referred to by the task.
func TaskComponentSetup(t *state.Task) (*ComponentSetup, *SnapSetup, error) {
	snapsup, err := TaskSnapSetup(t)
	if err != nil {
		return nil, nil, err
	}

	var compSetup ComponentSetup
	err = t.Get("component-setup", &compSetup)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}
	if err == nil {
		return &compSetup, snapsup, nil
	}

	var id string
	err = t.Get("component-setup-task", &id)
	if err != nil {
		return nil, nil, err
	}

	ts := t.State().Task(id)
	if ts == nil {
		return nil, nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	if err := ts.Get("component-setup", &compSetup); err != nil {
		return nil, nil, err
	}
	return &compSetup, snapsup, nil
}

func compSetupAndState(t *state.Task) (*ComponentSetup, *SnapSetup, *SnapState, error) {
	csup, ssup, err := TaskComponentSetup(t)
	if err != nil {
		return nil, nil, nil, err
	}
	var snapst SnapState
	err = Get(t.State(), ssup.InstanceName(), &snapst)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, nil, err
	}
	return csup, ssup, &snapst, nil
}

// componentSetupTask returns the task that contains the ComponentSetup
// identified by the component-setup-task contained by task t, or directly it
// returns t if it contains a ComponentSetup.
func componentSetupTask(t *state.Task) (*state.Task, error) {
	if t.Has("component-setup") {
		return t, nil
	} else {
		// this task isn't the component-setup-task, so go get that and
		// write to that one
		var id string
		err := t.Get("component-setup-task", &id)
		if err != nil {
			return nil, err
		}

		ts := t.State().Task(id)
		if ts == nil {
			return nil, fmt.Errorf("internal error: tasks are being pruned")
		}
		return ts, nil
	}
}

func (m *SnapManager) doPrepareComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	compSetup, _, snapSt, err := compSetupAndState(t)
	if err != nil {
		return err
	}

	if compSetup.Revision().Unset() {
		// This is a local installation, assign -1 to the revision if
		// no other local revision for the component is found, or
		// current more negative local revision decremented by one
		// otherwise.
		current := snapSt.LocalComponentRevision(compSetup.CompSideInfo.Component.ComponentName)
		compSetup.CompSideInfo.Revision = snap.R(current.N - 1)
	}

	t.Set("component-setup", compSetup)
	return nil
}

func (m *SnapManager) doDownloadComponent(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	compsup, snapsup, err := TaskComponentSetup(t)
	if err != nil {
		return err
	}

	if compsup.CompPath != "" {
		return fmt.Errorf("internal error: cannot download component %q that specifies a local file path", compsup.ComponentName())
	}

	if compsup.DownloadInfo == nil {
		return fmt.Errorf("internal error: cannot download component %q that does not specify download information", compsup.ComponentName())
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	user, err := userFromUserID(st, snapsup.UserID)
	if err != nil {
		return fmt.Errorf("cannot get user for user ID %d: %w", snapsup.UserID, err)
	}

	var rate int64
	if snapsup.IsAutoRefresh {
		rate = autoRefreshRateLimited(st)
	}

	cpi := snap.MinimalComponentContainerPlaceInfo(
		compsup.ComponentName(), compsup.CompSideInfo.Revision,
		snapsup.InstanceName(),
	)

	// TODO: to be consistent with snaps, this should be able to point somewhere
	// else, based on a path that is in the compsup. this would be used for
	// creating new recovery systems, like it is now for snaps
	target := cpi.MountFile()

	sto := Store(st, deviceCtx)
	meter := NewTaskProgressAdapterUnlocked(t)
	perf := state.TimingsForTask(t)

	st.Unlock()
	timings.Run(perf, "download", fmt.Sprintf("download component %q", compsup.ComponentName()), func(timings.Measurer) {
		compRef := compsup.CompSideInfo.Component.String()
		opts := &store.DownloadOptions{
			Scheduled: snapsup.IsAutoRefresh,
			RateLimit: rate,
		}

		err = sto.Download(tomb.Context(nil), compRef, target, compsup.DownloadInfo, meter, user, opts)
	})
	st.Lock()
	if err != nil {
		return fmt.Errorf("cannot download component %q: %w", compsup.ComponentName(), err)
	}

	// update component path for all the future tasks
	compsup.CompPath = target
	t.Set("component-setup", compsup)

	perf.Save(st)

	return nil
}

func (m *SnapManager) doMountComponent(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	perfTimings := state.TimingsForTask(t)
	compSetup, snapsup, err := TaskComponentSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	st.Lock()
	deviceCtx, err := DeviceCtx(t.State(), t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	// TODO we might want a checkComponents doing checks for some
	// component types (see checkSnap and checkSnapCallbacks slice)

	csi := compSetup.CompSideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(compSetup.ComponentName(),
		csi.Revision, snapsup.InstanceName())

	defer func() {
		st.Lock()
		defer st.Unlock()

		if err == nil {
			return
		}

		// RemoveComponentDir is idempotent so it's ok to always
		// call it in the cleanup path.
		if err := m.backend.RemoveComponentDir(cpi); err != nil {
			t.Errorf("cannot cleanup partial setup component %q: %v",
				csi.Component, err)
		}
	}()

	pm := NewTaskProgressAdapterUnlocked(t)
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-component",
		fmt.Sprintf("setup component %q", csi.Component),
		func(timings.Measurer) {
			installRecord, err = m.backend.SetupComponent(
				compSetup.CompPath,
				cpi,
				deviceCtx,
				pm)
		})
	if err != nil {
		return err
	}

	// double check that the component is mounted
	var readInfoErr error
	for i := 0; i < 10; i++ {
		compMntDir := cpi.MountDir()
		_, readInfoErr = readComponentInfo(compMntDir, nil, nil)
		if readInfoErr == nil {
			logger.Debugf("component %q (%v) available at %q",
				csi.Component, compSetup.Revision(), compMntDir)
			break
		}
		// snap not found, seems is not mounted yet
		time.Sleep(mountPollInterval)
	}
	if readInfoErr != nil {
		timings.Run(perfTimings, "undo-setup-component",
			fmt.Sprintf("Undo setup of component %q", csi.Component),
			func(timings.Measurer) {
				err = m.backend.UndoSetupComponent(cpi,
					installRecord, deviceCtx, pm)
			})
		if err != nil {
			st.Lock()
			t.Errorf("cannot undo partial setup of component %q: %v",
				csi.Component, err)
			st.Unlock()
		}

		return fmt.Errorf("expected component %q revision %v to be mounted but is not: %w",
			csi.Component, compSetup.Revision(), readInfoErr)
	}

	st.Lock()
	if installRecord != nil {
		t.Set("install-record", installRecord)
	}
	perfTimings.Save(st)
	st.Unlock()

	return nil
}

// Maybe we will need flags as in readInfo
var readComponentInfo = func(compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
	cont := snapdir.New(compMntDir)
	return snap.ReadComponentInfoFromContainer(cont, snapInfo, csi)
}

func (m *SnapManager) undoMountComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	compSetup, snapsup, err := TaskComponentSetup(t)
	st.Unlock()
	if err != nil {
		return err
	}

	return m.undoSetupComponent(t, compSetup.CompSideInfo, snapsup.InstanceName())
}

func (m *SnapManager) undoSetupComponent(t *state.Task, csi *snap.ComponentSideInfo, instanceName string) error {
	st := t.State()
	st.Lock()
	deviceCtx, err := DeviceCtx(st, t, nil)
	st.Unlock()
	if err != nil {
		return err
	}

	var installRecord backend.InstallRecord
	st.Lock()
	// install-record is optional
	err = t.Get("install-record", &installRecord)
	st.Unlock()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, instanceName)

	pm := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoSetupComponent(cpi, &installRecord, deviceCtx, pm); err != nil {
		return err
	}

	return m.backend.RemoveComponentDir(cpi)
}

func (m *SnapManager) doLinkComponent(t *state.Task, _ *tomb.Tomb) error {
	// invariant: component is not in the state (unlink happens previously if necessary)
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// snapSt is a copy of the current state
	compSetup, snapsup, snapSt, err := compSetupAndState(t)
	if err != nil {
		return err
	}

	// Grab information for current snap revision
	// TODO will this still be correct when jointly installing snap +
	// components? Link for the snap should happen earlier. But we do not
	// want to reboot until all components are linked and the initramfs has
	// the information to mount them already. Probably we should link first
	// the components and use information from SnapSetup in that case.
	snapInfo, err := snapSt.CurrentInfo()
	// the owner snap is expected to be installed (ErrNoCurrent not allowed)
	if err != nil {
		return err
	}

	cs := sequence.NewComponentState(compSetup.CompSideInfo, compSetup.CompType)
	// set information for undoLinkComponent in the task
	t.Set("linked-component", cs)
	// Append new component to components of the current snap
	if err := snapSt.Sequence.AddComponentForRevision(snapInfo.Revision, cs); err != nil {
		return fmt.Errorf("internal error while linking component: %w", err)
	}

	// Store last component refresh time
	if snapSt.LastCompRefreshTime == nil {
		snapSt.LastCompRefreshTime = make(map[string]time.Time)
	}
	snapSt.LastCompRefreshTime[compSetup.ComponentName()] = timeNow()

	// Create the symlink
	csi := cs.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	if err := m.backend.LinkComponent(cpi, snapInfo.Revision); err != nil {
		return err
	}

	// Finally, write the state
	Set(st, snapsup.InstanceName(), snapSt)
	// Make sure we won't be rerun
	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *SnapManager) undoLinkComponent(t *state.Task, _ *tomb.Tomb) error {
	// invariant: component is installed
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// snapSt is a copy of the current state
	_, snapsup, snapSt, err := compSetupAndState(t)
	if err != nil {
		return err
	}

	// Expected to be installed
	snapInfo, err := snapSt.CurrentInfo()
	if err != nil {
		return err
	}

	var linkedComp *sequence.ComponentState
	err = t.Get("linked-component", &linkedComp)
	if err != nil {
		return err
	}

	// Restore old state
	// relinking of the old component is done in the undo of unlink-current-snap

	// Remove the symlink
	csi := linkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	if err := m.backend.UnlinkComponent(cpi, snapInfo.Revision); err != nil {
		return err
	}

	snapSt.Sequence.RemoveComponentForRevision(snapInfo.Revision,
		linkedComp.SideInfo.Component)

	// Finally, write the state
	Set(st, snapsup.InstanceName(), snapSt)
	// Make sure we won't be rerun
	t.SetStatus(state.UndoneStatus)

	return nil
}

func (m *SnapManager) doUnlinkCurrentComponent(t *state.Task, _ *tomb.Tomb) (err error) {
	// invariant: current snap has a revision for this component installed
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// snapSt is a copy of the current state
	compSetup, snapsup, snapSt, err := compSetupAndState(t)
	if err != nil {
		return err
	}
	cref := compSetup.CompSideInfo.Component

	// Expected to be installed
	snapInfo, err := snapSt.CurrentInfo()
	if err != nil {
		return err
	}

	// Remove current component for the current snap
	unlinkedComp := snapSt.Sequence.RemoveComponentForRevision(snapInfo.Revision, cref)
	if unlinkedComp == nil {
		return fmt.Errorf("internal error while unlinking: %s expected but not found", cref)
	}

	// Remove symlink
	csi := unlinkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	if err := m.backend.UnlinkComponent(cpi, snapInfo.Revision); err != nil {
		return err
	}

	// set information for undoUnlinkCurrentComponent/doDiscardComponent in
	// the setup task
	setupTask, err := componentSetupTask(t)
	if err != nil {
		return err
	}
	setupTask.Set("unlinked-component", *unlinkedComp)

	// Finally, write the state
	Set(st, snapsup.InstanceName(), snapSt)
	// Make sure we won't be rerun
	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *SnapManager) undoUnlinkCurrentComponent(t *state.Task, _ *tomb.Tomb) (err error) {
	// invariant: component is not installed
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// snapSt is a copy of the current state
	_, snapsup, snapSt, err := compSetupAndState(t)
	if err != nil {
		return err
	}

	// Expected to be installed
	snapInfo, err := snapSt.CurrentInfo()
	if err != nil {
		return err
	}

	setupTask, err := componentSetupTask(t)
	if err != nil {
		return err
	}
	var unlinkedComp sequence.ComponentState
	if err := setupTask.Get("unlinked-component", &unlinkedComp); err != nil {
		return fmt.Errorf("internal error: no unlinked component: err")
	}

	if err := snapSt.Sequence.AddComponentForRevision(
		snapInfo.Revision, &unlinkedComp); err != nil {
		return fmt.Errorf("internal error while undo unlink component: %w", err)
	}

	// Re-create the symlink
	csi := unlinkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	if err := m.backend.LinkComponent(cpi, snapInfo.Revision); err != nil {
		return err
	}

	// Finally, write the state
	Set(st, snapsup.InstanceName(), snapSt)
	// Make sure we won't be rerun
	t.SetStatus(state.UndoneStatus)

	return nil
}

func (m *SnapManager) doSetupKernelModules(t *state.Task, _ *tomb.Tomb) error {
	// invariant: component not linked yet
	st := t.State()

	// snapSt is a copy of the current state
	st.Lock()
	compSetup, snapsup, snapSt, err := compSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	// kernel-modules components already in the system
	kmodComps := snapSt.Sequence.ComponentsWithTypeForRev(snapsup.Revision(), snap.KernelModulesComponent)

	// Set-up the new kernel modules component - called with unlocked state
	// as it can take a couple of seconds.
	pm := NewTaskProgressAdapterUnlocked(t)
	err = m.backend.SetupKernelModulesComponents(
		[]*snap.ComponentSideInfo{compSetup.CompSideInfo},
		kmodComps, snapsup.InstanceName(), snapsup.Revision(), pm)
	if err != nil {
		return err
	}

	// Make sure we won't be rerun
	st.Lock()
	defer st.Unlock()
	t.SetStatus(state.DoneStatus)
	return nil
}

func (m *SnapManager) doRemoveKernelModulesSetup(t *state.Task, _ *tomb.Tomb) error {
	// invariant: component unlinked on undo
	st := t.State()

	// snapSt is a copy of the current state
	st.Lock()
	compSetup, snapsup, snapSt, err := compSetupAndState(t)
	st.Unlock()
	if err != nil {
		return err
	}

	// current kernel-modules components in the system
	st.Lock()
	kmodComps := snapSt.Sequence.ComponentsWithTypeForRev(snapsup.Revision(), snap.KernelModulesComponent)
	st.Unlock()

	// Restore kernel modules components state - called with unlocked state
	// as it can take a couple of seconds.
	pm := NewTaskProgressAdapterUnlocked(t)
	// Component from compSetup has already been unlinked, so it is not in kmodComps
	err = m.backend.RemoveKernelModulesComponentsSetup(
		[]*snap.ComponentSideInfo{compSetup.CompSideInfo},
		kmodComps, snapsup.InstanceName(), snapsup.Revision(), pm)
	if err != nil {
		return err
	}

	// Make sure we won't be rerun
	st.Lock()
	defer st.Unlock()
	t.SetStatus(state.UndoneStatus)
	return nil
}

func infoForCompUndo(t *state.Task) (*snap.ComponentSideInfo, string, error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	_, snapsup, err := TaskComponentSetup(t)
	if err != nil {
		return nil, "", err
	}

	setupTask, err := componentSetupTask(t)
	if err != nil {
		return nil, "", err
	}
	var unlinkedComp sequence.ComponentState
	err = setupTask.Get("unlinked-component", &unlinkedComp)
	if err != nil {
		return nil, "", fmt.Errorf("internal error: no component to discard: %w", err)
	}

	return unlinkedComp.SideInfo, snapsup.InstanceName(), nil
}

func (m *SnapManager) doDiscardComponent(t *state.Task, _ *tomb.Tomb) error {
	compSideInfo, instanceName, err := infoForCompUndo(t)
	if err != nil {
		return err
	}

	// Discard the previously unlinked component
	return m.undoSetupComponent(t, compSideInfo, instanceName)
}
