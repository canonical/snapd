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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
	"gopkg.in/tomb.v2"
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
	m := &ConfdbManager{}

	// no undo since if we commit there's no rolling back
	runner.AddHandler("commit-confdb-tx", m.doCommitTransaction, nil)
	// only activated on undo, to clear the ongoing transaction from state and
	// unblock others who may be waiting for it
	runner.AddHandler("clear-confdb-tx-on-error", m.noop, m.clearOngoingTransaction)
	runner.AddHandler("clear-confdb-tx", m.clearOngoingTransaction, nil)

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

func (m *ConfdbManager) Ensure() error { return nil }

func (m *ConfdbManager) doCommitTransaction(t *state.Task, _ *tomb.Tomb) (err error) {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	confdbAssert, err := assertstateConfdb(st, tx.ConfdbAccount, tx.ConfdbName)
	if err != nil {
		return err
	}
	schema := confdbAssert.Confdb().Schema

	return tx.Commit(st, schema)
}

func (m *ConfdbManager) clearOngoingTransaction(t *state.Task, _ *tomb.Tomb) error {
	st := t.State()
	st.Lock()
	defer st.Unlock()

	tx, _, err := GetStoredTransaction(t)
	if err != nil {
		return err
	}

	err = unsetOngoingTransaction(st, tx.ConfdbAccount, tx.ConfdbName)
	if err != nil {
		return err
	}

	// TODO: unblock next waiting confdb writer once we add the blocking logic
	return nil
}

func setOngoingTransaction(st *state.State, account, confdbName, commitTaskID string) error {
	var commitTasks map[string]string
	err := st.Get("confdb-commit-tasks", &commitTasks)
	if err != nil {
		if !errors.Is(err, &state.NoStateError{}) {
			return err
		}

		commitTasks = make(map[string]string, 1)
	}

	confdbRef := account + "/" + confdbName
	if taskID, ok := commitTasks[confdbRef]; ok {
		return fmt.Errorf("internal error: cannot set task %q as ongoing commit task for confdb %s: already have %q", commitTaskID, confdbRef, taskID)
	}

	commitTasks[confdbRef] = commitTaskID
	st.Set("confdb-commit-tasks", commitTasks)
	return nil
}

func unsetOngoingTransaction(st *state.State, account, confdbName string) error {
	var commitTasks map[string]string
	err := st.Get("confdb-commit-tasks", &commitTasks)
	if err != nil {
		if errors.Is(err, &state.NoStateError{}) {
			// already unset, nothing to do
			return nil
		}
		return err
	}

	confdbRef := account + "/" + confdbName
	if _, ok := commitTasks[confdbRef]; !ok {
		// already unset, nothing to do
		return nil
	}

	delete(commitTasks, confdbRef)

	if len(commitTasks) == 0 {
		st.Set("confdb-commit-tasks", nil)
	} else {
		st.Set("confdb-commit-tasks", commitTasks)
	}

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
	tx, _, err := GetStoredTransaction(t)
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
	if err := t.Get("commit-task", &commitTaskID); err != nil {
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
		rollbackTask.Set("commit-task", commitTaskID)
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

	tx, saveChanges, err := GetStoredTransaction(t)
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
