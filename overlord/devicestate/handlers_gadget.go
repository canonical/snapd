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
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

func makeRollbackDir(name string) (string, error) {
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, name)

	if err := os.MkdirAll(rollbackDir, 0750); err != nil {
		return "", err
	}

	return rollbackDir, nil
}

// CurrentGadgetData returns the GadgetData for the currently active gadget.
func CurrentGadgetData(st *state.State, curDeviceCtx snapstate.DeviceContext) (*gadget.GadgetData, error) {
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

func pendingGadgetData(snapsup *snapstate.SnapSetup, pendingDeviceCtx snapstate.DeviceContext) (*gadget.GadgetData, error) {
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

func setGadgetRestartRequired(t *state.Task) {
	chg := t.Change()
	chg.Set("gadget-restart-required", true)
}

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

		updateData, err = pendingGadgetData(snapsup, remodelCtx)
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
		updateData, err = CurrentGadgetData(t.State(), groundDeviceCtx)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("internal errror: doUpdateGadgetAssets called with snap type %v", snapsup.Type)
	}

	currentData, err := CurrentGadgetData(t.State(), groundDeviceCtx)
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

	err = func() error {
		var updateObserver gadget.ContentUpdateObserver
		observeTrustedBootAssets, err := boot.TrustedAssetsUpdateObserverForModel(model, updateData.RootDir)
		if err != nil && err != boot.ErrObserverNotApplicable {
			return fmt.Errorf("cannot setup asset update observer: %v", err)
		}
		if err == nil {
			updateObserver = observeTrustedBootAssets
			defer observeTrustedBootAssets.Done()
		}
		// do not release the state lock, the update observer may
		// attempt to modify modeenv inside, which implicitly is
		// guarded by the state lock; on top of that we do not expect
		// the update to be moving large amounts of data
		return gadgetUpdate(model, *currentData, *updateData, snapRollbackDir, updatePolicy, updateObserver)
	}()
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
	setGadgetRestartRequired(t)
	return snapstate.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystem, nil)
}

// fromSystemOption tells us if t was created when setting a system
// option for the kernel command line.
func fromSystemOption(t *state.Task) bool {
	for _, param := range []string{"cmdline-append", "dangerous-cmdline-append"} {
		if t.Has(param) {
			return true
		}
	}

	return false
}

// kernelCommandLineAppendArgs returns extra arguments that we want to
// append to the kernel command line, searching first by looking at
// the task, and if not found, looking at the current configuration
// options. One thing or the other could happen depending on whether
// this is a task created when setting a kernel option or by gadget
// installation.
func kernelCommandLineAppendArgs(tsk *state.Task, tr *config.Transaction,
	taskParam string) (string, error) {

	var value string
	err := tsk.Get(taskParam, &value)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, state.ErrNoState) {
		return "", err
	}

	var option string
	switch taskParam {
	case "cmdline-append":
		option = "system.kernel.cmdline-append"
	case "dangerous-cmdline-append":
		option = "system.kernel.dangerous-cmdline-append"
	default:
		return "", fmt.Errorf("internal error, unexpected task parameter %q", taskParam)
	}
	if err := tr.Get("core", option, &value); err != nil && !config.IsNoOption(err) {
		return "", err
	}

	return value, nil
}

func buildAppendedKernelCommandLine(t *state.Task, gd *gadget.GadgetData, deviceCtx snapstate.DeviceContext) (string, error) {
	tr := config.NewTransaction(t.State())
	rawCmdlineAppend, err := kernelCommandLineAppendArgs(t, tr, "cmdline-append")
	if err != nil {
		return "", err
	}
	// Validation against allow list has already happened in
	// configcore, but the gadget might have changed, so we check
	// again and filter any unallowed argument.
	cmdlineAppend, forbidden := gadget.FilterKernelCmdline(rawCmdlineAppend, gd.Info.KernelCmdline.Allow)
	if forbidden != "" {
		warnMsg := fmt.Sprintf("%q is not allowed by the gadget and has been filtered out from the kernel command line", forbidden)
		logger.Noticef(warnMsg)
		t.Logf(warnMsg)
	}

	// Dangerous extra cmdline only considered for dangerous models
	if deviceCtx.Model().Grade() == asserts.ModelDangerous {
		cmdlineAppendDanger, err := kernelCommandLineAppendArgs(t, tr,
			"dangerous-cmdline-append")
		if err != nil {
			return "", err
		}
		cmdlineAppend = strutil.JoinNonEmpty(
			[]string{cmdlineAppend, cmdlineAppendDanger}, " ")
	}

	logger.Debugf("appended kernel command line part is %q", cmdlineAppend)

	return cmdlineAppend, nil
}

func (m *DeviceManager) updateGadgetCommandLine(t *state.Task, st *state.State, useCurrentGadget bool) (updated bool, err error) {
	logger.Debugf("updating kernel command line")
	devCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return false, err
	}
	if devCtx.Model().Grade() == asserts.ModelGradeUnset {
		// pre UC20 system, do nothing
		return false, nil
	}
	var gadgetData *gadget.GadgetData
	if !useCurrentGadget {
		// command line comes from the new gadget when updating
		snapsup, err := snapstate.TaskSnapSetup(t)
		if err != nil {
			return false, err
		}
		gadgetData, err = pendingGadgetData(snapsup, devCtx)
		if err != nil {
			return false, err
		}
	} else {
		// but when undoing or when the change comes from a
		// system option (no setup task), we use the current
		// gadget (should have been restored in the undo case)
		currentGadgetData, err := CurrentGadgetData(st, devCtx)
		if err != nil {
			return false, err
		}
		gadgetData = currentGadgetData
	}

	cmdlineAppend, err := buildAppendedKernelCommandLine(t, gadgetData, devCtx)
	if err != nil {
		return false, err
	}

	updated, err = boot.UpdateCommandLineForGadgetComponent(devCtx, gadgetData.RootDir, cmdlineAppend)
	if err != nil {
		return false, fmt.Errorf("cannot update kernel command line from gadget: %v", err)
	}
	if updated {
		setGadgetRestartRequired(t)
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

	// Find out if the update has been triggered by setting a system
	// option that modifies the kernel command line.
	isSysOption := fromSystemOption(t)

	// We use the current gadget kernel command line if the change comes
	// from setting a system option.
	useCurrentGadget := isSysOption
	updated, err := m.updateGadgetCommandLine(t, st, useCurrentGadget)
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

	if isSysOption {
		logger.Debugf("change comes from system option, we do not reboot")
		t.SetStatus(state.DoneStatus)
		return nil
	}

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
