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

package confdbstate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

const (
	// pendingCachePrefix is the prefix to be concatenated with confdb IDs to
	// form a cache key used to store pending access data.
	pendingCachePrefix = "pending-confdb-"

	// schedulingCachePrefix is the prefix to be concatenated with confdb IDs to
	// form a cache key used to store access data that was unblocked and is being
	// scheduled.
	schedulingCachePrefix = "scheduling-confdb-"
)

func setupConfdbHook(st *state.State, snapName, hookName string, ignoreError bool) *state.Task {
	hookSup := &hookstate.HookSetup{
		Snap:        snapName,
		Hook:        hookName,
		Optional:    true,
		IgnoreError: ignoreError,
	}
	summary := fmt.Sprintf(i18n.G("Run hook %s of snap %q"), hookName, snapName)
	task := hookstate.HookTask(st, summary, hookSup, nil)
	return task
}

type ConfdbManager struct{}

func Manager(st *state.State, hookMgr *hookstate.HookManager, runner *state.TaskRunner) *ConfdbManager {
	snapstate.IsConfdbHookname = IsConfdbHookname
	hookstate.IsConfdbHookname = IsConfdbHookname

	m := &ConfdbManager{}

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-confdb-tx", m.doCommitTransaction, nil)
	// only activated on undo, to clear the ongoing transaction from state and
	// unblock others who may be waiting for it
	runner.AddHandler("clear-confdb-tx-on-error", m.noop, m.clearOngoingTransaction)
	runner.AddHandler("clear-confdb-tx", m.clearOngoingTransaction, nil)
	runner.AddHandler("load-confdb-change", m.doLoadDataIntoChange, nil)

	hookMgr.Register(regexp.MustCompile("^change-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &changeViewHandler{ctx: context}
	})
	hookMgr.Register(regexp.MustCompile("^save-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &saveViewHandler{ctx: context}
	})
	hookMgr.Register(regexp.MustCompile("^observe-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &hookstate.SnapHookHandler{}
	})
	hookMgr.Register(regexp.MustCompile("^query-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &hookstate.SnapHookHandler{}
	})
	hookMgr.Register(regexp.MustCompile("^load-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &hookstate.SnapHookHandler{}
	})

	return m
}

func (m *ConfdbManager) Ensure() error { return nil }

func (m *ConfdbManager) doCommitTransaction(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	confdbAssert, err := assertstateConfdbSchema(st, tx.ConfdbAccount, tx.ConfdbName)
	if err != nil {
		return err
	}
	schema := confdbAssert.Schema().DatabagSchema

	hasSaveViewHook := false
	for _, task := range t.Change().Tasks() {
		if task.Kind() != "run-hook" {
			continue
		}

		var hooksup hookstate.HookSetup
		err := task.Get("hook-setup", &hooksup)
		if err != nil {
			return fmt.Errorf(`internal error: cannot get "hook-setup" from run-hook task: %w`, err)
		}

		if strings.HasPrefix(hooksup.Hook, "save-view-") {
			hasSaveViewHook = true
			break
		}
	}

	// we error early if a write may affect ephemeral data but no save-view hook
	// is present. However, a change-view hook may have written to an ephemeral
	// path after that so we have to check again
	if !hasSaveViewHook {
		var viewName string
		err = t.Get("view", &viewName)
		if err != nil {
			return fmt.Errorf(`internal error: cannot get "view" from task: %w`, err)
		}

		view := confdbAssert.Schema().View(viewName)
		paths := tx.AlteredPaths()
		mightAffectEph, err := view.WriteAffectsEphemeral(paths)
		if err != nil {
			return fmt.Errorf("cannot commit transaction: cannot check for ephemeral paths: %v", err)
		}

		if mightAffectEph {
			return fmt.Errorf("cannot commit transaction: write may affect ephemeral data but no save-view hook is present")
		}
	}

	return tx.Commit(st, schema)
}

func (m *ConfdbManager) clearOngoingTransaction(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, txTask, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	err = unsetOngoingTransaction(st, tx.ConfdbAccount, tx.ConfdbName, txTask.ID())
	if err != nil {
		return err
	}

	return nil
}

