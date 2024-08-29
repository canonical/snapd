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

package registrystate

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
)

func init() {
	hookstate.ViewChangedHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &viewChangedHandler{ctx: context}
	}
	hookstate.SaveViewHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &saveViewHandler{ctx: context}
	}
	hookstate.ChangeViewHandlerGenerator = func(context *hookstate.Context) hookstate.Handler {
		return &changeViewHandler{ctx: context}
	}
}

func Manager(st *state.State, runner *state.TaskRunner) *RegistryManager {
	m := &RegistryManager{registryChans: make(map[string]chan struct{})}
	registryMgr = m

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-transaction", m.doCommitTransaction, nil)
	// only activated on undo, to clear the ongoing transaction from state and
	// unblock others who may be waiting for it
	runner.AddHandler("clear-tx-on-error", m.noop, m.clearOngoingTransaction)
	runner.AddHandler("clear-tx", m.clearOngoingTransaction, nil)

	return m
}

var registryMgr *RegistryManager

type RegistryManager struct {
	// should only be written/read holding the state lock
	registryChans map[string]chan struct{}
}

func (m *RegistryManager) Ensure() error { return nil }

func (m *RegistryManager) doCommitTransaction(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	registryAssert, err := assertstateRegistry(st, tx.RegistryAccount, tx.RegistryName)
	if err != nil {
		return err
	}
	schema := registryAssert.Registry().Schema

	return tx.Commit(st, schema)
}

func (m *RegistryManager) clearOngoingTransaction(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	err = m.unsetOngoingTransaction(st, tx.RegistryAccount, tx.RegistryName)
	if err != nil {
		return err
	}

	m.unblockWaitingRegistryWriter(tx.RegistryAccount, tx.RegistryName)
	return nil
}

func (m *RegistryManager) noop(*state.Task, *tomb.Tomb) error { return nil }

// waitForOngoingTransaction blocks until the current transaction for the registry
// is finished, if any exists. The state should be locked by the caller and is
// released while waiting.
func (m *RegistryManager) waitForOngoingTransaction(st *state.State, account, registryName string, timeout time.Duration) error {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			return nil
		}

		return err
	}

	registryRef := account + "/" + registryName
	if taskID, ok := txCommits[registryRef]; !ok {
		// no ongoing transaction for this registry
		return nil
	} else {
		logger.Debugf("waiting for task %s to finish its transaction", taskID)
	}

	regChan, ok := registryMgr.registryChans[registryRef]
	if !ok {
		regChan = make(chan struct{}, 1)
		registryMgr.registryChans[registryRef] = regChan
	}

	st.Unlock()
	defer st.Lock()

	select {
	// NOTE: the spec (https://go.dev/ref/spec#Receive_operator) does not guarantee
	// that readers are unblocked in order, although the implementation uses a
	// queue (https://go.dev/src/runtime/chan.go, search for chanrecv). It's
	// tough to guarantee this ourselves using a queue of channels because we
	// may timeout which adds concurrency issues in the mgmt of that queue.
	case <-regChan:
	case <-time.After(timeout):
		return fmt.Errorf("cannot wait for ongoing transaction for registry %s any longer: timing out", registryRef)
	}

	return nil
}

// the state lock must be held by the caller
func (m *RegistryManager) unblockWaitingRegistryWriter(account, registryName string) {
	registryRef := account + "/" + registryName
	logger.Debugf("unblocking waiting writer for registry %s", registryRef)

	regChan := m.registryChans[registryRef]
	if regChan == nil {
		return
	}

	if len(regChan) > 0 {
		// shouldn't happen because we can only have one ongoing tx but if the
		// channel has any buffered values then the any future reader is already unblocked
		return
	}

	regChan <- struct{}{}
}

func setOngoingTransaction(st *state.State, account, registryName string, commitTaskID string) error {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return err
		}

		txCommits = make(map[string]string, 1)
	}

	registryRef := account + "/" + registryName
	if taskID, ok := txCommits[registryRef]; ok {
		return fmt.Errorf("internal error: task %s cannot set ongoing tx for registry %s: task %s already has ongoing tx", commitTaskID, registryRef, taskID)
	}

	txCommits[registryRef] = commitTaskID
	st.Set("registry-tx-commits", txCommits)
	return nil
}

