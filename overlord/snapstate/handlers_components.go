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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/timings"
	"gopkg.in/tomb.v2"
)

// TaskComponentSetup returns the ComponentSetup and SnapSetup with task params hold
// by or referred to by the task.
func TaskComponentSetup(t *state.Task) (*ComponentSetup, *SnapSetup, error) {
	snapsup := mylog.Check2(TaskSnapSetup(t))

	var compSetup ComponentSetup
	mylog.Check(t.Get("component-setup", &compSetup))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, nil, err
	}
	if err == nil {
		return &compSetup, snapsup, nil
	}

	var id string
	mylog.Check(t.Get("component-setup-task", &id))

	ts := t.State().Task(id)
	if ts == nil {
		return nil, nil, fmt.Errorf("internal error: tasks are being pruned")
	}
	mylog.Check(ts.Get("component-setup", &compSetup))

	return &compSetup, snapsup, nil
}

func compSetupAndState(t *state.Task) (*ComponentSetup, *SnapSetup, *SnapState, error) {
	csup, ssup := mylog.Check3(TaskComponentSetup(t))

	var snapst SnapState
	mylog.Check(Get(t.State(), ssup.InstanceName(), &snapst))
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
		mylog.Check(t.Get("component-setup-task", &id))

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

	compSetup, _, snapSt := mylog.Check4(compSetupAndState(t))

	if compSetup.Revision().Unset() {
		// This is a local installation, revision is -1 if the current
		// one is non-local or not installed, or current one
		// decremented by one otherwise.
		revision := snap.R(-1)
		current := snapSt.CurrentComponentSideInfo(compSetup.CompSideInfo.Component)
		if current != nil && current.Revision.N < 0 {
			revision = snap.R(current.Revision.N - 1)
		}
		compSetup.CompSideInfo.Revision = revision
	}

	t.Set("component-setup", compSetup)
	return nil
}

func (m *SnapManager) doMountComponent(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	perfTimings := state.TimingsForTask(t)
	compSetup, snapsup := mylog.Check3(TaskComponentSetup(t))
	st.Unlock()

	st.Lock()
	deviceCtx := mylog.Check2(DeviceCtx(t.State(), t, nil))
	st.Unlock()

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
		mylog.Check(

			// RemoveComponentDir is idempotent so it's ok to always
			// call it in the cleanup path.
			m.backend.RemoveComponentDir(cpi))
	}()

	pm := NewTaskProgressAdapterUnlocked(t)
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-component",
		fmt.Sprintf("setup component %q", csi.Component),
		func(timings.Measurer) {
			installRecord = mylog.Check2(m.backend.SetupComponent(
				compSetup.CompPath,
				cpi,
				deviceCtx,
				pm))
		})

	// double check that the component is mounted
	var readInfoErr error
	for i := 0; i < 10; i++ {
		compMntDir := cpi.MountDir()
		_, readInfoErr = readComponentInfo(compMntDir, nil)
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
				mylog.Check(m.backend.UndoSetupComponent(cpi,
					installRecord, deviceCtx, pm))
			})

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
var readComponentInfo = func(compMntDir string, snapInfo *snap.Info) (*snap.ComponentInfo, error) {
	cont := snapdir.New(compMntDir)
	return snap.ReadComponentInfoFromContainer(cont, snapInfo)
}

func (m *SnapManager) undoMountComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	compSetup, snapsup := mylog.Check3(TaskComponentSetup(t))
	st.Unlock()

	return m.undoSetupComponent(t, compSetup.CompSideInfo, snapsup.InstanceName())
}

func (m *SnapManager) undoSetupComponent(t *state.Task, csi *snap.ComponentSideInfo, instanceName string) error {
	st := t.State()
	st.Lock()
	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))
	st.Unlock()

	var installRecord backend.InstallRecord
	st.Lock()
	mylog.
		// install-record is optional
		Check(t.Get("install-record", &installRecord))
	st.Unlock()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, instanceName)

	pm := NewTaskProgressAdapterUnlocked(t)
	mylog.Check(m.backend.UndoSetupComponent(cpi, &installRecord, deviceCtx, pm))

	return m.backend.RemoveComponentDir(cpi)
}

