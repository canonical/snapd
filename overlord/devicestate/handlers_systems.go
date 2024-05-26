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

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(t.Get("recovery-system-setup", &setup))
	if err == nil {
		return &setup, nil
	}
	if !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	// find the task which holds the data
	var id string
	mylog.Check(t.Get("recovery-system-setup-task", &id))

	ts := t.State().Task(id)
	if ts == nil {
		return nil, fmt.Errorf("internal error: cannot find referenced task %v", id)
	}
	mylog.Check(ts.Get("recovery-system-setup", &setup))

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
	currentLog := mylog.Check2(os.ReadFile(logfile))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	modifiedLog := bytes.NewBuffer(currentLog)
	fmt.Fprintln(modifiedLog, fileName)
	return osutil.AtomicWriteFile(logfile, modifiedLog.Bytes(), 0644, 0)
}

func purgeNewSystemSnapFiles(logfile string) error {
	f := mylog.Check2(os.Open(logfile))

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
		if mylog.Check(os.Remove(fileName)); err != nil && !os.IsNotExist(err) {
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

		sd := mylog.Check2(seed.Open(dirs.SnapSeedDir, sys.Label))
		mylog.Check(sd.LoadAssertions(nil, func(*asserts.Batch) error {
			return nil
		}))
		mylog.Check(sd.LoadMeta(seed.AllModes, nil, timings.New(nil)))
		mylog.Check(sd.Iter(func(sn *seed.Snap) error {
			if sn.ID() != "" {
				requiredByOtherSystems[sn.Path] = true
			}
			return nil
		}))

	}

	targetSeed := mylog.Check2(seed.Open(dirs.SnapSeedDir, target))
	mylog.Check(targetSeed.LoadAssertions(nil, func(*asserts.Batch) error {
		return nil
	}))
	mylog.Check(targetSeed.LoadMeta(seed.AllModes, nil, timings.New(nil)))

	var uniqueToTarget []string
	mylog.Check(targetSeed.Iter(func(sn *seed.Snap) error {
		if sn.ID() != "" && !requiredByOtherSystems[sn.Path] {
			uniqueToTarget = append(uniqueToTarget, sn.Path)
		}
		return nil
	}))

	return uniqueToTarget, nil
}

func (m *DeviceManager) doRemoveRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	systems := mylog.Check2(m.systems())

	var setup removeRecoverySystemSetup
	mylog.Check(t.Get("remove-recovery-system-setup", &setup))

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

	deviceCtx := mylog.Check2(DeviceCtx(st, t, nil))

	// if this task has partially run before, then we might have already removed
	// some files required to load the being-removed system seed. to avoid this,
	// we first check if we already have stored a list of snaps to remove
	// (meaning, this task is being re-run). if the list isn't present, then we
	// calculate it and store it in the task state for potential future re-runs.
	var snapsToRemove uniqueSnapsInRecoverySystem
	mylog.Check(t.Get("snaps-to-remove", &snapsToRemove))

	// we need to unlock and re-lock the state to make sure that
	// snaps-to-remove is persisted. if we ever change how the exclusive
	// changes are handled, then we might need to revisit this.

	recoverySystemsDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems")
	mylog.Check(boot.DropRecoverySystem(deviceCtx, setup.Label))

	for _, sn := range snapsToRemove.SnapPaths {
		path := filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps", filepath.Base(sn))
		mylog.Check(os.RemoveAll(path))

	}
	mylog.Check(os.RemoveAll(filepath.Join(recoverySystemsDir, setup.Label)))

	t.SetStatus(state.DoneStatus)

	return nil
}

func (m *DeviceManager) doCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx := mylog.Check2(DeviceCtx(st, t, nil))

	if !remodelCtx.IsCoreBoot() {
		return fmt.Errorf("cannot create recovery systems on a classic (non-hybrid) system")
	}

	model := remodelCtx.Model()
	isRemodel := remodelCtx.ForRemodeling()

	setup := mylog.Check2(taskRecoverySystemSetup(t))

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

			snapf := mylog.Check2(snapfile.Open(l.Path))

			info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, l.SideInfo))

			return info, l.Path, true, nil
		}

		// in a remodel scenario, the snaps may need to be fetched and thus
		// their content can be different from what we have in already installed
		// snaps, so we should first check the download tasks before consulting
		// snapstate
		logger.Debugf("requested info for snap %q being installed during remodel", name)
		for _, tskID := range setup.SnapSetupTasks {
			taskWithSnapSetup := st.Task(tskID)
			snapsup := mylog.Check2(snapstate.TaskSnapSetup(taskWithSnapSetup))

			if snapsup.SnapName() != name {
				continue
			}
			// by the time this task runs, the file has already been
			// downloaded and validated
			snapFile := mylog.Check2(snapfile.Open(snapsup.MountFile()))

			info = mylog.Check2(snap.ReadInfoFromSnapFile(snapFile, snapsup.SideInfo))

			return info, info.MountFile(), true, nil
		}

		// either a remodel scenario, in which case the snap is not
		// among the ones being fetched, or just creating a recovery
		// system, in which case we use the snaps that are already
		// installed

		info = mylog.Check2(snapstate.CurrentInfo(st, name))
		if err == nil {
			hash, _ := mylog.Check3(asserts.SnapFileSHA3_384(info.MountFile()))

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
		mylog.Check(tempDB.Add(model))

		db = tempDB
	} else {
		db = assertstate.DB(st)
	}
	defer func() {
		if err == nil {
			return
		}
		mylog.Check(purgeNewSystemSnapFiles(filepath.Join(systemDirectory, "snapd-new-file-log")))

		// this is ok, as before the change with this task was created,
		// we checked that the system directory did not exist; it may
		// exist now if one of the post-create steps failed, or the the
		// task is being re-run after a reboot and creating a system
		// failed
		if mylog.Check(os.RemoveAll(systemDirectory)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("when removing recovery system %q: %v", label, err)
		}
		mylog.Check(boot.DropRecoverySystem(remodelCtx, label))

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
	_ = mylog.Check2(createSystemForModelFromValidatedSnaps(model, label, db, infoGetter, observeSnapFileWrite))

	logger.Debugf("recovery system dir: %v", systemDirectory)
	mylog.Check(

		// 2. keep track of the system in task state
		setTaskRecoverySystemSetup(t, setup))

	// during a remodel, we will always test the system. this handles the case
	// that the task was created prior to a snapd update, so setup.TestSystem
	// may have defaulted to false
	skipSystemTest := !setup.TestSystem && !remodelCtx.ForRemodeling()

	// if we do not need to test the system (testing not requested and task is
	// not part of a remodel), then we immediately promote the system and mark
	// it as ready to use
	if skipSystemTest {
		mylog.Check(boot.PromoteTriedRecoverySystem(remodelCtx, label, []string{label}))

		model := remodelCtx.Model()
		mylog.Check(markSystemRecoveryCapableAndDefault(t, setup.MarkDefault, label, model))

		return nil
	}
	mylog.Check(

		// 3. set up boot variables for tracking the tried system state
		boot.SetTryRecoverySystem(remodelCtx, label))
	mylog.Check(
		// rollback?

		// 4. and set up the next boot that that system
		boot.SetRecoveryBootSystemAndMode(remodelCtx, label, "recover"))

	// this task is done, further processing happens in finalize
	logger.Noticef("restarting into candidate system %q", label)
	return snapstate.FinishTaskWithRestart(t, state.DoneStatus, restart.RestartSystemNow, nil)
}

