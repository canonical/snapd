// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2022 Canonical Ltd
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

package devicestate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func makeRollbackDir(name string) (string, error) {
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, name)

	if err := os.MkdirAll(rollbackDir, 0750); err != nil {
		return "", err
	}

	return rollbackDir, nil
}

func currentGadgetInfo(st *state.State, curDeviceCtx snapstate.DeviceContext) (*gadget.GadgetData, error) {
	currentInfo, err := snapstate.GadgetInfo(st, curDeviceCtx)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	if currentInfo == nil {
		// no current yet
		return nil, nil
	}

	ci, err := gadgetDataFromInfo(currentInfo, curDeviceCtx.Model())
	if err != nil {
		return nil, fmt.Errorf("cannot read current gadget snap details: %v", err)
	}
	return ci, nil
}

func pendingGadgetInfo(snapsup *snapstate.SnapSetup, pendingDeviceCtx snapstate.DeviceContext) (*gadget.GadgetData, error) {
	info, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
	if err != nil {
		return nil, fmt.Errorf("cannot read candidate gadget snap details: %v", err)
	}

	gi, err := gadgetDataFromInfo(info, pendingDeviceCtx.Model())
	if err != nil {
		return nil, fmt.Errorf("cannot read candidate snap gadget metadata: %v", err)
	}
	return gi, nil
}

var (
	gadgetUpdate = gadget.Update
)

func (m *DeviceManager) doUpdateGadgetAssets(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return err
	}

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	if remodelCtx.IsClassicBoot() {
		return fmt.Errorf("cannot run update gadget assets task on a classic system")
	}
	isRemodel := remodelCtx.ForRemodeling()
	groundDeviceCtx := remodelCtx.GroundContext()

	model := groundDeviceCtx.Model()
	if isRemodel {
		model = remodelCtx.Model()
	}
	// be extra paranoid when checking we are installing the right gadget
	var updateData *gadget.GadgetData
	switch snapsup.Type {
	case snap.TypeGadget:
		expectedGadgetSnap := model.Gadget()
		if snapsup.InstanceName() != expectedGadgetSnap {
			return fmt.Errorf("cannot apply gadget assets update from non-model gadget snap %q, expected %q snap",
				snapsup.InstanceName(), expectedGadgetSnap)
		}

		updateData, err = pendingGadgetInfo(snapsup, remodelCtx)
		if err != nil {
			return err
		}
	case snap.TypeKernel:
		expectedKernelSnap := model.Kernel()
		if snapsup.InstanceName() != expectedKernelSnap {
			return fmt.Errorf("cannot apply kernel assets update from non-model kernel snap %q, expected %q snap",
				snapsup.InstanceName(), expectedKernelSnap)
		}

		// now calculate the "update" data, it's the same gadget but
		// argumented from a different kernel
		updateData, err = currentGadgetInfo(t.State(), groundDeviceCtx)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("internal errror: doUpdateGadgetAssets called with snap type %v", snapsup.Type)
	}

	currentData, err := currentGadgetInfo(t.State(), groundDeviceCtx)
	if err != nil {
		return err
	}
	if currentData == nil {
		// no updates during first boot & seeding
		return nil
	}

	// add kernel directories
	currentKernelInfo, err := snapstate.CurrentInfo(st, groundDeviceCtx.Model().Kernel())
	// XXX: switch to the normal `if err != nil { return err }` pattern
	// here once all tests are updated and have a kernel
	if err == nil {
		currentData.KernelRootDir = currentKernelInfo.MountDir()
		updateData.KernelRootDir = currentKernelInfo.MountDir()
	}
	// if this is a gadget update triggered by an updated kernel we
	// need to ensure "updateData.KernelRootDir" points to the new kernel
	if snapsup.Type == snap.TypeKernel {
		updateKernelInfo, err := snap.ReadInfo(snapsup.InstanceName(), snapsup.SideInfo)
		if err != nil {
			return fmt.Errorf("cannot read candidate kernel snap details: %v", err)
		}
		updateData.KernelRootDir = updateKernelInfo.MountDir()
	}

	snapRollbackDir, err := makeRollbackDir(fmt.Sprintf("%v_%v", snapsup.InstanceName(), snapsup.SideInfo.Revision))
	if err != nil {
		return fmt.Errorf("cannot prepare update rollback directory: %v", err)
	}

	var updatePolicy gadget.UpdatePolicyFunc = nil

	// Even with a remodel a kernel refresh only updates the kernel assets
	if snapsup.Type == snap.TypeKernel {
		updatePolicy = gadget.KernelUpdatePolicy
	} else if isRemodel {
		// use the remodel policy which triggers an update of all
		// structures
		updatePolicy = gadget.RemodelUpdatePolicy
	}

	var updateObserver gadget.ContentUpdateObserver
	observeTrustedBootAssets, err := boot.TrustedAssetsUpdateObserverForModel(model, updateData.RootDir)
	if err != nil && err != boot.ErrObserverNotApplicable {
		return fmt.Errorf("cannot setup asset update observer: %v", err)
	}
	if err == nil {
		updateObserver = observeTrustedBootAssets
	}
	// do not release the state lock, the update observer may attempt to
	// modify modeenv inside, which implicitly is guarded by the state lock;
	// on top of that we do not expect the update to be moving large amounts
	// of data
	err = gadgetUpdate(model, *currentData, *updateData, snapRollbackDir, updatePolicy, updateObserver)
	if err != nil {
		if err == gadget.ErrNoUpdate {
			// no update needed
			t.Logf("No gadget assets update needed")
			return nil
		}
		return err
	}

	if err := os.RemoveAll(snapRollbackDir); err != nil && !os.IsNotExist(err) {
		logger.Noticef("failed to remove gadget update rollback directory %q: %v", snapRollbackDir, err)
	}

	// TODO: consider having the option to do this early via recovery in
	// core20, have fallback code as well there
	return snapstate.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystem, nil)
}

