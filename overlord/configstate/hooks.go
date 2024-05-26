// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package configstate

import (
	"errors"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

// configureHandler is the handler for the configure hook.
type configureHandler struct {
	context *hookstate.Context
}

func newConfigureHandler(context *hookstate.Context) hookstate.Handler {
	return &configureHandler{context: context}
}

// Before is called by the HookManager before the configure hook is run.
func (h *configureHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()

	tr := ContextTransaction(h.context)

	// Initialize the transaction if there's a patch provided in the
	// context or useDefaults is set in which case gadget defaults are used.

	var patch map[string]interface{}
	var useDefaults bool
	if mylog.Check(h.context.Get("use-defaults", &useDefaults)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	instanceName := h.context.InstanceName()
	if useDefaults {
		st := h.context.State()
		task, _ := h.context.Task()
		deviceCtx := mylog.Check2(snapstate.DeviceCtx(st, task, nil))

		patch = mylog.Check2(snapstate.ConfigDefaults(st, deviceCtx, instanceName))
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		// core is handled internally and does not need a configure
		// hook, for other snaps double check that the hook is present
		if len(patch) != 0 && instanceName != "core" {
			// TODO: helper on context?
			info := mylog.Check2(snapstate.CurrentInfo(st, instanceName))

			if info.Hooks["configure"] == nil {
				return fmt.Errorf("cannot apply gadget config defaults for snap %q, no configure hook", instanceName)
			}
			// if both default-configure and configure hooks are present, default-configure
			// hook is responsible for applying the default configuration
			if info.Hooks["default-configure"] != nil {
				patch = nil
			}
		}
	} else {
		if mylog.Check(h.context.Get("patch", &patch)); err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
	}
	mylog.Check(config.Patch(tr, instanceName, patch))

	return nil
}

// Done is called by the HookManager after the configure hook has exited
// successfully.
func (h *configureHandler) Done() error {
	return nil
}

// Error is called by the HookManager after the configure hook has exited
// non-zero, and includes the error.
func (h *configureHandler) Error(err error) (bool, error) {
	return false, nil
}

// defaultConfigureHandler is the handler for the default-configure hook.
type defaultConfigureHandler struct {
	context *hookstate.Context
}

func newDefaultConfigureHandler(context *hookstate.Context) hookstate.Handler {
	return &defaultConfigureHandler{context: context}
}

// Before is called by the HookManager before the default-configure hook is run.
func (h *defaultConfigureHandler) Before() error {
	h.context.Lock()
	defer h.context.Unlock()

	tr := ContextTransaction(h.context)

	instanceName := h.context.InstanceName()
	st := h.context.State()
	info := mylog.Check2(snapstate.CurrentInfo(st, instanceName))

	hasDefaultConfigureHook := info.Hooks["default-configure"] != nil
	hasConfigureHook := info.Hooks["configure"] != nil

	// default-configure hook cannot be used without configure hook, because it is only intended
	// as an extension of the configure hook that provides additional configuration support
	if hasDefaultConfigureHook && !hasConfigureHook {
		// this scenario should be prevented by the snap checker
		return fmt.Errorf("cannot use default-configure hook for snap %q, no configure hook", instanceName)
	}

	if hasDefaultConfigureHook {
		task, _ := h.context.Task()
		deviceCtx := mylog.Check2(snapstate.DeviceCtx(st, task, nil))

		patch := mylog.Check2(snapstate.ConfigDefaults(st, deviceCtx, instanceName))
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return err
		}
		mylog.Check(config.Patch(tr, instanceName, patch))

	}

	return nil
}

// Done is called by the HookManager after the default-configure hook has exited
// successfully.
func (h *defaultConfigureHandler) Done() error {
	return nil
}

// Error is called by the HookManager after the default-configure hook has exited
// non-zero, and includes the error.
func (h *defaultConfigureHandler) Error(err error) (bool, error) {
	return false, nil
}

// cachedTransaction is the index into the context cache where the initialized
// transaction is stored.
type cachedTransaction struct{}

// ContextTransaction retrieves the transaction cached within the context (and
// creates one if it hasn't already been cached).
func ContextTransaction(context *hookstate.Context) *config.Transaction {
	// Check for one already cached
	tr, ok := context.Cached(cachedTransaction{}).(*config.Transaction)
	if ok {
		return tr
	}

	// It wasn't already cached, so create and cache a new one
	tr = config.NewTransaction(context.State())

	context.OnDone(func() error {
		tr.Commit()
		if context.InstanceName() == "core" {
			// make sure the Ensure logic can process
			// system configuration changes as soon as possible
			context.State().EnsureBefore(0)
		}
		return nil
	})

	context.Cache(cachedTransaction{}, tr)
	return tr
}