func (m *DeviceManager) undoCreateRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx := mylog.Check2(DeviceCtx(st, t, nil))

	if !remodelCtx.IsCoreBoot() {
		return fmt.Errorf("cannot create recovery systems on a classic (non-hybrid) system")
	}

	setup := mylog.Check2(taskRecoverySystemSetup(t))

	label := setup.Label

	var undoErr error

	skipSystemTest := !setup.TestSystem && !remodelCtx.ForRemodeling()

	// if we were not planning on testing the system , then we need to undo
	// marking the system as seeded and recovery capable
	if skipSystemTest {
		mylog.Check(
			// TODO: should this error go in undoErr, rather than just being logged?
			// this undoes what happens in markSystemRecoveryCapableAndDefault
			unmarkSystemRecoveryCapableAndDefault(t, label))
	}
	mylog.Check(purgeNewSystemSnapFiles(filepath.Join(setup.Directory, "snapd-new-file-log")))

	if mylog.Check(os.RemoveAll(setup.Directory)); err != nil && !os.IsNotExist(err) {
		t.Logf("when removing recovery system %q: %v", label, err)
		undoErr = err
	} else {
		t.Logf("removed recovery system directory %v", setup.Directory)
	}
	mylog.Check(boot.DropRecoverySystem(remodelCtx, label))

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

	remodelCtx := mylog.Check2(DeviceCtx(st, t, nil))

	isRemodel := remodelCtx.ForRemodeling()

	var triedSystems []string
	mylog.Check(
		// after rebooting to the recovery system and back, the system got moved
		// to the tried-systems list in the state
		st.Get("tried-systems", &triedSystems))

	setup := mylog.Check2(taskRecoverySystemSetup(t))

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
		mylog.Check(boot.PromoteTriedRecoverySystem(remodelCtx, label, triedSystems))

		model := remodelCtx.Model()
		mylog.Check(markSystemRecoveryCapableAndDefault(t, setup.MarkDefault, label, model))

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
		if mylog.Check(st.Get("default-recovery-system", &previousDefault)); err != nil && !errors.Is(err, state.ErrNoState) {
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
	mylog.Check(boot.MarkRecoveryCapableSystem(label))

	return nil
}

func unmarkSystemRecoveryCapableAndDefault(t *state.Task, label string) error {
	mylog.Check(unmarkRecoverySystemDefault(t, label))
	mylog.Check(boot.UnmarkRecoveryCapableSystem(label))

	return nil
}

func unmarkRecoverySystemDefault(t *state.Task, label string) error {
	st := t.State()

	var currentDefault DefaultRecoverySystem
	if mylog.Check(st.Get("default-recovery-system", &currentDefault)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// if the current default isn't this label, then there is nothing to do.
	if currentDefault.System != label {
		return nil
	}

	var previousDefault DefaultRecoverySystem
	mylog.Check(t.Get("previous-default-recovery-system", &previousDefault))
	// if this task doesn't have a previous default, then we know that this
	// task did not update the default, so there is nothing to do

	st.Set("default-recovery-system", previousDefault)

	return nil
}

func (m *DeviceManager) undoFinalizeTriedRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	remodelCtx := mylog.Check2(DeviceCtx(st, t, nil))

	setup := mylog.Check2(taskRecoverySystemSetup(t))

	label := setup.Label

	// during a remodel, setting the system as seeded and recovery capable will
	// happen in the set-model task
	if !remodelCtx.ForRemodeling() {
		mylog.Check(
			// this undoes what happens in markSystemRecoveryCapableAndDefault
			unmarkSystemRecoveryCapableAndDefault(t, label))
	}
	mylog.Check(boot.DropRecoverySystem(remodelCtx, label))

	return nil
}

func (m *DeviceManager) cleanupRecoverySystem(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	setup := mylog.Check2(taskRecoverySystemSetup(t))

	if mylog.Check(os.Remove(filepath.Join(setup.Directory, "snapd-new-file-log"))); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
