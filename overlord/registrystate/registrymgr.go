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

type RegistryManager struct {
}

func Manager(st *state.State, runner *state.TaskRunner) *RegistryManager {
	m := &RegistryManager{}

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-transaction", m.doCommitTransaction, nil)
	runner.AddHandler("clear-tx-on-error", m.noop, m.clearOngoingTransaction)

	return m
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

	// unset the tracked transaction/task for this registry regardless of whether
	// we successfully committed or not
	defer func() {
		unsetErr := UnsetTransactionCommit(st, tx.RegistryAccount, tx.RegistryName)
		if err == nil && unsetErr != nil {
			err = unsetErr
		}
	}()

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

	return UnsetTransactionCommit(st, tx.RegistryAccount, tx.RegistryName)
}

func (m *RegistryManager) noop(*state.Task, *tomb.Tomb) error { return nil }

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
