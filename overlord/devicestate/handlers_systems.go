// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"os"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

func taskRecoverySystemSetup(t *state.Task) (*recoverySystemSetup, error) {
	var setup recoverySystemSetup

	err := t.Get("recovery-system-setup", &setup)
	if err == nil {
		return &setup, nil
	}
	if err != state.ErrNoState {
		return nil, err
	}
	// find the task which holds the data
	var id string
	if err := t.Get("recovery-system-setup-task", &id); err != nil {
		return nil, err
	}
	ts := t.State().Task(id)
	if ts == nil {
		return nil, fmt.Errorf("internal error: cannot find referenced task %v", id)
	}
	if err := ts.Get("recovery-system-setup", &setup); err != nil {
		return nil, err
	}
	return &setup, nil
}

func setTaskRecoverySystemSetup(t *state.Task, setup *recoverySystemSetup) error {
	if t.Has("recovery-system-setup") {
		t.Set("recovery-system-setup", setup)
		return nil
	}
	var id string
	if err := t.Get("recovery-system-setup-task", &id); err != nil {
		return err
	}
	ts := t.State().Task(id)
	if ts == nil {
		return fmt.Errorf("internal error: cannot find referenced task %v", id)
	}
	ts.Set("recovery-system-setup", setup)
	return nil
}

func (m *DeviceManager) doCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	if release.OnClassic {
		// TODO: this may need to be lifted in the future
		return fmt.Errorf("cannot run update gadget assets task on a classic system")
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	isRemodel := remodelCtx.ForRemodeling()
	groundDeviceCtx := remodelCtx.GroundContext()

	model := groundDeviceCtx.Model()
	if isRemodel {
		model = remodelCtx.Model()
	}

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain recovery system setup information")
	}
	label := setup.Label

	// get all infos
	infoGetter := func(name string) (*snap.Info, bool, error) {
		// snap may be present in the system in which case info comes
		// from snapstate
		info, err := snapstate.CurrentInfo(st, name)
		if err == nil {
			hash, _, err := asserts.SnapFileSHA3_384(info.MountFile())
			if err != nil {
				return nil, true, fmt.Errorf("cannot compute SHA3 of snap file: %v", err)
			}
			info.Sha3_384 = hash
			return info, true, nil
		}
		if _, ok := err.(*snap.NotInstalledError); !ok {
			return nil, false, err
		}
		logger.Debugf("requested info for not yet installed snap %q", name)
		// TODO: handle remodel case in which snap may not be installed
		// yet, and thus we need to pull info from snapsup of relevant
		// download tasks
		return nil, false, fmt.Errorf("not implemented")
	}

	db := assertstate.DB(st)
	// 1. prepare recovery system from remodel snaps (or current snaps)
	newFiles, systemDir, err := createSystemForModelFromValidatedSnaps(model, label, db, infoGetter)
	if err != nil {
		return fmt.Errorf("cannot create a recovery system with label %q for %v: %v", label, model.Model(), err)
	}
	logger.Debugf("recovery system dir: %v", systemDir)
	logger.Debugf("new common snap files: %v", newFiles)

	defer func() {
		if err == nil {
			return
		}
		if err := os.RemoveAll(systemDir); err != nil && !os.IsNotExist(err) {
			logger.Noticef("when removing recovery system %q: %v", label, err)
		}
		for _, f := range newFiles {
			// new files under the recovery system dir would have
			// been removed with the directory already
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				logger.Noticef("when removing seed file %q: %v", f, err)
			}
		}
	}()

	setup.NewCommonFiles = newFiles
	setup.Directory = systemDir
	// 2. keep track of the system in task state
	if err := setTaskRecoverySystemSetup(t, setup); err != nil {
		return fmt.Errorf("cannot record recovery system setup state: %v", err)
	}
	// 3. set up boot variables for tracking the tried system state
	if err := boot.SetTryRecoverySystem(remodelCtx, label); err != nil {
		// rollback?
		return fmt.Errorf("cannot attempt booting into recovery system %q: %v", label, err)
	}
	// 4. and set up the next boot that that system
	if err := boot.SetRecoveryBootSystemAndMode(remodelCtx, label, "recover"); err != nil {
		return fmt.Errorf("cannot set device to boot into candidate system %q: %v", label, err)
	}

	// this task is done, further processing happens in finalize
	t.SetStatus(state.DoneStatus)

	logger.Noticef("restarting into candidate system %q", label)
	m.state.RequestRestart(state.RestartSystemNow)
	return nil
}

func (m *DeviceManager) undoCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	if release.OnClassic {
		// TODO: this may need to be lifted in the future
		return fmt.Errorf("cannot run update gadget assets task on a classic system")
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain recovery system setup information")
	}
	label := setup.Label

	var undoErr error
	if err := os.RemoveAll(setup.Directory); err != nil && !os.IsNotExist(err) {
		t.Logf("when removing recovery system %q: %v", setup.Label, err)
		undoErr = err
	} else {
		t.Logf("removed recovery system directory %v", setup.Directory)
	}
	for _, f := range setup.NewCommonFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			t.Logf("when removing seed file %q: %v", f, err)
			if undoErr == nil {
				undoErr = err
			}
		} else {
			t.Logf("removed new file %q", f)
		}
	}

	if err := boot.DropRecoverySystem(remodelCtx, label); err != nil {
		return fmt.Errorf("cannot drop a current recovery system %q: %v", label, err)
	}

	return undoErr
}

func (m *DeviceManager) doFinalizeTriedRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	if release.OnClassic {
		// TODO: this may need to be lifted in the future
		return fmt.Errorf("cannot run update gadget assets task on a classic system")
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	if ok, _ := st.Restarting(); ok {
		// don't continue until we are in the restarted snapd
		t.Logf("Waiting for system reboot...")
		return &state.Retry{}
	}

	logger.Noticef("in finalize recovery system")

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}
	isRemodel := remodelCtx.ForRemodeling()

	var triedSystems []string
	// after rebooting to the recovery system and back, the system got moved
	// to the tried-systems list in the state
	if err := st.Get("tried-systems", &triedSystems); err != nil {
		return fmt.Errorf("cannot obtain tried recovery systems: %v", err)
	}

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return err
	}
	label := setup.Label

	// so far so good
	if !strutil.ListContains(triedSystems, label) {
		// system failed, trigger undoing of everything we did so far
		return fmt.Errorf("tried recovery system %q failed", label)
	}

	if isRemodel {
		// XXX: candidate system is promoted to the list of good ones once we
		// complete the whole remodel change
		logger.Noticef("recovery system will be promoted later")
		return nil
	}

	if err := boot.MarkRecoverySystemGood(remodelCtx, label); err != nil {
		return fmt.Errorf("cannot mark recovery system %q as good: %v", label, err)
	}

	// tried systems should be a one item list, we can clear it now
	st.Set("tried-systems", nil)

	return nil
}

func (m *DeviceManager) undoFinalizeTriedRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return err
	}
	label := setup.Label

	if err := boot.DropRecoverySystem(remodelCtx, label); err != nil {
		return fmt.Errorf("cannot drop a good recovery system %q: %v", label, err)
	}

	return nil
}