func (m *ConfdbManager) doLoadDataIntoChange(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	var viewName string
	err = t.Get("view-name", &viewName)
	if err != nil {
		return fmt.Errorf(`internal error: cannot get "view-name" from task: %w`, err)
	}

	var requests []string
	err = t.Get("requests", &requests)
	if err != nil {
		return fmt.Errorf(`internal error: cannot get "requests" from task: %w`, err)
	}

	view, err := GetView(st, tx.ConfdbAccount, tx.ConfdbName, viewName)
	if err != nil {
		return fmt.Errorf("internal error: cannot get view: %w", err)
	}

	var cstrs map[string]any
	err = t.Get("constraints", &cstrs)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf(`internal error: cannot get "constraints" from task: %w`, err)
	}
	var userAccess confdb.Access
	err = t.Get("user-access", &userAccess)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf(`internal error: cannot get "user-access" from task: %w`, err)
	}

	return readViewIntoChange(t.Change(), tx, view, requests, cstrs, userAccess)
}

func readViewIntoChange(chg *state.Change, tx *Transaction, view *confdb.View, requests []string, constraints map[string]any, userAccess confdb.Access) error {
	var apiData map[string]any
	err := chg.Get("api-data", &apiData)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	if apiData == nil {
		apiData = make(map[string]any)
	}

	result, err := GetViaView(tx, view, requests, constraints, userAccess)
	if err != nil {
		if !errors.Is(err, &confdb.NoDataError{}) {
			// other errors (no match/view) would be detected before the change is created
			return fmt.Errorf("internal error: cannot read confdb %s/%s: %w", tx.ConfdbAccount, tx.ConfdbName, err)
		}

		apiData["error"] = map[string]any{
			"message": err.Error(),
			"kind":    client.ErrorKindConfigNoSuchOption,
		}
	} else {
		apiData["values"] = result
	}

	chg.Set("api-data", apiData)
	return nil
}

type confdbTransactions struct {
	ReadTxIDs []string `json:"read-tx-ids,omitempty"`
	WriteTxID string   `json:"write-tx-id,omitempty"`

	// Pending holds accesses that are waiting to be scheduled. It's read from
	// the state cache so it's only kept in-memory, never persisted into state.
	Pending []access `json:"-"`

	// Scheduling holds accesses that have been unblocked (moved from pending)
	// but have not yet finished scheduling tasks/exiting.
	Scheduling []access `json:"-"`
}

// CanStartReadTx returns true if there isn't a write transaction running or
// waiting to run.
func (txs *confdbTransactions) CanStartReadTx() bool {
	if txs.WriteTxID != "" {
		return false
	}

	accesses := append([]access{}, txs.Pending...)
	accesses = append(accesses, txs.Scheduling...)

	for _, access := range accesses {
		if access.AccessType == writeAccess {
			return false
		}
	}

	return true
}

// CanStartWriteTx returns true if there is no access currently running or
// waiting to run.
func (txs *confdbTransactions) CanStartWriteTx() bool {
	return txs.WriteTxID == "" && len(txs.ReadTxIDs) == 0 &&
		len(txs.Pending) == 0 && len(txs.Scheduling) == 0
}

// addReadTransaction adds a read transaction for the specified confdb, if no
// write transactions is ongoing. If a accessID is passed in, it'll be removed
// from the Scheduling list. The state must be locked by the caller.
func addReadTransaction(st *state.State, account, confdbName, id, accessID string) error {
	txs, updateTxStateFunc, err := getOngoingTxs(st, account, confdbName)
	if err != nil {
		return err
	}

	for i, acc := range txs.Scheduling {
		if acc.ID == accessID {
			txs.Scheduling = append(txs.Scheduling[:i], txs.Scheduling[i+1:]...)
			break
		}
	}

	if txs.WriteTxID != "" {
		// shouldn't happen save for programmer error
		return fmt.Errorf("internal error: cannot read confdb (%s/%s): a write transaction is ongoing", account, confdbName)
	}

	txs.ReadTxIDs = append(txs.ReadTxIDs, id)
	updateTxStateFunc(txs)
	return nil
}