func (m *RegistryManager) unsetOngoingTransaction(st *state.State, account, registryName string) error {
	var txCommits map[string]string
	err := st.Get("registry-tx-commits", &txCommits)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// already unset, nothing to do
			return nil
		}
		return err
	}

	registryRef := account + "/" + registryName
	if _, ok := txCommits[registryRef]; !ok {
		// already unset, nothing to do
		return nil
	}

	delete(txCommits, registryRef)

	if len(txCommits) == 0 {
		st.Set("registry-tx-commits", nil)
	} else {
		st.Set("registry-tx-commits", txCommits)
	}

	return nil
}

func setupRegistryHook(st *state.State, snapName, hookName string, ignoreError bool) *state.Task {
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

type viewChangedHandler struct {
	hookstate.SnapHookHandler
	ctx *hookstate.Context
}

func (h *viewChangedHandler) Precondition() (bool, error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	// TODO: find all the plugs again? new plugs might've been connected since the
	// previous check when the change is created (possible TOCTOU)

	plugName, _, ok := strings.Cut(h.ctx.HookName(), "-view-changed")
	if !ok || plugName == "" {
		// TODO: add support for manager hooks (e.g., change-vie, save-view)
		return false, fmt.Errorf("cannot run registry hook handler for unknown hook: %s", h.ctx.HookName())
	}

	repo := ifacerepo.Get(h.ctx.State())
	conns, err := repo.Connected(h.ctx.InstanceName(), plugName)
	if err != nil {
		return false, fmt.Errorf("cannot determine precondition for hook %s: %w", h.ctx.HookName(), err)
	}

	return len(conns) > 0, nil
}

type changeViewHandler struct {
	ctx *hookstate.Context
}

// TODO: precondition
func (h *changeViewHandler) Before() error { return nil }
func (h *changeViewHandler) Error(hookErr error) (ignoreHookErr bool, err error) {
	return false, nil
}

func (h *changeViewHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return fmt.Errorf("cannot get transaction in change-registry handler: %v", err)
	}

	if tx.aborted() {
		return fmt.Errorf("cannot change registry: snap %s rejected changes: %s", tx.abortingSnap, tx.abortReason)
	}

	return nil
}

type saveViewHandler struct {
	ctx *hookstate.Context
}

// TODO: precondition
func (h *saveViewHandler) Before() error { return nil }

func (h *saveViewHandler) Error(origErr error) (ignoreErr bool, err error) {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	st := h.ctx.State()

	var commitTaskID string
	if err := t.Get("commit-task", &commitTaskID); err != nil {
		return false, err
	}

	// stop all tasks
	for _, t := range t.Change().Tasks() {
		if t.Status() == state.DoStatus {
			t.SetStatus(state.HoldStatus)
		}
	}

	// create roll back tasks for the previously done save-registry hooks
	last := t
	for curTask := t; curTask.Kind() == "run-hook"; curTask = curTask.WaitTasks()[0] {
		var hooksup hookstate.HookSetup
		if err := curTask.Get("hook-setup", &hooksup); err != nil {
			// TODO; wrap error in more context
			return false, err
		}

		if !strings.HasPrefix(hooksup.Hook, "save-view-") {
			break
		}

		// if we fail to rollback, there's nothing we can do
		ignoreError := true
		rollbackTask := setupRegistryHook(st, hooksup.Snap, hooksup.Hook, ignoreError)
		rollbackTask.Set("commit-task", commitTaskID)
		rollbackTask.WaitFor(last)
		curTask.Change().AddTask(rollbackTask)
		last = rollbackTask

		// should never happen but let's be careful for now
		if len(curTask.WaitTasks()) != 1 {
			return false, fmt.Errorf("internal error: cannot rollback failed save-view: expected one prerequisite task")
		}
	}

	// save the original error so we can return that once the rollback is done
	last.Set("original-error", origErr.Error())

	tx, commitTask, err := GetStoredTransaction(t)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-view: cannot get transaction: %v", err)
	}

	// clear the transaction changes
	err = tx.Clear(st)
	if err != nil {
		return false, fmt.Errorf("cannot rollback failed save-view: cannot clear transaction changes: %v", err)
	}
	commitTask.Set("registry-transaction", tx)

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
		// TODO; wrap
		return err
	}

	logger.Noticef("successfully rolled back failed save-view hooks")
	// we're finished rolling back, so return the original error
	return errors.New(origErr)
}

func IsRegistryHook(ctx *hookstate.Context) bool {
	return !ctx.IsEphemeral() &&
		(strings.HasPrefix(ctx.HookName(), "change-view-") ||
			strings.HasPrefix(ctx.HookName(), "save-view-") ||
			strings.HasSuffix(ctx.HookName(), "-view-changed"))
}
