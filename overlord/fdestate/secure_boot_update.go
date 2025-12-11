// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
package fdestate

import (
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
)

var (
	backendResealKeysForSignaturesDBUpdate = backend.ResealKeysForSignaturesDBUpdate
)

var fdeEfiSecurebootDbUpdateChangeKind = swfeats.RegisterChangeKind("fde-efi-secureboot-db-update")

type EFISecurebootKeyDatabase int

const (
	EFISecurebootPK EFISecurebootKeyDatabase = iota
	EFISecurebootKEK
	EFISecurebootDB
	EFISecurebootDBX
)

func (db EFISecurebootKeyDatabase) String() string {
	switch db {
	case EFISecurebootPK:
		return "PK"
	case EFISecurebootKEK:
		return "KEK"
	case EFISecurebootDB:
		return "DB"
	case EFISecurebootDBX:
		return "DBX"
	default:
		return fmt.Sprintf("unknown EFISecurebootKeyDatabase: <%d>", db)
	}
}

// EFISecurebootDBUpdatePrepare notifies that the local EFI key
// database manager is about to update the database.
func EFISecurebootDBUpdatePrepare(st *state.State, db EFISecurebootKeyDatabase, payload []byte) error {
	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err != nil {
		if err == device.ErrNoSealedKeys {
			return nil
		}
		return err
	}

	st.Lock()
	defer st.Unlock()

	if err := checkSecurebootChangeConflicts(st, db); err != nil {
		return err
	}

	if err := checkFDEChangeConflict(st); err != nil {
		return err
	}

	op, err := addEFISecurebootDBUpdateChange(st, method, db, payload)
	if err != nil {
		return err
	}

	chgID := op.ChangeID

	chg := st.Change(chgID)

	// we're good so far, kick off the change
	st.EnsureBefore(0)

	// we want to synchronize with the prepare task completing successfully as
	// at this point the keys will have been resealed with the proposed update
	// payload
	chgFailed := false
	afterPrepareOKC := securebootUpdatePreparedOKChan(st, chgID)
	st.Unlock()
	// there is no timeout as we expect to observe one of the two outcomes: we
	// either complete the prepare step successfully or the change fails (and
	// becomes ready); we are not holding the state lock, so other processing
	// tasks not blocked
	select {
	case <-afterPrepareOKC:
		// prepare step has completed successfully
	case <-chg.Ready():
		// change failed unexpectedly
		chgFailed = true
	}
	st.Lock()

	if chgFailed {
		// The change is unexpectedly ready, which means that the prepare task
		// has failed. Need to ensure that the pending operation is either in a
		// failed state, or gone (which would be achieved by cleanup).
		op, err = findFirstExternalOperationByChangeID(st, chgID)
		if err != nil {
			return fmt.Errorf("internal error: cannot look up external operation by change ID: %w", err)
		}

		err = chg.Err()

		if op != nil {
			// it's still there, so let's update the status so that it does not
			// block other operations
			op.SetFailed(fmt.Sprintf("prepare task failed early: %v", err))
			updateExternalOperation(st, op)
		}
		return fmt.Errorf("prepare change failed: %w", err)
	}

	return nil
}

// EFISecurebootDBUpdateCleanup notifies that the local EFI key database manager
// has reached a cleanup stage of the update process.
func EFISecurebootDBUpdateCleanup(st *state.State) error {
	if _, err := device.SealedKeysMethod(dirs.GlobalRootDir); err == device.ErrNoSealedKeys {
		return nil
	} else if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	op, err := findFirstPendingExternalOperationByKind(st, "fde-efi-secureboot-db-update")
	if err != nil {
		return err
	}

	if op == nil {
		logger.Debugf("no pending Secureboot Key Database update request for cleanup")
		return nil
	}

	// ensure that a cleanup can only be called when operation has obtained
	// 'Doing' status which prevents attempting cleanup when we briefly unlock
	// the state doing the initial reseal for prepare in the 'do' path, and
	// similarly in the 'undo' path
	if op.Status != DoingStatus {
		return &snapstate.ChangeConflictError{
			ChangeKind: "fde-efi-secureboot-db-update",
			Message:    "cannot perform Secureboot Key Database update 'cleanup' action when conflicting actions are in progress",
		}
	}

	// mark as successful
	op.SetStatus(CompletingStatus)

	if err := updateExternalOperation(st, op); err != nil {
		return err
	}

	chg := st.Change(op.ChangeID)
	// complete unlocks the state waiting for change to become ready
	return completeEFISecurebootDBUpdateChange(chg)
}