func (m *SnapManager) doLinkComponent(t *state.Task, _ *tomb.Tomb) error {
	// invariant: component is not in the state (unlink happens previously if necessary)
	st := t.State()
	st.Lock()
	defer st.Unlock()

	// snapSt is a copy of the current state
	compSetup, snapsup, snapSt := mylog.Check4(compSetupAndState(t))

	// Grab information for current snap revision
	// TODO will this still be correct when jointly installing snap +
	// components? Link for the snap should happen earlier. But we do not
	// want to reboot until all components are linked and the initramfs has
	// the information to mount them already. Probably we should link first
	// the components and use information from SnapSetup in that case.
	snapInfo := mylog.Check2(snapSt.CurrentInfo())
	// the owner snap is expected to be installed (ErrNoCurrent not allowed)

	cs := sequence.NewComponentState(compSetup.CompSideInfo, compSetup.CompType)
	// set information for undoLinkComponent in the task
	t.Set("linked-component", cs)
	mylog.Check(
		// Append new component to components of the current snap
		snapSt.Sequence.AddComponentForRevision(snapInfo.Revision, cs))

	// Store last component refresh time
	if snapSt.LastCompRefreshTime == nil {
		snapSt.LastCompRefreshTime = make(map[string]time.Time)
	}
	snapSt.LastCompRefreshTime[compSetup.ComponentName()] = timeNow()

	// Create the symlink
	csi := cs.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	mylog.Check(m.backend.LinkComponent(cpi, snapInfo.Revision))

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
	_, snapsup, snapSt := mylog.Check4(compSetupAndState(t))

	// Expected to be installed
	snapInfo := mylog.Check2(snapSt.CurrentInfo())

	var linkedComp *sequence.ComponentState
	mylog.Check(t.Get("linked-component", &linkedComp))

	// Restore old state
	// relinking of the old component is done in the undo of unlink-current-snap

	// Remove the symlink
	csi := linkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	mylog.Check(m.backend.UnlinkComponent(cpi, snapInfo.Revision))

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
	compSetup, snapsup, snapSt := mylog.Check4(compSetupAndState(t))

	cref := compSetup.CompSideInfo.Component

	// Expected to be installed
	snapInfo := mylog.Check2(snapSt.CurrentInfo())

	// Remove current component for the current snap
	unlinkedComp := snapSt.Sequence.RemoveComponentForRevision(snapInfo.Revision, cref)
	if unlinkedComp == nil {
		return fmt.Errorf("internal error while unlinking: %s expected but not found", cref)
	}

	// Remove symlink
	csi := unlinkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	mylog.Check(m.backend.UnlinkComponent(cpi, snapInfo.Revision))

	// set information for undoUnlinkCurrentComponent/doDiscardComponent in
	// the setup task
	setupTask := mylog.Check2(componentSetupTask(t))

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
	_, snapsup, snapSt := mylog.Check4(compSetupAndState(t))

	// Expected to be installed
	snapInfo := mylog.Check2(snapSt.CurrentInfo())

	setupTask := mylog.Check2(componentSetupTask(t))

	var unlinkedComp sequence.ComponentState
	mylog.Check(setupTask.Get("unlinked-component", &unlinkedComp))
	mylog.Check(snapSt.Sequence.AddComponentForRevision(
		snapInfo.Revision, &unlinkedComp))

	// Re-create the symlink
	csi := unlinkedComp.SideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapInfo.InstanceName())
	mylog.Check(m.backend.LinkComponent(cpi, snapInfo.Revision))

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
	compSetup, snapsup, snapSt := mylog.Check4(compSetupAndState(t))
	st.Unlock()

	// kernel-modules components already in the system
	kmodComps := snapSt.Sequence.ComponentsWithTypeForRev(snapsup.Revision(), snap.KernelModulesComponent)

	// Set-up the new kernel modules component - called with unlocked state
	// as it can take a couple of seconds.
	pm := NewTaskProgressAdapterUnlocked(t)
	mylog.Check(m.backend.SetupKernelModulesComponents(
		[]*snap.ComponentSideInfo{compSetup.CompSideInfo},
		kmodComps, snapsup.InstanceName(), snapsup.Revision(), pm))

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
	compSetup, snapsup, snapSt := mylog.Check4(compSetupAndState(t))
	st.Unlock()

	// current kernel-modules components in the system
	st.Lock()
	kmodComps := snapSt.Sequence.ComponentsWithTypeForRev(snapsup.Revision(), snap.KernelModulesComponent)
	st.Unlock()

	// Restore kernel modules components state - called with unlocked state
	// as it can take a couple of seconds.
	pm := NewTaskProgressAdapterUnlocked(t)
	mylog.
		// Component from compSetup has already been unlinked, so it is not in kmodComps
		Check(m.backend.RemoveKernelModulesComponentsSetup(
			[]*snap.ComponentSideInfo{compSetup.CompSideInfo},
			kmodComps, snapsup.InstanceName(), snapsup.Revision(), pm))

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

	_, snapsup := mylog.Check3(TaskComponentSetup(t))

	setupTask := mylog.Check2(componentSetupTask(t))

	var unlinkedComp sequence.ComponentState
	mylog.Check(setupTask.Get("unlinked-component", &unlinkedComp))

	return unlinkedComp.SideInfo, snapsup.InstanceName(), nil
}

func (m *SnapManager) doDiscardComponent(t *state.Task, _ *tomb.Tomb) error {
	compSideInfo, instanceName := mylog.Check3(infoForCompUndo(t))

	// Discard the previously unlinked component
	return m.undoSetupComponent(t, compSideInfo, instanceName)
}