// setWriteTransaction sets a write transaction for the specified confdb schema,
// if no other transactions (read or write) are ongoing. If a accessID is passed
// in, it'll be removed from the Scheduling list. The state must be locked by
// the caller.
func setWriteTransaction(st *state.State, account, schemaName, id, accessID string) error {
	txs, updateTxStateFunc, err := getOngoingTxs(st, account, schemaName)
	if err != nil {
		return err
	}

	for i, acc := range txs.Scheduling {
		if acc.ID == accessID {
			txs.Scheduling = append(txs.Scheduling[:i], txs.Scheduling[i+1:]...)
			break
		}
	}

	if txs.WriteTxID != "" || len(txs.ReadTxIDs) != 0 {
		op := "read"
		if txs.WriteTxID != "" {
			op = "write"
		}

		// shouldn't happen save for programmer error
		return fmt.Errorf("internal error: cannot write confdb (%s/%s): a %s transaction is ongoing", account, schemaName, op)
	}

	txs.WriteTxID = id
	updateTxStateFunc(txs)
	return nil
}

// getOngoingTxs returns a representation of the ongoing and pending transaction
// for the given confdb schema. It's never nil even if there no transactions.
// It also returns a function to update the state with a modified struct, which
// should be used without unlocking and re-locking the state.
func getOngoingTxs(st *state.State, account, schemaName string) (ongoingTxs *confdbTransactions, updateTxStateFunc func(*confdbTransactions), err error) {
	var confdbTxs map[string]*confdbTransactions
	err = st.Get("confdb-ongoing-txs", &confdbTxs)
	if err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return nil, nil, err
		}

		confdbTxs = make(map[string]*confdbTransactions, 1)
	}

	ref := account + "/" + schemaName
	if confdbTxs[ref] == nil {
		confdbTxs[ref] = &confdbTransactions{}
	}

	updateTxStateFunc = func(ongoingTxs *confdbTransactions) {
		if ongoingTxs == nil || (ongoingTxs.WriteTxID == "" && len(ongoingTxs.ReadTxIDs) == 0) {
			delete(confdbTxs, ref)
		} else {
			confdbTxs[ref] = ongoingTxs
		}

		if len(confdbTxs) == 0 {
			st.Set("confdb-ongoing-txs", nil)
		} else {
			st.Set("confdb-ongoing-txs", confdbTxs)
		}

		if len(ongoingTxs.Pending) == 0 {
			st.Cache(pendingCachePrefix+ref, nil)
		} else {
			st.Cache(pendingCachePrefix+ref, ongoingTxs.Pending)
		}

		if len(ongoingTxs.Scheduling) == 0 {
			st.Cache(schedulingCachePrefix+ref, nil)
		} else {
			st.Cache(schedulingCachePrefix+ref, ongoingTxs.Scheduling)
		}
	}

	cached := st.Cached(pendingCachePrefix + ref)
	if cached != nil {
		queue, ok := cached.([]access)
		if !ok {
			return nil, nil, fmt.Errorf("internal error: cannot access confdb pending transaction queue")
		}
		confdbTxs[ref].Pending = queue
	}

	cached = st.Cached(schedulingCachePrefix + ref)
	if cached != nil {
		queue, ok := cached.([]access)
		if !ok {
			return nil, nil, fmt.Errorf("internal error: cannot access confdb scheduling list")
		}
		confdbTxs[ref].Scheduling = queue
	}

	return confdbTxs[ref], updateTxStateFunc, nil
}

// Removes the transaction represented by the id from the tracked state. The
// state must be locked by the caller.
func unsetOngoingTransaction(st *state.State, account, schemaName, id string) error {
	txs, updateTxStateFunc, err := getOngoingTxs(st, account, schemaName)
	if err != nil {
		return err
	}
	defer updateTxStateFunc(txs)

	if txs.WriteTxID == id {
		txs.WriteTxID = ""
	} else {
		for i, txID := range txs.ReadTxIDs {
			if txID == id {
				txs.ReadTxIDs = append(txs.ReadTxIDs[:i], txs.ReadTxIDs[i+1:]...)
				break
			}
		}
	}

	if len(txs.ReadTxIDs) > 0 {
		// there are other transactions running (can only be reads) so skip this.
		// The last one will unblock the next accesses
		return nil
	}

	return maybeUnblockAccesses(txs)
}