// EFISecurebootDBManagerStartup indicates that the local EFI key database
// manager has started.
func EFISecurebootDBManagerStartup(st *state.State) error {
	if _, err := device.SealedKeysMethod(dirs.GlobalRootDir); err == device.ErrNoSealedKeys {
		return nil
	} else if err != nil {
		return err
	}

	st.Lock()
	defer st.Unlock()

	op, err := findFirstPendingExternalOperationByKind(st, "fde-efi-secureboot-db-update")
	if err != nil {
		return err
	}

	if op == nil {
		logger.Debugf("no pending Secureboot Key Database request")
		return nil
	}

	// at this point we have a pending request, which means we must mark it as
	// failed and reseal with the current content of EFI PK/KEK/DB/DBX

	// ensure that the external operation has obtained 'Doing' status which
	// prevents attempting startup/cleanup when we briefly unlock the state
	// doing the initial reseal for prepare in the 'do' path, and similarly in
	// the 'undo' path
	if op.Status != DoingStatus {
		return &snapstate.ChangeConflictError{
			ChangeKind: "fde-efi-secureboot-db-update",
			Message:    "cannot perform Secureboot Key Database 'startup' action when conflicting actions are in progress",
		}
	}

	op.SetStatus(AbortingStatus)
	op.Err = "'startup' action invoked while an operation is in progress"
	if err := updateExternalOperation(st, op); err != nil {
		return nil
	}

	chg := st.Change(op.ChangeID)
	// complete unlocks the state waiting for change to become ready
	return completeEFISecurebootDBUpdateChange(chg)
}

type securebootUpdateContext struct {
	Payload []byte               `json:"payload"`
	Method  device.SealingMethod `json:"sealing-method"`
}

// addEFISecurebootDBUpdateChange adds a state change related to the Secureboot
// update. The state must be locked by the caller.
func addEFISecurebootDBUpdateChange(
	st *state.State,
	method device.SealingMethod,
	db EFISecurebootKeyDatabase,
	payload []byte,
) (*externalOperation, error) {
	// add a change carrying 2 tasks:
	// - efi-secureboot-db-update-prepare: with a noop do, but the undo handler
	// preforms necessary cleanup
	// - efi-secureboot-db-update: waits for the external caller to indicate
	// that the action is complete or failed
	//
	// if the original requester never calls cleanup/startup, the change
	// will be pruned automatically and the undo will perform a reseal

	updateKindStr := db.String()
	tPrep := st.NewTask("efi-secureboot-db-update-prepare",
		fmt.Sprintf("Prepare for external EFI %s update", updateKindStr))
	tUpdateWait := st.NewTask("efi-secureboot-db-update",
		fmt.Sprintf("Reseal after external EFI %s update", updateKindStr))
	tUpdateWait.WaitFor(tPrep)
	ts := state.NewTaskSet(tPrep, tUpdateWait)

	chg := st.NewChange(fdeEfiSecurebootDbUpdateChangeKind,
		fmt.Sprintf("External EFI %s update", updateKindStr))
	chg.AddAll(ts)

	data, err := json.Marshal(securebootUpdateContext{
		Payload: payload,
		Method:  method,
	})
	if err != nil {
		return nil, err
	}

	op := &externalOperation{
		// match the change kind
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: chg.ID(),
		Context:  json.RawMessage(data),
		Status:   PreparingStatus,
	}

	err = addExternalOperation(st, op)
	if err != nil {
		return nil, err
	}

	setupSecurebootNotifyPrepareDoneOKChan(st, chg.ID())

	return op, nil
}

// completeEFISecurebootDBUpdateChange waits for the change to complete and
// returns the error result obtained from the change
func completeEFISecurebootDBUpdateChange(chg *state.Change) error {
	st := chg.State()

	// trigger ensure so that the task runner attempts to run our tasks
	st.EnsureBefore(0)

	// there is no timeout as we expect the change to complete, either
	// successfully or with an error; note we are not holding the state lock so
	// other tasks are not blocked
	st.Unlock()
	logger.Debugf("waiting for FDE Secureboot Key Database change %v to become ready", chg.ID())
	<-chg.Ready()
	logger.Debugf("Secureboot Key Database change complete")
	st.Lock()

	chg = st.Change(chg.ID())
	if err := chg.Err(); err != nil {
		logger.Debugf("completed Secureboot Key Database change error: %v", chg.Err())
	}

	return nil
}

