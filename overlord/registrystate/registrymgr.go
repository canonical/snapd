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
	"regexp"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
)

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

type RegistryManager struct{}

func Manager(st *state.State, hookMgr *hookstate.HookManager, runner *state.TaskRunner) *RegistryManager {
	m := &RegistryManager{}

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-registry-tx", m.doCommitTransaction, nil)
	// only activated on undo, to clear the ongoing transaction from state and
	// unblock others who may be waiting for it
	runner.AddHandler("clear-registry-tx-on-error", m.noop, m.clearOngoingTransaction)
	runner.AddHandler("clear-registry-tx", m.clearOngoingTransaction, nil)

	hookMgr.Register(regexp.MustCompile("^change-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &changeViewHandler{ctx: context}
	})
	hookMgr.Register(regexp.MustCompile("^save-view-.+$"), func(context *hookstate.Context) hookstate.Handler {
		return &saveViewHandler{ctx: context}
	})
	hookMgr.Register(regexp.MustCompile("^.+-view-changed$"), func(context *hookstate.Context) hookstate.Handler {
		return &hookstate.SnapHookHandler{}
	})

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

	err = unsetOngoingTransaction(st, tx.RegistryAccount, tx.RegistryName)
	if err != nil {
		return err
	}

	// TODO: unblock next waiting registry writer once we add the blocking logic
	return nil
}

func setOngoingTransaction(st *state.State, account, registryName, commitTaskID string) error {
	var commitTasks map[string]string
	err := st.Get("registry-commit-tasks", &commitTasks)
	if err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return err
		}

		commitTasks = make(map[string]string, 1)
	}

	registryRef := account + "/" + registryName
	if taskID, ok := commitTasks[registryRef]; ok {
		return fmt.Errorf("internal error: cannot set task %q as ongoing commit task for registry %s: already have %q", commitTaskID, registryRef, taskID)
	}

	commitTasks[registryRef] = commitTaskID
	st.Set("registry-commit-tasks", commitTasks)
	return nil
}

func unsetOngoingTransaction(st *state.State, account, registryName string) error {
	var commitTasks map[string]string
	err := st.Get("registry-commit-tasks", &commitTasks)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// already unset, nothing to do
			return nil
		}
		return err
	}

	registryRef := account + "/" + registryName
	if _, ok := commitTasks[registryRef]; !ok {
		// already unset, nothing to do
		return nil
	}

	delete(commitTasks, registryRef)

	if len(commitTasks) == 0 {
		st.Set("registry-commit-tasks", nil)
	} else {
		st.Set("registry-commit-tasks", commitTasks)
	}

	return nil
}

func (m *RegistryManager) noop(*state.Task, *tomb.Tomb) error { return nil }

type changeViewHandler struct {
	ctx *hookstate.Context
}

func (h *changeViewHandler) Before() error                 { return nil }
func (h *changeViewHandler) Error(err error) (bool, error) { return false, nil }

func (h *changeViewHandler) Done() error {
	h.ctx.Lock()
	defer h.ctx.Unlock()

	t, _ := h.ctx.Task()
	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return fmt.Errorf("cannot get transaction in change-registry handler: %v", err)
	}

	if tx.aborted() {
		return fmt.Errorf("cannot change registry %s/%s: %s rejected changes: %s", tx.RegistryAccount, tx.RegistryName, tx.abortingSnap, tx.abortReason)
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
	if err := t.Get("commit-task", &commitTaskID); err != nil {
		return false, err
	}

	// create roll back tasks for the previously done save-registry hooks (starting
	// with the hook that failed, so it tries to overwrite with a pristine databag
	// just like any previous save-view hooks)
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

	// prevent the next registry tasks from running before the rollbacks. Once the
	// last rollback task errors, these will be put on Hold by the usual mechanism
	for _, halt := range t.HaltTasks() {
		halt.WaitFor(last)
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
		return err
	}

	logger.Noticef("successfully rolled back failed save-view hooks")
	// we're finished rolling back, so return the original error
	return errors.New(origErr)
}