func (m *DeviceManager) updateGadgetCommandLine(t *state.Task, st *state.State, isUndo bool) (updated bool, err error) {
	snapsup, err := snapstate.TaskSnapSetup(t)
	if err != nil {
		return false, err
	}
	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return false, err
	}
	if devCtx.Model().Grade() == asserts.ModelGradeUnset {
		// pre UC20 system, do nothing
		return false, nil
	}
	var gadgetData *gadget.GadgetData
	if !isUndo {
		// when updating, command line comes from the new gadget
		gadgetData, err = pendingGadgetInfo(snapsup, devCtx)
		if err != nil {
			return false, err
		}
	} else {
		// but when undoing, we use the current gadget which should have
		// been restored
		currentGadgetData, err := currentGadgetInfo(st, devCtx)
		if err != nil {
			return false, err
		}
		gadgetData = currentGadgetData
	}
	// TODO: set optional command line
	updated, err = boot.UpdateCommandLineForGadgetComponent(devCtx, gadgetData.RootDir, "")
	if err != nil {
		return false, fmt.Errorf("cannot update kernel command line from gadget: %v", err)
	}
	return updated, nil
}

func (m *DeviceManager) doUpdateGadgetCommandLine(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	if devCtx.IsClassicBoot() {
		return fmt.Errorf("internal error: cannot run update gadget kernel command line task on a classic system")
	}

	var seeded bool
	err = st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		// do nothing during first boot & seeding
		return nil
	}

	const isUndo = false
	updated, err := m.updateGadgetCommandLine(t, st, isUndo)
	if err != nil {
		return err
	}
	if !updated {
		logger.Debugf("no kernel command line update from gadget")
		return nil
	}
	t.Logf("Updated kernel command line")

	// TODO: consider optimization to avoid double reboot when the gadget
	// snap carries an update to the gadget assets and a change in the
	// kernel command line

	// kernel command line was updated, request a reboot to make it effective

	return snapstate.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystem, nil)
}

func (m *DeviceManager) undoUpdateGadgetCommandLine(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	if devCtx.IsClassicBoot() {
		return fmt.Errorf("internal error: cannot run undo update gadget kernel command line task on a classic system")
	}

	var seeded bool
	err = st.Get("seeded", &seeded)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	if !seeded {
		// do nothing during first boot & seeding
		return nil
	}

	const isUndo = true
	updated, err := m.updateGadgetCommandLine(t, st, isUndo)
	if err != nil {
		return err
	}
	if !updated {
		logger.Debugf("no kernel command line update to undo")
		return nil
	}
	t.Logf("Reverted kernel command line change")

	// kernel command line was updated, request a reboot to make it effective
	return snapstate.FinishTaskWithRestart(t, state.UndoneStatus, restart.RestartSystem, nil)
}