// postUpdateReseal performs a reseal after a Secureboot Key Database update.
func postUpdateReseal(mgr *FDEManager, unlocker boot.Unlocker, method device.SealingMethod) error {
	return boot.WithBootChains(func(bc boot.BootChains) error {
		logger.Debugf("attempting post Secureboot Key Database reseal")

		params := &boot.ResealKeyForBootChainsParams{
			BootChains: bc,
			Options: boot.ResealKeyToModeenvOptions{
				ExpectReseal:  true,
				RevokeOldKeys: true,
			},
		}
		return mgr.resealKeyForBootChains(unlocker, method, dirs.GlobalRootDir, params)
	}, method)
}

func (m *FDEManager) doEFISecurebootDBUpdatePrepare(t *state.Task, tomb *tomb.Tomb) error {
	// the do handler perform the initial reseal with update payload which will be
	// used during update

	st := t.State()

	st.Lock()
	defer st.Unlock()

	chgID := t.Change().ID()
	op, err := findFirstExternalOperationByChangeID(st, chgID)
	if err != nil {
		return fmt.Errorf("internal error: no matching external operation for change ID %v", chgID)
	}

	if op.Status != PreparingStatus {
		return fmt.Errorf("internal error: external operation already in state %q, but expected %q",
			op.Status, PreparingStatus)
	}

	var updateData securebootUpdateContext
	if err := json.Unmarshal(op.Context, &updateData); err != nil {
		return fmt.Errorf("cannot unmarshal Secureboot Key Database context data: %v", err)
	}

	err = func() error {
		mgr := fdeMgr(st)

		return boot.WithBootChains(func(bc boot.BootChains) error {
			// TODO: are we logging too much?
			logger.Debugf("attempting reseal for Secureboot Key Database")
			logger.Debugf("boot chains: %v\n", bc)
			logger.Debugf("Secureboot Key Database payload: %x", updateData.Payload)

			params := &boot.ResealKeyForBootChainsParams{
				BootChains: bc,
			}
			// unlocks the state internally as needed
			return backendResealKeysForSignaturesDBUpdate(
				&unlockedStateManager{
					FDEManager: mgr,
					unlocker:   st.Unlocker(),
				},
				updateData.Method, dirs.GlobalRootDir, params, updateData.Payload,
			)
		}, updateData.Method)
	}()

	if err != nil {
		err = fmt.Errorf("cannot perform initial reseal of keys for Secureboot Key Database update: %w", err)
		op.SetFailed(err.Error())
	} else {
		op.SetStatus(DoingStatus)
	}

	updateExternalOperation(st, op)

	if err == nil {
		t.SetStatus(state.DoneStatus)
		notifySecurebootUpdatePrepareDoneOK(st, chgID)
	}

	return err
}

func (m *FDEManager) undoEFISecurebootDBUpdatePrepare(t *state.Task, tomb *tomb.Tomb) error {
	// the undo handler runs when both the external change has failed, eg. due
	// to startup called after prepare, or when the task was aborted due to the
	// original not making any calls after the initial prepare
	st := t.State()

	st.Lock()
	defer st.Unlock()

	op, err := findFirstExternalOperationByChangeID(st, t.Change().ID())
	if err != nil || op == nil {
		return fmt.Errorf("internal error: cannot execute efi-dbx-update handler: %v", err)
	}

	var updateData securebootUpdateContext
	if err := json.Unmarshal(op.Context, &updateData); err != nil {
		return fmt.Errorf("cannot unmarshal Secureboot Key Database context data: %v", err)
	}

	t.Logf("Secureboot Key Database prepare undo called with operation in status: %v", op.Status)

	switch op.Status {
	case ErrorStatus:
		// operation status already indicates error, which means that it failed
		// in the efi-secureboot-db-update handler

		// TODO:FDEM: should we perform a reseal? one attempt in the 'do' handler
		// already failed
		t.Logf("action already in error state with error: %v", op.Err)
		return nil
	case DoingStatus, AbortingStatus:
		// we hit abort, the external operation is still in doing state, update its
		// state and continue the undo sequence

		mgr := fdeMgr(st)
		err = postUpdateReseal(mgr, st.Unlocker(), updateData.Method)
		if err != nil {
			t.Logf("cannot complete post update reseal in undo: %v", err)
			op.SetFailed(
				fmt.Sprintf("cannot perform post update reseal: %v, "+
					"while aborting explicitly or due to timeout waiting for subsequent request from the caller",
					err))
		} else {
			reason := "aborted explicitly or due to timeout waiting for subsequent request from the caller"
			if op.Status == AbortingStatus && op.Err != "" {
				// aborting with explicit reason
				reason = op.Err
			}
			op.SetFailed(reason)
		}

		if updateErr := updateExternalOperation(st, op); updateErr != nil {
			return updateErr
		}

		t.Logf("external action state updated to %v: %v", op.Status, op.Err)
		if err != nil {
			return fmt.Errorf("cannot complete reseal in undo: %v", err)
		}
		return nil
	}

	return fmt.Errorf("internal error: unexpected state of external action in undo handler: %v", op.Status)
}