// maybeUnblockAccesses unblocks as many consecutive pending accesses as
// possible, either one write or one or more sequential reads.
// This may be a no-op, if there are:
//   - no pending changes (i.e., there's nothing to unblock)
//   - changes running for other transactions - pending accesses would've been
//     scheduled w/o waiting if they could (see waitForAccess) so any pending
//     accesses are guaranteed to be incompatible.
//   - accesses that have been unblocked but are still scheduling changes. If we
//     unblocked accesses here, they would race with the ones already scheduling
//
// If accesses are unblocked, they're removed from the Pending list and put into
// the Scheduling list so we can track unblocked but still unscheduled accesses.
func maybeUnblockAccesses(txs *confdbTransactions) error {
	if len(txs.Pending) == 0 || txs.WriteTxID != "" || len(txs.ReadTxIDs) > 0 || len(txs.Scheduling) != 0 {
		return nil
	}

	var upTo int
	for i, acc := range txs.Pending {
		if acc.AccessType == writeAccess {
			if i == 0 {
				acc.WaitChan <- struct{}{}
				logger.Debugf("unblocking pending %s access %s", acc.AccessType, acc.ID)
				upTo = i
			}

			break
		}

		acc.WaitChan <- struct{}{}
		logger.Debugf("unblocking pending %s access %s", acc.AccessType, acc.ID)
		upTo = i
	}

	txs.Scheduling = append([]access{}, txs.Pending[:upTo+1]...)
	txs.Pending = txs.Pending[upTo+1:]

	return nil
}

func (m *ConfdbManager) noop(*state.Task, *tomb.Tomb) error { return nil }

type changeViewHandler struct {
	ctx *hookstate.Context
}

func (h *changeViewHandler) Before() error                 { return nil }
func (h *changeViewHandler) Error(err error) (bool, error) { return false, nil }

func (h *changeViewHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	tx, _, _, err := GetStoredTransaction(t)
	if err != nil {
		return fmt.Errorf("cannot get transaction in change-confdb handler: %v", err)
	}

	if tx.aborted() {
		return fmt.Errorf("cannot change confdb %s/%s: %s rejected changes: %s", tx.ConfdbAccount, tx.ConfdbName, tx.abortingSnap, tx.abortReason)
	}

	return nil
}

type saveViewHandler struct {
	ctx *hookstate.Context
}

func (h *saveViewHandler) Before() error { return nil }

func (h *saveViewHandler) Error(origErr error) (ignoreErr bool, err error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	st := h.ctx.State()

	var commitTaskID string
	if err := t.Get("tx-task", &commitTaskID); err != nil {
		return false, err
	}

	// save the tasks after the failed task so we can insert rollback tasks between them
	haltTasks := t.HaltTasks()

	// create roll back tasks for the previously done save-confdb hooks (starting
	// with the hook that failed, so it tries to overwrite with a pristine databag)
	last := t
	for curTask := t; curTask.Kind() == "run-hook"; curTask = curTask.WaitTasks()[0] {
		var hooksup hookstate.HookSetup
		if err := curTask.Get("hook-setup", &hooksup); err != nil {
			return false, err
		}

		if !strings.HasPrefix(hooksup.Hook, "save-view-") {
			break
		}

		// if we fail to rollback, there's nothing we can do
		ignoreError := true
		rollbackTask := setupConfdbHook(st, hooksup.Snap, hooksup.Hook, ignoreError)
		rollbackTask.Set("tx-task", commitTaskID)
		rollbackTask.WaitFor(last)
		curTask.Change().AddTask(rollbackTask)
		last = rollbackTask

		// should never happen but let's be careful for now
		if len(curTask.WaitTasks()) != 1 {
			return false, fmt.Errorf("internal error: cannot rollback failed save-view: expected one prerequisite task")
		}
	}

	// prevent the next confdb tasks from running before the rollbacks. Once the
	// last rollback task errors, these will be put on Hold by the usual mechanism
	for _, halt := range haltTasks {
		halt.WaitFor(last)
	}

	// save the original error so we can return that once the rollback is done
	last.Set("original-error", origErr.Error())

	tx, _, saveChanges, err := GetStoredTransaction(t)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-view: cannot get transaction: %v", err)
	}

	// clear the transaction changes
	err = tx.Clear(st)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-view: cannot clear transaction changes: %v", err)
	}
	saveChanges()

	// ignore error for now so we run again to try to undo any committed data
	return true, nil
}

func (h *saveViewHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()

	var origErr string
	if err := t.Get("original-error", &origErr); err != nil {
		if errors.Is(err, state.ErrNoState) {
			// no original-error, this isn't the last hook in a rollback, nothing to do
			return nil
		}
		return err
	}

	logger.Noticef("successfully rolled back failed save-view hooks")
	// we're finished rolling back, so return the original error
	return errors.New(origErr)
}
