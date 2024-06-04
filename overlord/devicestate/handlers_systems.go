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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

func taskRecoverySystemSetup(t *state.Task) (*recoverySystemSetup, error) {
	var setup recoverySystemSetup

	err := t.Get("recovery-system-setup", &setup)
	if err == nil {
		return &setup, nil
	}
	if !errors.Is(err, state.ErrNoState) {
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
	return fmt.Errorf("internal error: cannot indirectly set recovery-system-setup")
}

func logNewSystemSnapFile(logfile, fileName string) error {
	if !strings.HasPrefix(filepath.Dir(fileName), boot.InitramfsUbuntuSeedDir+"/") {
		return fmt.Errorf("internal error: unexpected recovery system snap location %q", fileName)
	}
	currentLog, err := os.ReadFile(logfile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	modifiedLog := bytes.NewBuffer(currentLog)
	fmt.Fprintln(modifiedLog, fileName)
	return osutil.AtomicWriteFile(logfile, modifiedLog.Bytes(), 0644, 0)
}

func purgeNewSystemSnapFiles(logfile string) error {
	f, err := os.Open(logfile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for {
		if !s.Scan() {
			break
		}
		// one file per line
		fileName := strings.TrimSpace(s.Text())
		if fileName == "" {
			continue
		}
		if !strings.HasPrefix(fileName, boot.InitramfsUbuntuSeedDir) {
			logger.Noticef("while removing new seed snap %q: unexpected recovery system snap location", fileName)
			continue
		}
		if err := os.Remove(fileName); err != nil && !os.IsNotExist(err) {
			logger.Noticef("while removing new seed snap %q: %v", fileName, err)
		}
	}
	return s.Err()
}

type uniqueSnapsInRecoverySystem struct {
	SnapPaths []string `json:"snap-paths"`
}

func snapsUniqueToRecoverySystem(target string, systems []*System) ([]string, error) {
	// asserted snaps are shared by systems, figure out which ones are unique to
	// the system we want to remove
	requiredByOtherSystems := make(map[string]bool)
	for _, sys := range systems {
		if sys.Label == target {
			continue
		}

		sd, err := seed.Open(dirs.SnapSeedDir, sys.Label)
		if err != nil {
			return nil, err
		}

		if err := sd.LoadAssertions(nil, func(*asserts.Batch) error {
			return nil
		}); err != nil {
			return nil, err
		}

		if err := sd.LoadMeta(seed.AllModes, nil, timings.New(nil)); err != nil {
			return nil, err
		}

		err = sd.Iter(func(sn *seed.Snap) error {
			if sn.ID() != "" {
				requiredByOtherSystems[sn.Path] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	targetSeed, err := seed.Open(dirs.SnapSeedDir, target)
	if err != nil {
		return nil, err
	}

	if err := targetSeed.LoadAssertions(nil, func(*asserts.Batch) error {
		return nil
	}); err != nil {
		return nil, err
	}

	if err := targetSeed.LoadMeta(seed.AllModes, nil, timings.New(nil)); err != nil {
		return nil, err
	}

	var uniqueToTarget []string
	err = targetSeed.Iter(func(sn *seed.Snap) error {
		if sn.ID() != "" && !requiredByOtherSystems[sn.Path] {
			uniqueToTarget = append(uniqueToTarget, sn.Path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return uniqueToTarget, nil
}

func (m *DeviceManager) doRemoveRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	systems, err := m.systems()
	if err != nil {
		return fmt.Errorf("cannot get recovery systems: %w", err)
	}

	var setup removeRecoverySystemSetup
	if err := t.Get("remove-recovery-system-setup", &setup); err != nil {
		return err
	}

	found := false
	for _, sys := range systems {
		if sys.Label == setup.Label {
			found = true
			if sys.Current {
				return fmt.Errorf("cannot remove current recovery system: %q", setup.Label)
			}

			if sys.DefaultRecoverySystem {
				return fmt.Errorf("cannot remove default recovery system: %q", setup.Label)
			}
			break
		}
	}

	// if we couldn't find the system in the returned list of systems, then we
	// might have already attempted to remove it. in that case, we should do our
	// best effort at making sure that it is fully removed.

	if found && len(systems) == 1 {
		return fmt.Errorf("cannot remove last recovery system: %q", setup.Label)
	}

	deviceCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return fmt.Errorf("cannot get device context: %w", err)
	}

	// if this task has partially run before, then we might have already removed
	// some files required to load the being-removed system seed. to avoid this,
	// we first check if we already have stored a list of snaps to remove
	// (meaning, this task is being re-run). if the list isn't present, then we
	// calculate it and store it in the task state for potential future re-runs.
	var snapsToRemove uniqueSnapsInRecoverySystem
	if err := t.Get("snaps-to-remove", &snapsToRemove); err != nil {
		if !errors.Is(err, state.ErrNoState) {
			return fmt.Errorf("cannot get snaps to remove from task: %w", err)
		}

		uniqueSnapPaths, err := snapsUniqueToRecoverySystem(setup.Label, systems)
		if err != nil {
			return fmt.Errorf("cannot get snaps unique to recovery system %q: %w", setup.Label, err)
		}

		snapsToRemove.SnapPaths = uniqueSnapPaths

		t.Set("snaps-to-remove", snapsToRemove)

		// we need to unlock and re-lock the state to make sure that
		// snaps-to-remove is persisted. if we ever change how the exclusive
		// changes are handled, then we might need to revisit this.
		st.Unlock()
		st.Lock()
	}

	recoverySystemsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems")

	if err := boot.DropRecoverySystem(deviceCtx, setup.Label); err != nil {
		return fmt.Errorf("cannot drop recovery system %q: %v", setup.Label, err)
	}

	for _, sn := range snapsToRemove.SnapPaths {
		path := filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps", filepath.Base(sn))
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("cannot remove snap %q: %w", path, err)
		}
	}

	if err := os.RemoveAll(filepath.Join(recoverySystemsDir, setup.Label)); err != nil {
		return fmt.Errorf("cannot remove recovery system %q: %w", setup.Label, err)
	}

	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *DeviceManager) doCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	if !remodelCtx.IsCoreBoot() {
		return fmt.Errorf("cannot create recovery systems on a classic (non-hybrid) system")
	}

	model := remodelCtx.Model()
	isRemodel := remodelCtx.ForRemodeling()

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain recovery system setup information")
	}

	label := setup.Label
	systemDirectory := setup.Directory

	// get all infos
	infoGetter := func(name string) (info *snap.Info, path string, present bool, err error) {
		// snaps will come from one of these places:
		//   * passed into the task via a list of side infos (these would have
		//     come from a user posting snaps via the API)
		//   * have just been downloaded by a task in setup.SnapSetupTasks
		//   * already installed on the system

		for _, l := range setup.LocalSnaps {
			if l.SideInfo.RealName != name {
				continue
			}

			snapf, err := snapfile.Open(l.Path)
			if err != nil {
				return nil, "", false, err
			}

			info, err := snap.ReadInfoFromSnapFile(snapf, l.SideInfo)
			if err != nil {
				return nil, "", false, err
			}

			return info, l.Path, true, nil
		}

		// in a remodel scenario, the snaps may need to be fetched and thus
		// their content can be different from what we have in already installed
		// snaps, so we should first check the download tasks before consulting
		// snapstate
		logger.Debugf("requested info for snap %q being installed during remodel", name)
		for _, tskID := range setup.SnapSetupTasks {
			taskWithSnapSetup := st.Task(tskID)
			snapsup, err := snapstate.TaskSnapSetup(taskWithSnapSetup)
			if err != nil {
				return nil, "", false, err
			}
			if snapsup.SnapName() != name {
				continue
			}
			// by the time this task runs, the file has already been
			// downloaded and validated
			snapFile, err := snapfile.Open(snapsup.MountFile())
			if err != nil {
				return nil, "", false, err
			}
			info, err = snap.ReadInfoFromSnapFile(snapFile, snapsup.SideInfo)
			if err != nil {
				return nil, "", false, err
			}

			return info, info.MountFile(), true, nil
		}

		// either a remodel scenario, in which case the snap is not
		// among the ones being fetched, or just creating a recovery
		// system, in which case we use the snaps that are already
		// installed

		info, err = snapstate.CurrentInfo(st, name)
		if err == nil {
			hash, _, err := asserts.SnapFileSHA3_384(info.MountFile())
			if err != nil {
				return nil, "", true, fmt.Errorf("cannot compute SHA3 of snap file: %v", err)
			}
			info.Sha3_384 = hash
			return info, info.MountFile(), true, nil
		}
		if _, ok := err.(*snap.NotInstalledError); !ok {
			return nil, "", false, err
		}
		return nil, "", false, nil
	}

	observeSnapFileWrite := func(recoverySystemDir, where string) error {
		if recoverySystemDir != systemDirectory {
			return fmt.Errorf("internal error: unexpected recovery system path %q", recoverySystemDir)
		}
		// track all the files, both asserted shared snaps and private
		// ones
		return logNewSystemSnapFile(filepath.Join(recoverySystemDir, "snapd-new-file-log"), where)
	}

	var db asserts.RODatabase
	if isRemodel {
		// during remodel, the model assertion is not yet present in the
		// assertstate database, hence we need to use a temporary one to
		// which we explicitly add the new model assertion, as
		// createSystemForModelFromValidatedSnaps expects all relevant
		// assertions to be present in the passed db
		tempDB := assertstate.TemporaryDB(st)
		if err := tempDB.Add(model); err != nil {
			return fmt.Errorf("cannot create a temporary database with model: %v", err)
		}
		db = tempDB
	} else {
		db = assertstate.DB(st)
	}
	defer func() {
		if err == nil {
			return
		}
		if err := purgeNewSystemSnapFiles(filepath.Join(systemDirectory, "snapd-new-file-log")); err != nil {
			logger.Noticef("when removing seed files: %v", err)
		}
		// this is ok, as before the change with this task was created,
		// we checked that the system directory did not exist; it may
		// exist now if one of the post-create steps failed, or the the
		// task is being re-run after a reboot and creating a system
		// failed
		if err := os.RemoveAll(systemDirectory); err != nil && !os.IsNotExist(err) {
			logger.Noticef("when removing recovery system %q: %v", label, err)
		}
		if err := boot.DropRecoverySystem(remodelCtx, label); err != nil {
			logger.Noticef("when dropping the recovery system %q: %v", label, err)
		}
		// we could have reentered the task after a reboot, but the
		// state was set up sufficiently such that the system was
		// actually tried and ended up in the tried systems list, which
		// we should reset now
		st.Set("tried-systems", nil)
	}()
	// 1. prepare recovery system from remodel snaps (or current snaps)
	// TODO: this fails when there is a partially complete system seed which
	// creation could have been interrupted by an unexpected reboot;
	// consider clearing the recovery system directory and restarting from
	// scratch
	_, err = createSystemForModelFromValidatedSnaps(model, label, db, infoGetter, observeSnapFileWrite)
	if err != nil {
		return fmt.Errorf("cannot create a recovery system with label %q for %v: %v", label, model.Model(), err)
	}
	logger.Debugf("recovery system dir: %v", systemDirectory)

	// 2. keep track of the system in task state
	if err := setTaskRecoverySystemSetup(t, setup); err != nil {
		return fmt.Errorf("cannot record recovery system setup state: %v", err)
	}

	// during a remodel, we will always test the system. this handles the case
	// that the task was created prior to a snapd update, so setup.TestSystem
	// may have defaulted to false
	skipSystemTest := !setup.TestSystem && !remodelCtx.ForRemodeling()

	// if we do not need to test the system (testing not requested and task is
	// not part of a remodel), then we immediately promote the system and mark
	// it as ready to use
	if skipSystemTest {
		if err := boot.PromoteTriedRecoverySystem(remodelCtx, label, []string{label}); err != nil {
			return fmt.Errorf("cannot promote recovery system %q: %v", label, err)
		}

		model := remodelCtx.Model()

		if err := markSystemRecoveryCapableAndDefault(t, setup.MarkDefault, label, model); err != nil {
			return err
		}

		return nil
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
	logger.Noticef("restarting into candidate system %q", label)
	return snapstate.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystemNow, nil)
}

func (m *DeviceManager) undoCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx, err := DeviceCtx(st, t, nil)
	if err != nil {
		return err
	}

	if !remodelCtx.IsCoreBoot() {
		return fmt.Errorf("cannot create recovery systems on a classic (non-hybrid) system")
	}

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return fmt.Errorf("internal error: cannot obtain recovery system setup information")
	}
	label := setup.Label

	var undoErr error

	skipSystemTest := !setup.TestSystem && !remodelCtx.ForRemodeling()

	// if we were not planning on testing the system , then we need to undo
	// marking the system as seeded and recovery capable
	if skipSystemTest {
		// TODO: should this error go in undoErr, rather than just being logged?
		// this undoes what happens in markSystemRecoveryCapableAndDefault
		if err := unmarkSystemRecoveryCapableAndDefault(t, label); err != nil {
			t.Logf("when deleting and unmarking seeded system: %v", err)
		}
	}

	if err := purgeNewSystemSnapFiles(filepath.Join(setup.Directory, "snapd-new-file-log")); err != nil {
		t.Logf("when removing seed files: %v", err)
	}
	if err := os.RemoveAll(setup.Directory); err != nil && !os.IsNotExist(err) {
		t.Logf("when removing recovery system %q: %v", label, err)
		undoErr = err
	} else {
		t.Logf("removed recovery system directory %v", setup.Directory)
	}

	if err := boot.DropRecoverySystem(remodelCtx, label); err != nil {
		return fmt.Errorf("cannot drop a current recovery system %q: %v", label, err)
	}

	return undoErr
}

func (m *DeviceManager) doFinalizeTriedRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	if ok, _ := restart.Pending(st); ok {
		// don't continue until we are in the restarted snapd
		t.Logf("Waiting for system reboot...")
		return &state.Retry{}
	}

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

	logger.Debugf("finalize recovery system with label %q", label)

	if isRemodel {
		// so far so good, a recovery system created during remodel was
		// tested successfully
		if !strutil.ListContains(triedSystems, label) {
			// system failed, trigger undoing of everything we did so far
			return fmt.Errorf("tried recovery system %q failed", label)
		}

		// XXX: candidate system is promoted to the list of good ones once we
		// complete the whole remodel change
		logger.Debugf("recovery system created during remodel will be promoted later")
	} else {
		logger.Debugf("promoting recovery system %q", label)

		if err := boot.PromoteTriedRecoverySystem(remodelCtx, label, triedSystems); err != nil {
			return fmt.Errorf("cannot promote recovery system %q: %v", label, err)
		}

		model := remodelCtx.Model()

		if err := markSystemRecoveryCapableAndDefault(t, setup.MarkDefault, label, model); err != nil {
			return err
		}

		// tried systems should be a one item list, we can clear it now
		st.Set("tried-systems", nil)
	}

	// we are done
	t.SetStatus(state.DoneStatus)

	return nil
}

type DefaultRecoverySystem struct {
	// System is the label that is the current default recovery system.
	System string `json:"system"`
	// Model is the model that the system was derived from.
	Model string `json:"model"`
	// BrandID is the brand account ID
	BrandID string `json:"brand-id"`
	// Revision is the revision of the model assertion
	Revision int `json:"revision"`
	// Timestamp is the timestamp of the model assertion
	Timestamp time.Time `json:"timestamp"`
	// TimeMadeDefault is the timestamp when the system was made the default
	TimeMadeDefault time.Time `json:"time-made-default"`
}

func (d *DefaultRecoverySystem) sameAs(other *System) bool {
	return d != nil &&
		d.System == other.Label &&
		d.Model == other.Model.Model() &&
		d.BrandID == other.Brand.AccountID()
}

func markSystemRecoveryCapableAndDefault(t *state.Task, markDefault bool, label string, model *asserts.Model) error {
	if markDefault {
		st := t.State()

		var previousDefault DefaultRecoverySystem
		if err := st.Get("default-recovery-system", &previousDefault); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}

		t.Set("previous-default-recovery-system", previousDefault)
		st.Set("default-recovery-system", DefaultRecoverySystem{
			System:          label,
			Model:           model.Model(),
			BrandID:         model.BrandID(),
			Revision:        model.Revision(),
			Timestamp:       model.Timestamp(),
			TimeMadeDefault: time.Now(),
		})
	}

	if err := boot.MarkRecoveryCapableSystem(label); err != nil {
		return fmt.Errorf("cannot mark system %q as recovery capable", label)
	}
	return nil
}

func unmarkSystemRecoveryCapableAndDefault(t *state.Task, label string) error {
	if err := unmarkRecoverySystemDefault(t, label); err != nil {
		return fmt.Errorf("cannot unmark system as default recovery system: %w", err)
	}

	if err := boot.UnmarkRecoveryCapableSystem(label); err != nil {
		return fmt.Errorf("cannot unark system as recovery capable: %w", err)
	}

	return nil
}

func unmarkRecoverySystemDefault(t *state.Task, label string) error {
	st := t.State()

	var currentDefault DefaultRecoverySystem
	if err := st.Get("default-recovery-system", &currentDefault); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// if the current default isn't this label, then there is nothing to do.
	if currentDefault.System != label {
		return nil
	}

	var previousDefault DefaultRecoverySystem
	if err := t.Get("previous-default-recovery-system", &previousDefault); err != nil {
		// if this task doesn't have a previous default, then we know that this
		// task did not update the default, so there is nothing to do
		if errors.Is(err, state.ErrNoState) {
			return nil
		}
		return err
	}

	st.Set("default-recovery-system", previousDefault)

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

	// during a remodel, setting the system as seeded and recovery capable will
	// happen in the set-model task
	if !remodelCtx.ForRemodeling() {
		// this undoes what happens in markSystemRecoveryCapableAndDefault
		if err := unmarkSystemRecoveryCapableAndDefault(t, label); err != nil {
			return err
		}
	}

	if err := boot.DropRecoverySystem(remodelCtx, label); err != nil {
		return fmt.Errorf("cannot drop a good recovery system %q: %v", label, err)
	}

	return nil
}

func (m *DeviceManager) cleanupRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	setup, err := taskRecoverySystemSetup(t)
	if err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(setup.Directory, "snapd-new-file-log")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