func (m *FDEManager) doEFISecurebootDBUpdate(t *state.Task, tomb *tomb.Tomb) error {
	// the handler is running, which means that we are no longer blocked waiting
	// for the op to complete

	st := t.State()

	st.Lock()
	defer st.Unlock()

	op, err := findFirstExternalOperationByChangeID(st, t.Change().ID())
	if err != nil || op == nil {
		return fmt.Errorf("internal error: cannot execute efi-dbx-update handler: %v", err)
	}

	switch op.Status {
	case CompletingStatus:
		// handled below
	case AbortingStatus:
		// explicit error when operation got aborted
		reason := "aborted by external request"
		if op.Err != "" {
			// aborting with explicit reason
			reason = op.Err
		}
		return errors.New(reason)
	default:
		return fmt.Errorf("cannot perform post update reseal, operation in status %v", op.Status)
	}

	var updateData securebootUpdateContext
	if err := json.Unmarshal(op.Context, &updateData); err != nil {
		return fmt.Errorf("cannot unmarshal Secureboot Key Database context data: %v", err)
	}

	mgr := fdeMgr(st)
	err = postUpdateReseal(mgr, st.Unlocker(), updateData.Method)
	if err != nil {
		t.Errorf("cannot complete post update reseal: %v", err)
	}

	if err != nil {
		op.SetFailed(
			fmt.Sprintf("cannot complete post update reseal: %v, while completing due to external request", err))
	} else {
		op.SetStatus(DoneStatus)
	}

	if updateErr := updateExternalOperation(st, op); updateErr != nil {
		t.Logf("cannot update operation status: %v", updateErr)
		return updateErr
	}

	if err == nil {
		// update task status before unlocking
		t.SetStatus(state.DoneStatus)
	}

	return err
}

func (m *FDEManager) doEFISecurebootDBUpdatePrepareCleanup(t *state.Task, tomb *tomb.Tomb) error {
	st := t.State()

	st.Lock()
	defer st.Unlock()

	chgID := t.Change().ID()
	return withFdeState(st, func(fde *FdeState) (modified bool, err error) {
		for idx, op := range fde.PendingExternalOperations {
			logger.Debugf("cleaning up external operation for change %v", op.ChangeID)
			if op.ChangeID == chgID {
				fde.PendingExternalOperations = append(fde.PendingExternalOperations[:idx],
					fde.PendingExternalOperations[idx+1:]...)

				cleanupUpdatePreparedOKChan(st, chgID)

				return true, nil
			}
		}
		return false, nil
	})
}

func isEFISecurebootDBUpdateBlocked(t *state.Task) bool {
	extChg, err := findFirstExternalOperationByChangeID(t.State(), t.Change().ID())
	if err != nil {
		// error obtaining data from the state, which does not mean the
		// operation isn't there, best case, leave it running and wait for the
		// change to abort
		return true
	}

	if extChg == nil {
		// no operation, then why were we called?
		return true
	}

	switch extChg.Status {
	case CompletingStatus, AbortingStatus:
		// operation states that unblock the efi-secureboot-update-db task
		return false
	default:
		return true
	}
}

type securebootUpdatePrepareSyncKey struct{}

func setupSecurebootNotifyPrepareDoneOKChan(st *state.State, changeID string) {
	var syncChs map[string]chan struct{}

	val := st.Cached(securebootUpdatePrepareSyncKey{})
	if val == nil {
		syncChs = make(map[string]chan struct{})
	} else {
		syncChs = val.(map[string]chan struct{})
	}

	syncChs[changeID] = make(chan struct{})
	st.Cache(securebootUpdatePrepareSyncKey{}, syncChs)
}

func notifySecurebootUpdatePrepareDoneOK(st *state.State, changeID string) {
	val := st.Cached(securebootUpdatePrepareSyncKey{})

	if val != nil {
		syncChs := val.(map[string]chan struct{})
		close(syncChs[changeID])
	}
}

func securebootUpdatePreparedOKChan(st *state.State, changeID string) <-chan struct{} {
	val := st.Cached(securebootUpdatePrepareSyncKey{})

	syncChs := val.(map[string]chan struct{})
	return syncChs[changeID]
}

func cleanupUpdatePreparedOKChan(st *state.State, changeID string) {
	val := st.Cached(securebootUpdatePrepareSyncKey{})
	if val != nil {
		syncChs := val.(map[string]chan struct{})
		delete(syncChs, changeID)
	}
}
