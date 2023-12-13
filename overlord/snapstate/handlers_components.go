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
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
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

func (m *SnapManager) doPrepareComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	compSetup, _, err := TaskComponentSetup(t)
	if err != nil {
		return err
	}

	if compSetup.Revision().Unset() {
		// This is a local installation, revision is -1 (there
		// is no history of local revisions for components).
		compSetup.CompSideInfo.Revision = snap.R(-1)
	}

	t.Set("component-setup", compSetup)
	return nil
}

func (m *SnapManager) doMountComponent(t *state.Task, _ *tomb.Tomb) error {
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
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapsup.InstanceName(), snapsup.Revision())

	cleanup := func() {
		st.Lock()
		defer st.Unlock()

		// RemoveComponentDir is idempotent so it's ok to always
		// call it in the cleanup path.
		if err := m.backend.RemoveComponentDir(cpi); err != nil {
			t.Errorf("cannot cleanup partial setup component %q: %v",
				compSetup.CompSideInfo, err)
		}
	}

	pm := NewTaskProgressAdapterUnlocked(t)
	var installRecord *backend.InstallRecord
	timings.Run(perfTimings, "setup-component",
		fmt.Sprintf("setup component %q", compSetup.CompSideInfo.Component),
		func(timings.Measurer) {
			installRecord, err = m.backend.SetupComponent(
				compSetup.CompPath,
				cpi,
				deviceCtx,
				pm)
		})
	if err != nil {
		cleanup()
		return err
	}

	// double check that the component is mounted
	var readInfoErr error
	for i := 0; i < 10; i++ {
		compMntDir := cpi.MountDir()
		_, readInfoErr = readComponentInfo(compMntDir)
		if readInfoErr == nil {
			logger.Debugf("component %q (%v) available at %q",
				compSetup.CompSideInfo.Component,
				compSetup.Revision(), compMntDir)
			break
		}
		// snap not found, seems is not mounted yet
		time.Sleep(mountPollInterval)
	}
	if readInfoErr != nil {
		timings.Run(perfTimings, "undo-setup-component",
			fmt.Sprintf("Undo setup of component %q",
				compSetup.CompSideInfo.Component),
			func(timings.Measurer) {
				err = m.backend.UndoSetupComponent(cpi,
					installRecord, deviceCtx, pm)
			})
		if err != nil {
			st.Lock()
			t.Errorf("cannot undo partial setup of component %q: %v",
				compSetup.CompSideInfo.Component, err)
			st.Unlock()
		}

		cleanup()
		return fmt.Errorf("expected component %q rev %v to be mounted but is not: %w",
			compSetup.CompSideInfo.Component, compSetup.Revision(), readInfoErr)
	}

	st.Lock()
	if installRecord != nil {
		t.Set("install-record", installRecord)
	}
	st.Unlock()

	st.Lock()
	perfTimings.Save(st)
	st.Unlock()

	return nil
}

// Maybe we will need flags as in readInfo
var readComponentInfo = func(compMntDir string) (*snap.ComponentInfo, error) {
	cont := snapdir.New(compMntDir)
	return snap.ReadComponentInfoFromContainer(cont)
}

func (m *SnapManager) undoMountComponent(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
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

	var installRecord backend.InstallRecord
	st.Lock()
	// install-record is optional
	err = t.Get("install-record", &installRecord)
	st.Unlock()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	csi := compSetup.CompSideInfo
	cpi := snap.MinimalComponentContainerPlaceInfo(csi.Component.ComponentName,
		csi.Revision, snapsup.InstanceName(), snapsup.Revision())

	pm := NewTaskProgressAdapterUnlocked(t)
	if err := m.backend.UndoSetupComponent(cpi, &installRecord, deviceCtx, pm); err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	return m.backend.RemoveComponentDir(cpi)
}
